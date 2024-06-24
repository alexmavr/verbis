package connectors

import (
	"context"
	"fmt"
	"log"

	"github.com/google/uuid"
	"github.com/verbis-ai/verbis/verbis/keychain"
	"github.com/verbis-ai/verbis/verbis/store"
	"github.com/verbis-ai/verbis/verbis/types"
	"github.com/verbis-ai/verbis/verbis/util"
)

var AllConnectors = map[string]types.ConnectorConstructor{
	string(types.ConnectorTypeGoogleDrive): NewGoogleDriveConnector,
	string(types.ConnectorTypeGmail):       NewGmailConnector,
	string(types.ConnectorTypeOutlook):     NewOutlookConnector,
	string(types.ConnectorTypeSlack):       NewSlackConnector,
}

const (
	// The following parameters are only used in connectors that use
	// content chunking of larger documents.

	// MaxChunkSize in number of words in a chunk
	MaxChunkSize = 200
	// ChunkOverlap is the number of words that overlap between chunks
	ChunkOverlap = 0.2
)

func IsConnectorType(s string) bool {
	_, ok := AllConnectors[s]
	return ok
}

// BaseConnector contains methods and fields common to all connector
// implementations. Most connectors are expected to embed BaseConnector.
type BaseConnector struct {
	id            string
	user          string
	connectorType types.ConnectorType
	context       context.Context
	cancel        context.CancelFunc
	store         types.Store
}

func (s *BaseConnector) ID() string {
	return s.id
}

func (s *BaseConnector) User() string {
	return s.user
}
func (s *BaseConnector) Type() types.ConnectorType {
	return s.connectorType
}

func (s *BaseConnector) Status(ctx context.Context) (*types.ConnectorState, error) {
	state, err := s.store.GetConnectorState(ctx, s.ID())
	if err != nil {
		return nil, fmt.Errorf("failed to get connector state: %v", err)
	}

	if state == nil {
		// No stored state, only happens if sync() is called before init()
		return nil, fmt.Errorf("connector state not found")
	}
	return state, nil
}

func (s *BaseConnector) Cancel() {
	s.cancel()
}

func (c *BaseConnector) Init(ctx context.Context, connectorID string) error {
	if connectorID != "" {
		// connectorID is passed only when Init is called to re-create the
		// connector from a state object during initial load
		c.id = connectorID
	}
	if c.id == "" {
		c.id = uuid.New().String()
	}

	// Set up a new context for the connector
	c.context, c.cancel = context.WithCancel(ctx)

	state, err := c.store.GetConnectorState(ctx, c.ID())
	if err != nil && !store.IsStateNotFound(err) {
		return fmt.Errorf("failed to get connector state: %v", err)
	}

	if state == nil {
		state = &types.ConnectorState{}
	}

	state.ConnectorID = c.ID()
	state.Syncing = false
	// state.User is unknown until auth is complete
	state.ConnectorType = string(c.Type())
	token, err := keychain.TokenFromKeychain(c.ID(), c.Type())
	state.AuthValid = (err == nil && token != nil) // TODO: check for expiry of refresh token

	err = c.store.UpdateConnectorState(ctx, state)
	if err != nil {
		return fmt.Errorf("failed to set connector state: %v", err)
	}
	return nil
}

func (s *BaseConnector) UpdateConnectorState(ctx context.Context, state *types.ConnectorState) error {
	return s.store.UpdateConnectorState(ctx, state)
}

func emitChunks(fileName string, content string, document types.Document, chunkChan chan types.ChunkSyncResult) {
	numChunks := 0
	content = util.CleanChunk(content)
	chunkTexts := util.ChunkText(content, MaxChunkSize, ChunkOverlap)
	for _, text := range chunkTexts {
		numChunks++
		log.Printf("Processing chunk %d of document %s", numChunks, fileName)
		chunkChan <- types.ChunkSyncResult{
			Chunk: types.Chunk{
				Text:     text,
				Document: document,
			},
		}
	}
}
