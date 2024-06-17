package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"sync"
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
	credentials       types.BuildCredentials
	version           string
}

func NewSyncer(posthogClient posthog.Client, posthogDistinctID string, creds types.BuildCredentials, version string) *Syncer {
	return &Syncer{
		connectors:        map[string]types.Connector{},
		syncCheckPeriod:   1 * time.Minute,
		staleThreshold:    1 * time.Minute,
		posthogClient:     posthogClient,
		posthogDistinctID: posthogDistinctID,
		credentials:       creds,
		version:           version,
	}
}

func (s *Syncer) Init(ctx context.Context) error {
	s.connectors = map[string]types.Connector{}

	states, err := store.AllConnectorStates(ctx, store.GetWeaviateClient())
	if err != nil {
		return fmt.Errorf("failed to get connector states: %s", err)
	}
	count := 0
	for _, state := range states {
		constructor, ok := connectors.AllConnectors[state.ConnectorType]
		if !ok {
			return fmt.Errorf("unknown connector type %s", state.ConnectorType)
		}
		c := constructor(s.credentials)
		err = c.Init(ctx, state.ConnectorID)
		if err != nil {
			return fmt.Errorf("failed to init connector %s: %s", state.ConnectorID, err)
		}
		err = s.AddConnector(c)
		count++
		if err != nil {
			return fmt.Errorf("failed to add connector %s: %s", state.ConnectorID, err)
		}
	}

	log.Printf("Syncer initialized with %d connectors from stored states", count)
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

func (s *Syncer) DeleteConnector(ctx context.Context, connectorID string) error {
	connector, ok := s.connectors[connectorID]
	if !ok {
		return fmt.Errorf("connector %s not found", connectorID)
	}
	connector.Cancel()
	err := store.DeleteConnector(ctx, connector)
	if err != nil {
		return fmt.Errorf("failed to delete connector %s: %s", connectorID, err)
	}
	delete(s.connectors, connectorID)
	return nil
}

func (s *Syncer) GetConnectorStates(ctx context.Context, fetch_all bool) ([]*types.ConnectorState, error) {
	states := []*types.ConnectorState{}
	for _, c := range s.connectors {
		state, err := c.Status(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get state for %s: %s", c.ID(), err)
		}

		// Fetch all if explicitly requested, else only ones with AuthValid
		if fetch_all || state.AuthValid {
			states = append(states, state)
		}
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
	defer log.Printf("Syncer has stopped")
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(s.syncCheckPeriod):
			// TODO clean stale connectors
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

func chunkAdder(ctx context.Context, chunkChan chan types.ChunkSyncResult, resChan chan chunkAddResult) {
	defer close(resChan)
	// TODO: hold buffer and add vectors in batches
	for res := range chunkChan {
		if res.Err != nil {
			resChan <- chunkAddResult{
				err: fmt.Errorf("error processing chunk: %s", res.Err),
			}
			continue
		}
		chunk := res.Chunk

		saneChunk := chunk.Text
		saneName := chunk.Name
		if !res.SkipClean {
			saneChunk = util.CleanChunk(chunk.Text)
			saneName = util.CleanChunk(chunk.Name)
		}
		log.Printf("New chunk, length: %d, sanitized: %d\n", len(chunk.Text), len(saneChunk))
		if len(saneChunk) < MinChunkSize {
			log.Printf("Skipping short chunk: %s\n", saneChunk)
			continue
		}

		chunkHash := hash(saneChunk)
		exists, err := store.ChunkHashExists(ctx, store.GetWeaviateClient(), chunkHash)
		if err != nil && !store.IsErrChunkNotFound(err) {
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

func stateUpdater(ctx context.Context, c types.Connector, resChan chan chunkAddResult, doneChan chan struct{}) {
	defer close(doneChan)

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

func (s *Syncer) connectorSync(ctx context.Context, c types.Connector, state *types.ConnectorState) error {
	// Keep a copy of the current connector state to calculate diffs
	prevState, err := copyState(state)
	if err != nil {
		return fmt.Errorf("failed to copy state: %s", err)
	}

	syncStartTime := time.Now()

	// The channel where all chunks are sent. Closed by c.Sync when done
	chunkChan := make(chan types.ChunkSyncResult)

	// The channel where sync-wide errors during sync are sent, causing a halt. Closed here
	errChanSync := make(chan error)
	defer close(errChanSync)

	// The channel where the results of chunk addition are sent, to be processed
	// by the stateUpdater. Closed by chunkAdder after all chunks are processed.
	chunkAddResChan := make(chan chunkAddResult)

	// The channel to signal that the sync is done, including the final state
	// update. Closed by the stateUpdater
	doneChan := make(chan struct{})

	// Sync sends chunks to chunkChan, chunkAdder processes them and sends results to chunkAddResChan
	// This allows the following to happen in parallel:
	// - Fetches from the connector and document conversions (in Sync)
	// - Embeddings generation and addition to weaviate (in chunkAdder)
	// - Periodic updates to the connector state (in stateUpdater)
	go c.Sync(state.LastSync, chunkChan, errChanSync)
	go chunkAdder(ctx, chunkChan, chunkAddResChan)
	go stateUpdater(ctx, c, chunkAddResChan, doneChan)

	syncError := ""
	select {
	case <-ctx.Done():
		// The sync time has not been updated, so the next sync will pick up the same chunks
		log.Printf("Syncer: Context cancelled")
		state, err := c.Status(ctx)
		if err != nil {
			return fmt.Errorf("failed to get status for %s: %s", c.ID(), err)
		}
		state.Syncing = false
		err = c.UpdateConnectorState(ctx, state)
		if err != nil {
			return fmt.Errorf("unable to update last sync for %s: %s", c.ID(), err)
		}
		break
	case err := <-errChanSync:
		if err != nil {
			log.Printf("Sync for connector %s %s completed with error: %s", c.Type(), c.ID(), err)
			syncError = err.Error()
		} else {
			log.Printf("Unexpected close for errChanSync")
		}
	case <-doneChan:
		log.Printf("Sync for connector %s %s completed successfully", c.Type(), c.ID())
	}

	state, err = c.Status(ctx)
	if err != nil {
		return fmt.Errorf("failed to get status for %s: %s", c.ID(), err)
	}
	if syncError == "" {
		// Only update the sync time if the overall sync was successful (even if there were chunk errors)
		state.LastSync = syncStartTime
	}
	state.Syncing = false
	err = c.UpdateConnectorState(ctx, state)
	if err != nil {
		return fmt.Errorf("unable to update last sync for %s: %s", c.ID(), err)
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
		log.Printf("Syncer: no new items found for %s %s\n", c.Type(), c.ID())
		return nil
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
			Set("sync_error", syncError).
			Set("version", s.version),
	})
	if err != nil {
		return fmt.Errorf("failed to enqueue sync event: %s", err)
	}

	log.Printf("Posted Sync on posthog for %s %s\n", c.Type(), c.ID())
	return nil
}

func (s *Syncer) ASyncNow(ctx context.Context) {
	log.Printf("Attempting async sync")
	go func() {
		err := s.SyncNow(ctx)
		if err != nil {
			log.Printf("Failed to sync: %s\n", err)
		}
	}()
}

// maybeSyncConnector returns an error only if the entire sync should halt
func (s *Syncer) maybeSyncConnector(ctx context.Context, wg *sync.WaitGroup, c types.Connector) error {
	log.Printf("Checking status for connector %s %s\n", c.Type(), c.ID())

	state, err := store.SetConnectorSyncing(ctx, store.GetWeaviateClient(), c.ID(), true)
	if store.IsSyncingAlreadyExpected(err) {
		log.Printf("Connector %s %s already syncing", c.Type(), c.ID())
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to set connector %s %s to syncing state: %s", c.Type(), c.ID(), err)
	}
	log.Printf("Connector %s %s set to syncing", c.Type(), c.ID())
	unlock := true

	if !state.AuthValid {
		log.Printf("Auth required for %s %s", c.Type(), c.ID())
	} else {
		if time.Since(state.LastSync) > s.staleThreshold {
			log.Printf("Sync required for %s %s", c.Type(), c.ID())
			unlock = false
			wg.Add(1)
			go func(c types.Connector) {
				defer wg.Done()
				new_err := s.connectorSync(ctx, c, state)
				if new_err != nil {
					log.Printf("Error syncing %s %s: %s", c.Type(), c.ID(), new_err)
				}
			}(c)
		} else {
			log.Printf("Sync not required for %s", c.ID())
		}
	}

	// Unlock syncing state
	if unlock {
		_, err = store.SetConnectorSyncing(ctx, store.GetWeaviateClient(), c.ID(), false)
		if err != nil {
			log.Printf("Failed to set connector %s %s to not syncing state: %s", c.Type(), c.ID(), err)
		} else {
			log.Printf("Connector %s %s set to not syncing", c.Type(), c.ID())
		}
	}

	return nil
}

func (s *Syncer) SyncNow(ctx context.Context) error {
	log.Printf("SyncNow started")
	wg := sync.WaitGroup{}
	for _, c := range s.connectors {
		err := s.maybeSyncConnector(ctx, &wg, c)
		if err != nil {
			return fmt.Errorf("failed to trigger sync for connector %s %s: %s", c.Type(), c.ID(), err)
		}
	}
	wg.Wait()
	log.Printf("SyncNow complete")
	return nil
}
