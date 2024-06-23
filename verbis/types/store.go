package types

import "context"

type Store interface {
	ChunkHashExists(ctx context.Context, hash string) (bool, error)
	GetChunkByHash(ctx context.Context, hash string) (*Chunk, error)
	GetDocument(ctx context.Context, uniqueID string) (*Document, error)
	AddVectors(ctx context.Context, items []AddVectorItem) (*AddVectorResponse, error)
	HybridSearch(ctx context.Context, query string, vector []float32) ([]*Chunk, error)
	CreateDocumentClass(ctx context.Context, force bool) error
	CreateChunkClass(ctx context.Context, force bool) error
	CreateConversationClass(ctx context.Context, force bool) error
	CreateConnectorStateClass(ctx context.Context, force bool) error
	CreateConversation(ctx context.Context) (string, error)
	ListConversations(ctx context.Context) ([]*Conversation, error)
	GetConversation(ctx context.Context, conversationID string) (*Conversation, error)
	ConversationAppend(ctx context.Context, conversationID string, items []HistoryItem, chunks []*Chunk) error
	SetConnectorSyncing(ctx context.Context, connectorID string, syncing bool) (*ConnectorState, error)
	UpdateConnectorState(ctx context.Context, state *ConnectorState) error
	AllConnectorStates(ctx context.Context) ([]*ConnectorState, error)
	GetConnectorState(ctx context.Context, connectorID string) (*ConnectorState, error)
	DeleteDocumentById(ctx context.Context, documentId string) error
	DeleteDocumentChunksById(ctx context.Context, documentId string) error
	DeleteDocumentChunks(ctx context.Context, uniqueID string, connectorID string) error
	DeleteConnector(ctx context.Context, connector Connector) error
}

type AddVectorResponse struct {
	NumChunksAdded int
	NumDocsAdded   int
}
