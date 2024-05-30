package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/posthog/posthog-go"

	"github.com/verbis-ai/verbis/verbis/connectors"
	"github.com/verbis-ai/verbis/verbis/store"
	"github.com/verbis-ai/verbis/verbis/types"
	"github.com/verbis-ai/verbis/verbis/util"
)

const (
	MinChunkSize = 10
)

type Syncer struct {
	connectors        map[string]types.Connector
	syncCheckPeriod   time.Duration
	staleThreshold    time.Duration
	posthogClient     posthog.Client
	posthogDistinctID string
}

func NewSyncer(posthogClient posthog.Client, posthogDistinctID string) *Syncer {
	return &Syncer{
		connectors:        map[string]types.Connector{},
		syncCheckPeriod:   1 * time.Minute,
		staleThreshold:    1 * time.Minute,
		posthogClient:     posthogClient,
		posthogDistinctID: posthogDistinctID,
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

type chunkAddResult struct {
	numChunks    int
	numDocuments int
	err          error
}

func chunkAdder(ctx context.Context, chunkChan chan types.Chunk, resChan chan chunkAddResult) {
	defer close(resChan)

	// TODO: hold buffer and add vectors in batches
	for chunk := range chunkChan {
		saneChunk := util.SanitizeString(chunk.Text)
		saneName := util.SanitizeString(chunk.Name)
		log.Printf("New chunk, length: %d, sanitized: %d\n", len(chunk.Text), len(saneChunk))
		if len(saneChunk) < MinChunkSize {
			log.Printf("Skipping short chunk: %s\n", saneChunk)
			continue
		}

		chunkHash := hash(saneChunk)
		exists, err := store.ChunkHashExists(ctx, store.GetWeaviateClient(), chunkHash)
		if err != nil {
			resChan <- chunkAddResult{
				err: fmt.Errorf("failed to check chunk hash: %s", err),
			}
			continue
		}
		if exists {
			log.Printf("Chunk already exists: %s\n", chunkHash)
			continue
		}

		resp, err := EmbedFromModel(saneChunk)
		if err != nil {
			resChan <- chunkAddResult{
				err: fmt.Errorf("failed to get embeddings: %s", err),
			}
			continue
		}

		log.Printf("Genereated embeddings for chunk of length %d", len(saneChunk))
		embedding := resp.Embedding
		chunk.Text = saneChunk
		chunk.Name = saneName
		chunk.Hash = chunkHash
		addResp, err := store.AddVectors(ctx, store.GetWeaviateClient(), []types.AddVectorItem{
			{
				Chunk:  chunk,
				Vector: embedding,
			},
		})
		if err != nil {
			resChan <- chunkAddResult{
				err: fmt.Errorf("failed to add vector: %s", err),
			}
			continue
		}

		resChan <- chunkAddResult{
			numChunks:    addResp.NumChunksAdded,
			numDocuments: addResp.NumDocsAdded,
		}
		log.Printf("Added %d chunks, %d documents for source URL: %s\n", addResp.NumChunksAdded, addResp.NumDocsAdded, chunk.SourceURL)
	}

	log.Printf("Chunk channel closed")
}

func updateState(ctx context.Context, c types.Connector, numChunks, numDocs, numErrors int) {
	state, err := c.Status(ctx)
	if err != nil {
		log.Printf("Failed to get status: %s\n", err)
		return
	}
	state.NumChunks += numChunks
	state.NumDocuments += numDocs
	state.NumErrors += numErrors
	err = c.UpdateConnectorState(ctx, state)
	if err != nil {
		log.Printf("Failed to update status: %s\n", err)
		return
	}
}

func stateUpdater(ctx context.Context, c types.Connector, resChan chan chunkAddResult) {
	// countChan is expected to close before errChunkChan when the sync completes
	numErrors := 0
	numChunks := 0
	numDocs := 0
	updateEvery := 10 // Number of chunks after which we should update the state
	counts := []chunkAddResult{}

	for res := range resChan {
		if res.err == nil {
			counts = append(counts, res)
		} else {
			log.Printf("Error processing chunk: %s\n", res.err)
			numErrors++
		}

		if len(counts)+numErrors < updateEvery {
			continue
		}

		numChunks = 0
		numDocs = 0
		for _, prevCount := range counts {
			numChunks += prevCount.numChunks
			numDocs += prevCount.numDocuments
		}
		updateState(ctx, c, numChunks, numDocs, numErrors)
		counts = []chunkAddResult{}
		numErrors = 0
	}

	numChunks = 0
	numDocs = 0
	for _, prevCount := range counts {
		numChunks += prevCount.numChunks
		numDocs += prevCount.numDocuments
	}
	updateState(ctx, c, numChunks, numDocs, numErrors)
}

func copyState(state *types.ConnectorState) (*types.ConnectorState, error) {
	jsonData, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal state: %s", err)
	}

	newState := &types.ConnectorState{}
	err = json.Unmarshal(jsonData, newState)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal state: %s", err)
	}
	return newState, nil
}

func (s *Syncer) connectorSync(ctx context.Context, c types.Connector, state *types.ConnectorState, errChan chan<- error) {
	defer close(errChan)

	// Keep a copy of the current connector state to calculate diffs
	prevState, err := copyState(state)
	if err != nil {
		errChan <- fmt.Errorf("failed to copy state: %s", err)
		return
	}

	// TODO: replace with atomic get and set for syncing state
	err = store.LockConnector(ctx, store.GetWeaviateClient(), c.ID())
	if err != nil {
		errChan <- fmt.Errorf("failed to lock connector %s: %s", c.ID(), err)
		return
	}
	defer store.UnlockConnector(ctx, store.GetWeaviateClient(), c.ID())

	syncStartTime := time.Now()

	chunkChan := make(chan types.Chunk)          // closed by Sync
	errChanSync := make(chan error)              // closed by Sync
	chunkAddResChan := make(chan chunkAddResult) // closed by chunkAdder
	errChunkChan := make(chan error)             // closed by chunkAdder

	go chunkAdder(ctx, chunkChan, chunkAddResChan)
	go stateUpdater(ctx, c, chunkAddResChan)
	go c.Sync(ctx, state.LastSync, chunkChan, errChunkChan, errChanSync)

	syncError := ""
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
	case err := <-errChanSync:
		if err != nil {
			log.Printf("Sync for connector %s %s complete with error: %s", c.Type(), c.ID(), err)
			syncError = err.Error()
		} else {
			log.Printf("Sync for connector %s %s complete without errors", c.Type(), c.ID())
		}
	}

	state, err = c.Status(ctx)
	if err != nil {
		errChan <- fmt.Errorf("failed to get status for %s: %s", c.ID(), err)
		return
	}

	state.LastSync = syncStartTime
	state.Syncing = false
	err = c.UpdateConnectorState(ctx, state)
	if err != nil {
		errChan <- fmt.Errorf("unable to update last sync for %s: %s", c.ID(), err)
		return
	}
	syncDoneTime := time.Now()

	// Only report sync events if the state has changed to avoid spamming posthog
	num_synced_chunks := state.NumChunks - prevState.NumChunks
	num_synced_docs := state.NumDocuments - prevState.NumDocuments
	num_synced_errors := state.NumErrors - prevState.NumErrors

	log.Printf(
		"Connector sync complete for %s %s: %d new_chunks, %d new_docs, %d new_errors",
		c.Type(),
		c.ID(),
		num_synced_chunks,
		num_synced_docs,
		num_synced_errors,
	)
	if num_synced_chunks == 0 && num_synced_docs == 0 && num_synced_errors == 0 {
		return
	}

	err = s.posthogClient.Enqueue(posthog.Capture{
		DistinctId: s.posthogDistinctID,
		Event:      "Sync",
		Properties: posthog.NewProperties().
			Set("connector_id", c.ID()).
			Set("connector_type", c.Type()).
			Set("new_num_chunks", num_synced_chunks).
			Set("new_num_documents", num_synced_docs).
			Set("new_num_errors", num_synced_errors).
			Set("total_num_chunks", state.NumChunks).
			Set("total_num_documents", state.NumDocuments).
			Set("total_num_errors", state.NumErrors).
			Set("sync_duration", syncDoneTime.Sub(syncStartTime).String()).
			Set("sync_error", syncError),
	})
	if err != nil {
		log.Printf("Posthog: failed to enqueue sync event: %s\n", err)
	}

	log.Printf("Connector sync complete for %s %s\n", c.Type(), c.ID())
}

func (s *Syncer) SyncNow(ctx context.Context) error {
	// TODO: still a minor race condition here
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

	log.Printf("SyncNow complete")
	return nil
}
