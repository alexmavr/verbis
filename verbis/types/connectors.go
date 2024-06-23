package types

import (
	"context"
	"time"
)

type ConnectorType string

const (
	ConnectorTypeGoogleDrive ConnectorType = "googledrive"
	ConnectorTypeGmail       ConnectorType = "gmail"
	ConnectorTypeOutlook     ConnectorType = "outlook"
	ConnectorTypeSlack       ConnectorType = "slack"
)

type ConnectorConstructor func(BuildCredentials, Store) Connector
type Connector interface {
	ID() string
	Type() ConnectorType
	User() string

	// Init runs in one of two cases:
	// 1. When the connector is first created, where connectorID is ""
	// 2. When the connector is being restored from state, where connectorID is the ID of the connector
	// It is responsible for determining if the connector has valid auth
	// and setting the initial state. It must be called before any of the other methods
	Init(ctx context.Context, connectorID string) error

	// Cancels the connector's own context
	Cancel()

	UpdateConnectorState(ctx context.Context, state *ConnectorState) error
	Status(ctx context.Context) (*ConnectorState, error)
	AuthSetup(ctx context.Context) error
	AuthCallback(ctx context.Context, code string) error
	Sync(lastSync time.Time, chunkChan chan ChunkSyncResult, errChan chan error)
}

type ChunkSyncResult struct {
	Chunk Chunk
	Err   error

	// if SkipClean is set to true, the chunk will not be cleaned by the syncer
	// This is used in connectors such as Slack where newlines are needed to
	// indicate different messages. The connector takes over the responsibility
	// of sanitizing the content appropriately with util.CleanChunk
	SkipClean bool
}
