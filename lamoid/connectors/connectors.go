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
	Status(ctx context.Context) ConnectorStatus

	Init(ctx context.Context) error
	AuthCallback(ctx context.Context, code string) error
	Sync(ctx context.Context) ([]Chunk, error)
	NeedsSync(ctx context.Context) bool
}

type ConnectorStatus struct {
	Name         string    `json:"name"`
	AuthValid    bool      `json:"auth_valid"`
	LastSync     time.Time `json:"last_sync"`
	NumDocuments int       `json:"num_documents"`
	NumChunks    int       `json:"num_chunks"`
}
