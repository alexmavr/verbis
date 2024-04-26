package types

import (
	"context"
)

type Connector interface {
	Name() string

	// Init runs once when the connector is initially registered
	// It is responsible for determining if the connector has valid auth from
	// other connectors, and setting the initial state. It must be called before any of the other methods
	Init(ctx context.Context) error

	Status(ctx context.Context) (*ConnectorState, error)
	AuthSetup(ctx context.Context) error
	AuthCallback(ctx context.Context, code string) error
	Sync(ctx context.Context) ([]Chunk, error)
}
