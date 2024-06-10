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
)

type ConnectorConstructor func(BuildCredentials) Connector
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

	UpdateConnectorState(ctx context.Context, state *ConnectorState) error
	Status(ctx context.Context) (*ConnectorState, error)
	AuthSetup(ctx context.Context) error
	AuthCallback(ctx context.Context, code string) error
	Sync(ctx context.Context, lastSync time.Time, chunkChan chan ChunkSyncResult, errChan chan error)
}

type ChunkSyncResult struct {
	Chunk Chunk
	Err   error
}
