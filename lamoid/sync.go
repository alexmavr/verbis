package main

import (
	"context"
	"fmt"
	"log"
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
		staleThreshold:  30 * time.Minute,
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

		if time.Since(state.LastSync) > s.staleThreshold && !state.Syncing {
			log.Printf("Sync required for %s\n", c.Name())

			// TODO: replace with atomic get and set for syncing state
			err = store.LockConnector(ctx, store.GetWeaviateClient(), c.Name())
			if err != nil {
				return fmt.Errorf("failed to lock connector %s: %s", c.Name(), err)
			}
			// TODO: unlock at end of for loop, not function return
			defer store.UnlockConnector(ctx, store.GetWeaviateClient(), c.Name())

			chunks, err := c.Sync(ctx)
			if err != nil {
				return fmt.Errorf("failed to sync %s: %s", c.Name(), err)
			}

			chunkItems := []types.AddVectorItem{}
			for _, chunk := range chunks {
				log.Printf("Processing chunk: %s\n", chunk.SourceURL)
				resp, err := EmbedFromModel(chunk.Text)
				if err != nil {
					log.Printf("Failed to get embeddings: %s\n", err)
					continue
				}
				embedding := resp.Embedding

				chunkItems = append(chunkItems, types.AddVectorItem{
					Chunk:  chunk,
					Vector: embedding,
				})
			}

			if len(chunkItems) == 0 {
				log.Printf("No chunks to add for %s\n", c.Name())
				continue
			}

			err = store.AddVectors(ctx, store.GetWeaviateClient(), chunkItems)
			if err != nil {
				return fmt.Errorf("failed to add vectors: %s", err)
			}
		}
	}

	return nil
}
