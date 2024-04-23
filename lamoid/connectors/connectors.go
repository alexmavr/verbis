package connectors

import (
	"context"
	"time"
)

type Chunk struct {
	Document
	Text string
}

type Document struct {
	SourceURL  string
	SourceName string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type Connector interface {
	Name() string

	Init(ctx context.Context) error
	AuthCallback(ctx context.Context, code string) error
	Sync(ctx context.Context) ([]Chunk, error)
	NeedsSync(ctx context.Context) bool
}
