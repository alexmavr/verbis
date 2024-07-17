package types

import (
	"time"
)

// Add a vector to Weaviate
type AddVectorItem struct {
	Chunk
	Vector []float32
}

type Source struct {
	Title string `json:"title"`
	URL   string `json:"url"`
	Type  string `json:"type"`
}

type HistoryItem struct {
	Role    string   `json:"role"`
	Content string   `json:"content"`
	Sources []Source `json:"sources"`
}

type ConnectorState struct {
	ConnectorID   string    `json:"connector_id"`
	User          string    `json:"user"`
	ConnectorType string    `json:"connector_type"`
	AuthValid     bool      `json:"auth_valid"`
	Syncing       bool      `json:"syncing"`
	LastSync      time.Time `json:"last_sync"`
	NumDocuments  int       `json:"num_documents"`
	NumChunks     int       `json:"num_chunks"`
	NumErrors     int       `json:"num_errors"`
}

type Chunk struct {
	Document `json:"document"`
	Text     string `json:"text"`
	Hash     string `json:"hash"`

	// The following fields are only filled in when the chunk is a search result
	Score        float64 `json:"score"`
	ExplainScore string  `json:"explain_score"`
}
type Document struct {
	UniqueID      string    `json:"unique_id"` // Uniquely identifies the document in the connector's context
	Name          string    `json:"name"`
	SourceURL     string    `json:"source_url"`
	ConnectorID   string    `json:"connector_id"`
	ConnectorType string    `json:"connector_type"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type Conversation struct {
	ID          string        `json:"id"`
	History     []HistoryItem `json:"history"`
	ChunkHashes []string      `json:"chunk_hashes"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
	Title       string        `json:"title"`
}

type BuildCredentials struct {
	PosthogAPIKey     string
	AzureSecretID     string
	AzureSecretValue  string
	SlackClientID     string
	SlackClientSecret string
	GoogleJSONCreds   string
}

type Config struct {
	ID string `json:"id"`
	// InitConfigDone  bool   `json:"init_config_done"`
	// To be used only for boot time user prompting for configs, such as opt-in
	// (right now we're opt out telemetry)

	EnableTelemetry bool `json:"enable_telemetry"`
}
