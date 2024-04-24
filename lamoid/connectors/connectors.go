package connectors

import (
	"context"

	"github.com/epochlabs-ai/lamoid/lamoid/types"
)

type Connector interface {
	Name() string
	Status(ctx context.Context) (types.ConnectorState, error)

	Init(ctx context.Context) error
	AuthCallback(ctx context.Context, code string) error
	Sync(ctx context.Context) ([]types.Chunk, error)
	NeedsSync(ctx context.Context) bool
}
