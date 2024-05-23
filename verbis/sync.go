package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/verbis-ai/verbis/verbis/connectors"
	"github.com/verbis-ai/verbis/verbis/store"
	"github.com/verbis-ai/verbis/verbis/types"
	"github.com/verbis-ai/verbis/verbis/util"
)

const (
	MinChunkSize = 10
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

func (s *Syncer) Init(ctx context.Context) error {
	if len(s.connectors) > 0 {
		// Connectors already initialized, nothing to do
		log.Printf("Syncer init called with %d connectors, skipping state restoration", len(s.connectors))
		return nil
	}

	states, err := store.AllConnectorStates(ctx, store.GetWeaviateClient())
	if err != nil {
		return fmt.Errorf("failed to get connector states: %s", err)
	}
	for _, state := range states {
		constructor, ok := connectors.AllConnectors[state.ConnectorType]
		if !ok {
			return fmt.Errorf("unknown connector type %s", state.ConnectorType)
		}
		c := constructor()
		err = c.Init(ctx, state.ConnectorID)
		if err != nil {
			return fmt.Errorf("failed to init connector %s: %s", state.ConnectorID, err)
		}
		err = s.AddConnector(c)
		if err != nil {
			return fmt.Errorf("failed to add connector %s: %s", state.ConnectorID, err)
		}
	}
	return nil
}

func (s *Syncer) AddConnector(c types.Connector) error {
	_, ok := s.connectors[c.ID()]
	if !ok {
		s.connectors[c.ID()] = c
	}
	return nil
}

func (s *Syncer) GetConnector(id string) types.Connector {
	return s.connectors[id]
}

func (s *Syncer) GetConnectorStates(ctx context.Context) ([]*types.ConnectorState, error) {
	states := []*types.ConnectorState{}
	for _, c := range s.connectors {
		state, err := c.Status(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get state for %s: %s", c.ID(), err)
		}
		states = append(states, state)
	}

	// Sort by connector type and user
	sort.Slice(states, func(i, j int) bool {
		if states[i].ConnectorType == states[j].ConnectorType {
			return states[i].User < states[j].User
		}
		return states[i].ConnectorType < states[j].ConnectorType
	})

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

func hash(text string) string {
	h := sha256.New()
	h.Write([]byte(text))
	return base64.URLEncoding.EncodeToString(h.Sum(nil))
}

type chunkCount struct {
	numChunks    int
	numDocuments int
}

func chunkAdder(ctx context.Context, c types.Connector, chunkChan chan types.Chunk, countChan chan chunkCount, doneChan chan struct{}) {
	// TODO: hold buffer and add vectors in batches
	for chunk := range chunkChan {
		log.Printf("Received chunk of length %d\n", len(chunk.Text))
		saneChunk := util.CleanWhitespace(chunk.Text)
		log.Printf("Sanitized chunk to length %d\n", len(saneChunk))
		if len(saneChunk) < MinChunkSize {
			log.Printf("Skipping short chunk: %s\n", saneChunk)
			continue
		}

		chunkHash := hash(saneChunk)
		exists, err := store.ChunkHashExists(ctx, store.GetWeaviateClient(), chunkHash)
		if err != nil {
			log.Printf("Failed to check chunk hash: %s\n", err)
			continue
		}
		if exists {
			log.Printf("Chunk already exists: %s\n", chunkHash)
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
		chunk.Hash = chunkHash
		addResp, err := store.AddVectors(ctx, store.GetWeaviateClient(), []types.AddVectorItem{
			{
				Chunk:  chunk,
				Vector: embedding,
			},
		})
		if err != nil {
			log.Printf("Failed to add vectors: %s\n", err)
		}
		log.Printf("Added vectors")

		countChan <- chunkCount{
			numChunks:    addResp.NumChunksAdded,
			numDocuments: addResp.NumDocsAdded,
		}
		log.Printf("Added vectors for chunk: %s\n", chunk.SourceURL)
	}

	log.Printf("Chunk channel closed")
	close(doneChan)
}

func chunkCounter(ctx context.Context, c types.Connector, countChan chan chunkCount) {
	updateEvery := 10

	counts := []chunkCount{}
	for count := range countChan {
		counts = append(counts, count)
		if len(counts) < updateEvery {
			continue
		}

		numChunks := 0
		numDocs := 0
		for _, prevCount := range counts {
			numChunks += prevCount.numChunks
			numDocs += prevCount.numDocuments
		}
		counts = []chunkCount{}
		state, err := c.Status(ctx)
		if err != nil {
			log.Printf("Failed to get status: %s\n", err)
			return
		}
		state.NumChunks += numChunks
		state.NumDocuments += numDocs
		err = c.UpdateConnectorState(ctx, state)
		if err != nil {
			log.Printf("Failed to update status: %s\n", err)
			return
		}
	}

	if len(counts) == 0 {
		return
	}
	numChunks := 0
	numDocs := 0
	for _, prevCount := range counts {
		numChunks += prevCount.numChunks
		numDocs += prevCount.numDocuments
	}
	state, err := c.Status(ctx)
	if err != nil {
		log.Printf("Failed to get status: %s\n", err)
		return
	}
	state.NumChunks += numChunks
	state.NumDocuments += numDocs
	err = c.UpdateConnectorState(ctx, state)
	if err != nil {
		log.Printf("Failed to update status: %s\n", err)
		return
	}
}

func (s *Syncer) connectorSync(ctx context.Context, c types.Connector, state *types.ConnectorState, errChan chan<- error) {
	// TODO: replace with atomic get and set for syncing state
	err := store.LockConnector(ctx, store.GetWeaviateClient(), c.ID())
	if err != nil {
		errChan <- fmt.Errorf("failed to lock connector %s: %s", c.ID(), err)
		return
	}
	// TODO: unlock at end of for loop, not function return
	defer store.UnlockConnector(ctx, store.GetWeaviateClient(), c.ID())

	newSyncTime := time.Now()

	chunkChan := make(chan types.Chunk) // closed by Sync
	errChanSync := make(chan error)     // closed by Sync
	doneChan := make(chan struct{})     // closed by chunkAdder
	countChan := make(chan chunkCount)  // closed by chunkCounter

	// TODO: rewrite as sync waitgroup
	go chunkAdder(ctx, c, chunkChan, countChan, doneChan)
	go chunkCounter(ctx, c, countChan)
	go c.Sync(ctx, state.LastSync, chunkChan, errChanSync)

	select {
	case <-ctx.Done():
		// The sync time has not been updated, so the next sync will pick up the same chunks
		state.Syncing = false
		err = c.UpdateConnectorState(ctx, state)
		if err != nil {
			errChan <- fmt.Errorf("unable to update last sync for %s: %s", c.ID(), err)
			return
		}
		break
	case <-doneChan:
		log.Printf("Sync complete for %s\n", c.ID())
	case err := <-errChanSync:
		if err != nil {
			log.Printf("Error during sync for %s: %s\n", c.ID(), err)
		}
		log.Printf("ErrChan closed before DoneChan")
	}
	log.Printf("Sync for connector %s complete\n", c.ID())

	log.Printf("NumChunks: %d, NumDocuments: %d\n", state.NumChunks, state.NumDocuments)

	state, err = c.Status(ctx)
	if err != nil {
		errChan <- fmt.Errorf("failed to get status for %s: %s", c.ID(), err)
		return
	}

	state.LastSync = newSyncTime
	state.Syncing = false
	err = c.UpdateConnectorState(ctx, state)
	if err != nil {
		errChan <- fmt.Errorf("unable to update last sync for %s: %s", c.ID(), err)
		return
	}
	close(errChan)
}

func (s *Syncer) SyncNow(ctx context.Context) error {
	errChans := []chan error{}
	for _, c := range s.connectors {
		log.Printf("Checking status for connector %s\n", c.ID())
		state, err := c.Status(ctx)
		if err != nil {
			return fmt.Errorf("failed to get status for %s: %s", c.ID(), err)
		}
		if state == nil {
			return fmt.Errorf("nil state for %s", c.ID())
		}

		if !state.AuthValid {
			log.Printf("Auth required for %s\n", c.ID())
			continue
		}

		if state.Syncing {
			log.Printf("Sync already in progress for %s, skipping\n", c.ID())
			continue
		}

		errChan := make(chan error)
		errChans = append(errChans, errChan)

		if time.Since(state.LastSync) > s.staleThreshold {
			log.Printf("Sync required for %s\n", c.ID())
			go s.connectorSync(ctx, c, state, errChan)
		}
	}

	for _, errChan := range errChans {
		err := <-errChan
		if err != nil {
			return err
		}
	}

	return nil
}
