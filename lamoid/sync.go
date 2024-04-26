package main

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/epochlabs-ai/lamoid/lamoid/store"
	"github.com/epochlabs-ai/lamoid/lamoid/types"
)

type Syncer struct {
	connectors      map[string]types.Connector
	syncCheckPeriod time.Duration
	staleThreshold  time.Duration
}

func NewSyncer() *Syncer {
	return &Syncer{
		connectors:      map[string]types.Connector{},
		syncCheckPeriod: 1 * time.Minute,
		staleThreshold:  1 * time.Minute,
	}
}

func (s *Syncer) AddConnector(c types.Connector) error {
	_, ok := s.connectors[c.Name()]
	if ok {
		return fmt.Errorf("connector %s already exists", c.Name())
	}
	s.connectors[c.Name()] = c
	return nil
}

func (s *Syncer) GetConnector(name string) types.Connector {
	return s.connectors[name]
}

func (s *Syncer) GetConnectorStates(ctx context.Context) ([]*types.ConnectorState, error) {
	states := []*types.ConnectorState{}
	for _, c := range s.connectors {
		state, err := c.Status(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get state for %s: %s", c.Name(), err)
		}
		states = append(states, state)
	}
	return states, nil
}

// On launch, and after every sync_period, find all connectors that are not
// actively syncing
func (s *Syncer) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(s.syncCheckPeriod):
			err := s.SyncNow(ctx)
			if err != nil {
				log.Printf("Failed to sync: %s\n", err)
			}
		}
	}
}

func cleanWhitespace(text string) string {
	// The UTF-8 BOM is sometimes present in text files, and should be removed
	bom := []byte{0xEF, 0xBB, 0xBF}
	text = strings.TrimPrefix(text, string(bom))

	// Replace internal sequences of whitespace with a single space
	spacePattern := regexp.MustCompile(`\s+`)
	text = spacePattern.ReplaceAllString(text, " ")
	// Trim leading and trailing whitespace
	// If the initial text was all whitespace, it should return an empty string
	return strings.TrimSpace(text)
}

func printBytes(s string) {
	fmt.Printf("Byte representation of '%s':\n", s)
	for i := 0; i < len(s); i++ {
		fmt.Printf("%d ", s[i])
	}
	fmt.Println()
}

func chunkAdder(ctx context.Context, c types.Connector, chunkChan chan types.Chunk, doneChan chan struct{}) {
	// TODO: hold buffer and add vectors in batches
	for chunk := range chunkChan {
		log.Printf("Received chunk of length %d\n", len(chunk.Text))
		saneChunk := cleanWhitespace(chunk.Text)
		log.Printf("Sanitized chunk to length %d\n", len(saneChunk))
		if len(saneChunk) < 50 {
			log.Printf("Warning: short chunk: %s\n", saneChunk)
			printBytes(saneChunk)
		}
		if len(saneChunk) == 0 {
			log.Printf("Chunk with only whitespace was detected, skipping\n")
			continue
		}
		resp, err := EmbedFromModel(saneChunk)
		if err != nil {
			log.Printf("Failed to get embeddings: %s\n", err)
			continue
		}
		log.Printf("Received embeddings for chunk of length %d", len(saneChunk))
		embedding := resp.Embedding
		chunk.Text = saneChunk
		err = store.AddVectors(ctx, store.GetWeaviateClient(), []types.AddVectorItem{
			{
				Chunk:  chunk,
				Vector: embedding,
			},
		})
		if err != nil {
			log.Printf("Failed to add vectors: %s\n", err)
		}
		log.Printf("Added vectors")

		state, err := c.Status(ctx)
		if err != nil {
			log.Printf("Failed to get status: %s\n", err)
			continue
		}
		state.NumChunks++
		state.NumDocuments++
		err = c.UpdateConnectorState(ctx, state)
		if err != nil {
			log.Printf("Failed to update status: %s\n", err)
			continue
		}
		log.Printf("Added vectors for chunk: %s\n", chunk.SourceURL)
	}
	log.Printf("Chunk channel closed")
	close(doneChan)
}

func (s *Syncer) SyncNow(ctx context.Context) error {
	for _, c := range s.connectors {
		log.Printf("Checking status for connector %s\n", c.Name())
		state, err := c.Status(ctx)
		if err != nil {
			return fmt.Errorf("failed to get status for %s: %s", c.Name(), err)
		}
		if state == nil {
			return fmt.Errorf("nil state for %s", c.Name())
		}

		if !state.AuthValid {
			log.Printf("Auth required for %s\n", c.Name())
			continue
		}

		if state.Syncing {
			log.Printf("Sync already in progress for %s, skipping\n", c.Name())
			continue
		}

		if time.Since(state.LastSync) > s.staleThreshold {
			log.Printf("Sync required for %s\n", c.Name())

			// TODO: replace with atomic get and set for syncing state
			err = store.LockConnector(ctx, store.GetWeaviateClient(), c.Name())
			if err != nil {
				return fmt.Errorf("failed to lock connector %s: %s", c.Name(), err)
			}
			// TODO: unlock at end of for loop, not function return
			defer store.UnlockConnector(ctx, store.GetWeaviateClient(), c.Name())

			newSyncTime := time.Now()

			chunkChan := make(chan types.Chunk) // closed by Sync
			errChan := make(chan error)         // closed by Sync
			doneChan := make(chan struct{})     // closed by chunkAdder

			go chunkAdder(ctx, c, chunkChan, doneChan)
			go c.Sync(ctx, state.LastSync, chunkChan, errChan)

			select {
			case <-ctx.Done():
				return fmt.Errorf("context cancelled")
			case <-doneChan:
				log.Printf("Sync complete for %s\n", c.Name())
			case err := <-errChan:
				if err != nil {
					log.Printf("Error during sync for %s: %s\n", c.Name(), err)
				}
				log.Printf("ErrChan closed before DoneChan")
			}
			log.Printf("Sync for connector %s complete\n", c.Name())

			state, err := c.Status(ctx)
			if err != nil {
				return fmt.Errorf("failed to get status for %s: %s", c.Name(), err)
			}

			log.Printf("NumChunks: %d, NumDocuments: %d\n", state.NumChunks, state.NumDocuments)

			state.LastSync = newSyncTime
			state.Syncing = false
			err = c.UpdateConnectorState(ctx, state)
			if err != nil {
				return fmt.Errorf("unable to update last sync for %s: %s", c.Name(), err)
			}
		}
	}

	return nil
}
