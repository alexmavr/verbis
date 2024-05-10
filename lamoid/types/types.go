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
	ConnectorID   string    `json:"connector_id"`
	ConnectorType string    `json:"connector_type"`
	AuthValid     bool      `json:"auth_valid"`
	Syncing       bool      `json:"syncing"`
	LastSync      time.Time `json:"last_sync"`
	NumDocuments  int       `json:"num_documents"`
	NumChunks     int       `json:"num_chunks"`
}

type Chunk struct {
	Document `json:"document"`
	Text     string `json:"text"`

	// The following fields are only filled in when the chunk is a search result
	Score        float64 `json:"score"`
	ExplainScore string  `json:"explain_score"`
}
type Document struct {
	Name       string    `json:"name"`
	SourceURL  string    `json:"source_url"`
	SourceName string    `json:"source_name"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}
