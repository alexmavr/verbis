package types

import (
	"time"
)

// Add a vector to Weaviate
type AddVectorItem struct {
	Chunk
	Vector []float32
}

type HistoryItem struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ConnectorState struct {
	Name         string    `json:"name"`
	AuthValid    bool      `json:"auth_valid"`
	Syncing      bool      `json:"syncing"`
	LastSync     time.Time `json:"last_sync"`
	NumDocuments int       `json:"num_documents"`
	NumChunks    int       `json:"num_chunks"`
}

type Chunk struct {
	Document
	Text string

	// The following fields are only filled in when the chunk is a search result
	Score        float32
	ExplainScore string
}

type Document struct {
	Name       string
	SourceURL  string
	SourceName string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}
