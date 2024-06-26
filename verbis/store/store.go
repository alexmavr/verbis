package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	"github.com/weaviate/weaviate-go-client/v4/weaviate"
	"github.com/weaviate/weaviate-go-client/v4/weaviate/filters"
	"github.com/weaviate/weaviate-go-client/v4/weaviate/graphql"
	"github.com/weaviate/weaviate/entities/models"

	"github.com/verbis-ai/verbis/verbis/keychain"
	"github.com/verbis-ai/verbis/verbis/types"
)

var (
	chunkClassName        = "VerbisChunk"
	documentClassName     = "Document"
	stateClassName        = "ConnectorState"
	conversationClassName = "Conversation"
)

const (
	MaxNumSearchResults = 10
	HybridSearchAlpha   = 0.3

	// Max number of chunks per document, will break after that
	WeaviateMaxResults = 1000
)

type WeaviateStore struct {
	client              *weaviate.Client
	ollamaURL           string
	embeddingsModelName string
}

func NewWeaviateStore(ollamaURL, embeddingsModelName string) types.Store {
	return &WeaviateStore{
		client:              GetWeaviateClient(),
		ollamaURL:           ollamaURL,
		embeddingsModelName: embeddingsModelName,
	}
}

func GetWeaviateClient() *weaviate.Client {
	// Initialize Weaviate client
	return weaviate.New(weaviate.Config{
		Host:   "localhost:8088",
		Scheme: "http",
	})
}

var ErrChunkNotFound = errors.New("chunk not found")

func IsErrChunkNotFound(err error) bool {
	return errors.Is(err, ErrChunkNotFound)
}

func (w *WeaviateStore) ChunkHashExists(ctx context.Context, hash string) (bool, error) {
	chunk, err := w.GetChunkByHash(ctx, hash)
	if err != nil {
		return false, err
	}
	if chunk == nil {
		return false, nil
	}

	return true, nil
}

func (w *WeaviateStore) GetChunkByHash(ctx context.Context, hash string) (*types.Chunk, error) {
	where := filters.Where().
		WithPath([]string{"hash"}).
		WithOperator(filters.Equal).
		WithValueString(hash)

	resp, err := w.client.GraphQL().Get().
		WithClassName(chunkClassName).
		WithFields([]graphql.Field{
			{Name: "hash"},
			{Name: "documentid"},
			{Name: "document_title"},
			{Name: "chunk"},
		}...).
		WithWhere(where).
		Do(ctx)
	if err != nil {
		return nil, err
	}

	if resp.Data["Get"] == nil {
		return nil, ErrChunkNotFound
	}

	get := resp.Data["Get"].(map[string]interface{})
	chunks, ok := get[chunkClassName].([]interface{})
	if !ok || len(chunks) == 0 {
		return nil, ErrChunkNotFound
	}

	if len(chunks) > 1 {
		return nil, fmt.Errorf("found %d chunks instead of 1", len(chunks))
	}

	parsedChunks, err := parseChunks(ctx, w.client, chunks, false)
	if err != nil {
		return nil, err
	}

	return parsedChunks[0], nil
}

var ErrDocumentNotFound = errors.New("document not found")

func IsErrDocumentNotFound(err error) bool {
	return errors.Is(err, ErrDocumentNotFound)
}

func (w *WeaviateStore) GetDocument(ctx context.Context, uniqueID string) (*types.Document, error) {
	docID, err := getDocumentIDFromUniqueID(ctx, w.client, uniqueID)
	if err != nil {
		return nil, fmt.Errorf("unable to get document ID: %v", err)
	}
	if docID == "" {
		return nil, ErrDocumentNotFound
	}

	docData, err := getDocument(ctx, w.client, docID)
	if err != nil {
		return nil, fmt.Errorf("unable to get document: %s", err)
	}
	createdAt, _ := time.Parse(time.RFC3339, docData["createdAt"].(string))
	updatedAt, _ := time.Parse(time.RFC3339, docData["updatedAt"].(string))

	doc := &types.Document{
		Name:          docData["name"].(string),
		SourceURL:     docData["sourceURL"].(string),
		ConnectorID:   docData["connectorID"].(string),
		ConnectorType: docData["connectorType"].(string),
		Summary:       docData["summary"].(string),
		CreatedAt:     createdAt,
		UpdatedAt:     updatedAt,
	}
	return doc, nil
}

func (w *WeaviateStore) SetDocumentSummary(ctx context.Context, uniqueID string, summary string) error {
	docID, err := getDocumentIDFromUniqueID(ctx, w.client, uniqueID)
	if err != nil {
		return fmt.Errorf("unable to get document ID: %v", err)
	}
	if docID == "" {
		return ErrDocumentNotFound
	}

	docData, err := getDocument(ctx, w.client, docID)
	if err != nil {
		return fmt.Errorf("unable to get document: %s", err)
	}
	createdAt, _ := time.Parse(time.RFC3339, docData["createdAt"].(string))
	updatedAt, _ := time.Parse(time.RFC3339, docData["updatedAt"].(string))

	doc := &types.Document{
		Name:          docData["name"].(string),
		SourceURL:     docData["sourceURL"].(string),
		ConnectorID:   docData["connectorID"].(string),
		ConnectorType: docData["connectorType"].(string),
		Summary:       summary,
		CreatedAt:     createdAt,
		UpdatedAt:     updatedAt,
	}

	properties := map[string]interface{}{
		"unique_id":     doc.UniqueID,
		"name":          doc.Name,
		"sourceURL":     doc.SourceURL,
		"connectorID":   doc.ConnectorID,
		"connectorType": doc.ConnectorType,
		"summary":       summary,
		"createdAt":     doc.CreatedAt.Format(time.RFC3339),
		"updatedAt":     doc.UpdatedAt.Format(time.RFC3339),
	}
	err = w.client.Data().Updater(). // replaces the entire object
						WithID(docID).
						WithClassName(documentClassName).
						WithProperties(properties).
						Do(ctx)
	return err
}

func (w *WeaviateStore) GetDocumentChunkTexts(ctx context.Context, uniqueID string) ([]string, error) {
	docID, err := getDocumentIDFromUniqueID(ctx, w.client, uniqueID)
	if err != nil {
		return nil, fmt.Errorf("unable to get document ID: %v", err)
	}

	where := filters.Where().
		WithPath([]string{"documentid"}).
		WithOperator(filters.Equal).
		WithValueString(docID)

	resp, err := w.client.GraphQL().
		Get().
		WithClassName(chunkClassName).
		WithWhere(where).
		WithLimit(WeaviateMaxResults).
		WithFields([]graphql.Field{
			{Name: "chunk"},
		}...).
		Do(ctx)
	if err != nil {
		return nil, err
	}

	if resp.Data["Get"] == nil {
		return nil, fmt.Errorf("no chunks found")
	}
	get := resp.Data["Get"].(map[string]interface{})
	if get[chunkClassName] == nil {
		// return empty result
		return nil, nil
	}

	chunks := get[chunkClassName].([]interface{})
	texts := []string{}
	for _, chunkMap := range chunks {
		c := chunkMap.(map[string]interface{})

		// Retrieve and parse document details from the linked Document
		text, ok := c["chunk"].(string)
		if !ok {
			return nil, fmt.Errorf("unable to assert chunk text as string")
		}
		texts = append(texts, text)
	}

	return texts, nil
}

func getDocumentIDFromUniqueID(ctx context.Context, client *weaviate.Client, uniqueID string) (string, error) {
	where := filters.Where().
		WithPath([]string{"unique_id"}).
		WithOperator(filters.Equal).
		WithValueString(uniqueID)

	resp, err := client.GraphQL().Get().
		WithClassName(documentClassName).
		WithFields(
			[]graphql.Field{
				{
					Name: "unique_id",
				},
				{
					Name: "_additional",
					Fields: []graphql.Field{
						{Name: "id"},
					},
				},
			}...,
		).
		WithWhere(where).
		Do(ctx)
	if err != nil {
		return "", err
	}

	if resp.Data["Get"] == nil {
		return "", nil
	}

	get := resp.Data["Get"].(map[string]interface{})
	docs, ok := get[documentClassName].([]interface{})
	if !ok {
		return "", nil
	}

	for _, doc := range docs {
		m, ok := doc.(map[string]interface{})
		if !ok {
			log.Printf("Failed to parse document: %v\n", doc)
			continue
		}

		storedID, ok := m["unique_id"].(string)
		if !ok {
			log.Printf("Failed to parse unique_id: %v\n", m)
			continue
		}
		if storedID == uniqueID {
			return m["_additional"].(map[string]interface{})["id"].(string), nil
		}
	}
	return "", nil
}

func (w *WeaviateStore) AddVectors(ctx context.Context, items []types.AddVectorItem) (*types.AddVectorResponse, error) {
	objects := []*models.Object{}

	for _, item := range items {
		// Look if a document with the same ID exists
		docID, err := getDocumentIDFromUniqueID(ctx, w.client, item.Document.UniqueID)
		if err != nil {
			return nil, fmt.Errorf("unable to get document ID: %v", err)
		}

		var documentObj *models.Object
		if docID == "" {
			docID = uuid.NewString()
			// Create a new document if it does not exist
			log.Printf("Creating new document with ID: %s and Name: %s\n", docID, item.Document.Name)
			documentObj = &models.Object{
				Class: documentClassName,
				ID:    strfmt.UUID(docID),
				Properties: map[string]interface{}{
					"unique_id":     item.Document.UniqueID,
					"name":          item.Document.Name,
					"sourceURL":     item.Document.SourceURL,
					"connectorID":   item.Document.ConnectorID,
					"connectorType": item.Document.ConnectorType,
					"summary":       "", // To be populated when the document is summarized
					"createdAt":     item.Document.CreatedAt.Format(time.RFC3339),
					"updatedAt":     item.Document.UpdatedAt.Format(time.RFC3339),
				},
			}
			objects = append(objects, documentObj)
		}

		// TODO: if the provided document sourceURL is different from the stored one, update it

		// Create a new chunk
		chunkObj := &models.Object{
			Class: chunkClassName,
			ID:    strfmt.UUID(uuid.NewString()),
			Properties: map[string]interface{}{
				"chunk":          item.Chunk.Text,
				"hash":           item.Chunk.Hash,
				"documentid":     docID,
				"document_title": item.Document.Name, // Stored both here and in document, to facilitate hybrid search
			},
		}
		objects = append(objects, chunkObj)
	}

	_, err := w.client.Batch().ObjectsBatcher().WithObjects(objects...).Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to batch objects: %v", err)
	}

	return &types.AddVectorResponse{
		NumChunksAdded: len(items),
		NumDocsAdded:   len(objects) - len(items), // Total set of objects created versus the known num of chunks
	}, nil
}

func getDocument(ctx context.Context, client *weaviate.Client, docid string) (map[string]interface{}, error) {
	docs, err := client.Data().ObjectsGetter().
		WithClassName(documentClassName).
		WithID(docid).
		Do(ctx)
	if err != nil {
		return nil, err
	}

	if len(docs) == 0 {
		return nil, fmt.Errorf("document with id %s not found", docid)
	}

	return docs[0].Properties.(map[string]interface{}), nil
}

// Search for a vector in Weaviate
func (w *WeaviateStore) HybridSearch(ctx context.Context, query string, vector []float32) ([]*types.Chunk, error) {
	fmt.Println("Query vector length: ", len(vector))

	_chunk_fields := []graphql.Field{
		{Name: "chunk"},
		{Name: "hash"},
		{Name: "documentid"},
		{Name: "document_title"},
		{Name: "_additional", Fields: []graphql.Field{
			{Name: "score"},
			{Name: "explainScore"},
		}},
	}

	log.Printf("Searching for chunks with query: %s\n", query)
	hybrid := w.client.GraphQL().HybridArgumentBuilder().
		WithQuery(query).
		WithVector(vector).
		WithAlpha(HybridSearchAlpha).
		WithProperties([]string{"chunk", "document_title^2"}).
		WithFusionType(graphql.RelativeScore)

	resp, err := w.client.GraphQL().
		Get().
		WithClassName(chunkClassName).
		WithHybrid(hybrid).
		WithLimit(MaxNumSearchResults).
		WithFields(_chunk_fields...).
		Do(ctx)
	if err != nil {
		return nil, err
	}

	log.Print(resp.Data["Get"])
	if resp.Data["Get"] == nil {
		return nil, fmt.Errorf("no chunks found")
	}
	get := resp.Data["Get"].(map[string]interface{})
	if get[chunkClassName] == nil {
		// return empty result
		return []*types.Chunk{}, nil
	}

	return parseChunks(ctx, w.client, get[chunkClassName].([]interface{}), true)
}

func parseChunks(ctx context.Context, client *weaviate.Client, chunks []interface{}, withScore bool) ([]*types.Chunk, error) {
	res := []*types.Chunk{}
	score := 0.0
	var err error
	for _, chunkMap := range chunks {
		c := chunkMap.(map[string]interface{})

		// Parse additional info
		if withScore {
			addl := c["_additional"].(map[string]interface{})
			scoreStr := addl["score"].(string)
			log.Printf("ScoreStr: %s\n", scoreStr)
			score, err = strconv.ParseFloat(scoreStr, 64)
			if err != nil {
				log.Printf("Failed to parse score: %s\n", err)
				continue
			}
		}

		// Retrieve and parse document details from the linked Document
		docid, ok := c["documentid"].(string)
		if !ok {
			return nil, fmt.Errorf("documentid is nil")
		}
		docData, err := getDocument(ctx, client, docid)
		if err != nil {
			return nil, fmt.Errorf("failed to get document: %v", err)
		}
		createdAt, _ := time.Parse(time.RFC3339, docData["createdAt"].(string))
		updatedAt, _ := time.Parse(time.RFC3339, docData["updatedAt"].(string))

		chunk := &types.Chunk{
			Document: types.Document{
				Name:          docData["name"].(string),
				SourceURL:     docData["sourceURL"].(string),
				ConnectorID:   docData["connectorID"].(string),
				ConnectorType: docData["connectorType"].(string),
				Summary:       docData["summary"].(string),
				CreatedAt:     createdAt,
				UpdatedAt:     updatedAt,
			},
			Text: c["chunk"].(string),
			Hash: c["hash"].(string),
			// Document Title is not exported separately, although it's stored in the chunk
		}
		if withScore {
			chunk.Score = score
		}
		res = append(res, chunk)
	}
	return res, nil
}

func (w *WeaviateStore) CreateDocumentClass(ctx context.Context, force bool) error {
	// DEBUG: attempt to delete the class, don't fail if it doesn't exist
	if force {
		w.client.Schema().ClassDeleter().WithClassName(documentClassName).Do(ctx)
	}

	class := &models.Class{
		Class:      documentClassName,
		Vectorizer: "none",
		Properties: []*models.Property{
			{
				Name:     "name",
				DataType: []string{"text"},
			},
			{
				Name:     "sourceURL",
				DataType: []string{"text"},
			},
			{
				Name:     "connectorID",
				DataType: []string{"text"},
			},
			{
				Name:     "connectorType",
				DataType: []string{"text"},
			},
			{
				Name:     "createdAt",
				DataType: []string{"date"},
			},
			{
				Name:     "updatedAt",
				DataType: []string{"date"},
			},
			{
				Name:     "summary",
				DataType: []string{"text"},
			},
		},
	}

	// Create the class in Weaviate
	err := w.client.Schema().ClassCreator().WithClass(class).Do(ctx)
	if err != nil {
		return fmt.Errorf("failed to create chunk class: %v", err)
	}

	// Create the class in Weaviate
	return nil
}

func (w *WeaviateStore) CreateChunkClass(ctx context.Context, force bool) error {
	// DEBUG: attempt to delete the class, don't fail if it doesn't exist
	if force {
		w.client.Schema().ClassDeleter().WithClassName(chunkClassName).Do(ctx)
	}

	class := &models.Class{
		Class:      chunkClassName,
		Vectorizer: "text2vec-ollama",
		ModuleConfig: map[string]map[string]string{
			"text2vec-ollama": {
				"apiEndpoint": w.ollamaURL,
				"model":       w.embeddingsModelName,
			},
		},
		Properties: []*models.Property{
			{
				Name:     "chunk",
				DataType: []string{"text"},
			},
			{
				Name:     "hash",
				DataType: []string{"text"},
			},
			{
				Name:     "documentid",
				DataType: []string{"text"},
			},
			{
				Name:     "document_title", // Stored both here and in document, to facilitate hybrid search
				DataType: []string{"text"},
			},
		},
	}

	// Create the class in Weaviate
	err := w.client.Schema().ClassCreator().WithClass(class).Do(ctx)
	if err != nil {
		return fmt.Errorf("failed to create chunk class: %v", err)
	}

	return nil
}

func (w *WeaviateStore) CreateConversation(ctx context.Context) (string, error) {
	// Create a new conversation object
	conversationID := uuid.NewString()
	_, err := w.client.Data().Creator().WithClassName(conversationClassName).WithID(conversationID).
		WithProperties(map[string]interface{}{
			"history":    []interface{}{},
			"chunks":     []interface{}{},
			"created_at": time.Now().Format(time.RFC3339),
			"updated_at": time.Now().Format(time.RFC3339),
			"title":      "", // TODO: Dynamically create conversation title based on first prompt
		}).Do(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to create conversation: %v", err)
	}

	return conversationID, nil
}

var ErrConversationNotFound = errors.New("conversation not found")

func IsErrConversationNotFound(err error) bool {
	return errors.Is(err, ErrConversationNotFound)
}

func (w *WeaviateStore) ListConversations(ctx context.Context) ([]*types.Conversation, error) {
	// TODO: Exclude 'history' and 'chunks' from list response. For long living convos this can really bulk up the response. Clients should be able to retrieve these via GET on individual convos instead. Excluding requires some refactoring since parseConversation method breaks currently.
	resp, err := w.client.GraphQL().Get().
		WithClassName(conversationClassName).
		WithFields(
			[]graphql.Field{
				{
					Name: "history",
				},
				{
					Name: "chunks",
				},
				{
					Name: "created_at",
				},
				{
					Name: "updated_at",
				},
				{
					Name: "title",
				},
				{
					Name: "_additional",
					Fields: []graphql.Field{
						{Name: "id"},
					},
				},
			}...,
		).
		Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list conversations: %v", err)
	}

	if resp.Data["Get"] == nil {
		return []*types.Conversation{}, nil
	}

	get := resp.Data["Get"].(map[string]interface{})
	if get[conversationClassName] == nil {
		return []*types.Conversation{}, nil
	}

	conversations := get[conversationClassName].([]interface{})
	resConversations := []*types.Conversation{}
	for _, conversation := range conversations {
		cMap := conversation.(map[string]interface{})
		res, err := parseConversation("", cMap)
		if err != nil {
			return nil, fmt.Errorf("failed to parse conversation: %v", err)
		}
		resConversations = append(resConversations, res)
	}

	return resConversations, nil
}

func (w *WeaviateStore) GetConversation(ctx context.Context, conversationID string) (*types.Conversation, error) {
	log.Printf("Looking for conversation with ID %s\n", conversationID)
	conversations, err := w.client.Data().ObjectsGetter().
		WithClassName(conversationClassName).
		WithID(conversationID).
		Do(ctx)
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			return nil, ErrConversationNotFound
		}
		return nil, fmt.Errorf("failed to get conversation with id %s: %v", conversationID, err)
	}
	if len(conversations) == 0 {
		return nil, ErrConversationNotFound
	}

	if len(conversations) > 1 {
		return nil, fmt.Errorf("found %d conversations instead of 1", len(conversations))
	}

	conv := conversations[0].Properties.(map[string]interface{})
	return parseConversation(conversationID, conv)
}

func parseConversation(conversationID string, conversationMap map[string]interface{}) (*types.Conversation, error) {
	if conversationID == "" {
		conversationID = conversationMap["_additional"].(map[string]interface{})["id"].(string)
	}
	conversationChunks := conversationMap["chunks"].([]interface{})
	conversationHistory := conversationMap["history"].([]interface{})

	chunkHashes := []string{}
	for _, chunk := range conversationChunks {
		chunkHashes = append(chunkHashes, chunk.(string))
	}

	historyItems := []types.HistoryItem{}
	for _, item := range conversationHistory {
		historyItem := types.HistoryItem{}
		err := json.Unmarshal([]byte(item.(string)), &historyItem)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal history item: %v", err)
		}

		historyItems = append(historyItems, historyItem)
	}

	createdAtStr := conversationMap["created_at"].(string)
	createdAt, err := time.Parse(time.RFC3339, createdAtStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse created_at time: %v", err)
	}

	updatedAtStr := conversationMap["updated_at"].(string)
	updatedAt, err := time.Parse(time.RFC3339, updatedAtStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse created_at time: %v", err)
	}

	title := conversationMap["title"]
	if title == nil {
		title = ""
	}

	conversation := &types.Conversation{
		ID:          conversationID,
		History:     historyItems,
		ChunkHashes: chunkHashes,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
		Title:       title.(string),
	}
	return conversation, nil
}

func (w *WeaviateStore) ConversationAppend(ctx context.Context, conversationID string, items []types.HistoryItem, chunks []*types.Chunk) error {
	conversation, err := w.GetConversation(ctx, conversationID)
	if err != nil {
		return fmt.Errorf("unable to get conversation: %v", err)
	}

	// Add chunk hashes to the conversation
	for _, chunk := range chunks {
		conversation.ChunkHashes = append(conversation.ChunkHashes, chunk.Hash)
	}
	conversation.History = append(conversation.History, items...)

	// Convert each HistoryItem to a JSON string
	jsonHistory := make([]string, len(conversation.History))
	for i, item := range conversation.History {
		historyItemJSON, err := json.Marshal(item)
		if err != nil {
			return fmt.Errorf("failed to marshal history item: %v", err)
		}
		jsonHistory[i] = string(historyItemJSON)
	}

	err = w.client.Data().Updater(). // replaces the entire object
						WithID(conversationID).
						WithClassName(conversationClassName).
						WithProperties(map[string]interface{}{
			"history":    jsonHistory,
			"chunks":     conversation.ChunkHashes,
			"updated_at": time.Now().Format(time.RFC3339),
			"created_at": conversation.CreatedAt,
		}).
		Do(ctx)
	if err != nil {
		return fmt.Errorf("failed to update conversation: %v", err)
	}

	return nil
}

func (w *WeaviateStore) CreateConversationClass(ctx context.Context, force bool) error {
	if force {
		w.client.Schema().ClassDeleter().WithClassName(conversationClassName).Do(ctx)
	}

	class := &models.Class{
		Class:      conversationClassName,
		Vectorizer: "none",
		Properties: []*models.Property{
			{
				Name:     "history",
				DataType: []string{"text[]"},
			},
			{
				Name:     "chunks",
				DataType: []string{"text[]"},
			},
			{
				Name:     "created_at",
				DataType: []string{"date"},
			},
			{
				Name:     "updated_at",
				DataType: []string{"date"},
			},
			{
				Name:     "title",
				DataType: []string{"text"},
			},
		},
	}

	// Create the class in Weaviate
	return w.client.Schema().ClassCreator().WithClass(class).Do(ctx)
}

// Create a Weaviate class schema for the connector state
func (w *WeaviateStore) CreateConnectorStateClass(ctx context.Context, force bool) error {
	// DEBUG: attempt to delete the class, don't fail if it doesn't exist
	if force {
		w.client.Schema().ClassDeleter().WithClassName(stateClassName).Do(ctx)
	}

	class := &models.Class{
		Class:      stateClassName,
		Vectorizer: "none",
		Properties: []*models.Property{
			{
				Name:     "connector_id",
				DataType: []string{"text"},
			},
			{
				Name:     "type",
				DataType: []string{"text"},
			},
			{
				Name:     "user",
				DataType: []string{"text"},
			},
			{
				Name:     "syncing",
				DataType: []string{"boolean"},
			},
			{
				Name:     "auth_valid",
				DataType: []string{"boolean"},
			},
			{
				Name:     "lastSync",
				DataType: []string{"date"},
			},
			{
				Name:     "numDocuments",
				DataType: []string{"int"},
			},
			{
				Name:     "numChunks",
				DataType: []string{"int"},
			},
			{
				Name:     "numErrors",
				DataType: []string{"int"},
			},
		},
	}

	// Create the class in Weaviate
	return w.client.Schema().ClassCreator().WithClass(class).Do(ctx)
}

var ErrSyncingAlreadyExpected = errors.New("syncing is already at the expected value")

func IsSyncingAlreadyExpected(err error) bool {
	return errors.Is(err, ErrSyncingAlreadyExpected)
}

func (w *WeaviateStore) SetConnectorSyncing(ctx context.Context, connectorID string, syncing bool) (*types.ConnectorState, error) {
	state, err := w.GetConnectorState(ctx, connectorID)
	if err != nil {
		return nil, fmt.Errorf("unable to get connector state: %s", err)
	}

	if state.Syncing == syncing {
		return state, ErrSyncingAlreadyExpected
	}

	state.Syncing = syncing
	err = w.UpdateConnectorState(ctx, state)
	return state, err
}

// Add or update the connector state in Weaviate
func (w *WeaviateStore) UpdateConnectorState(ctx context.Context, state *types.ConnectorState) error {
	where := filters.Where().
		WithPath([]string{"connector_id"}).
		WithOperator(filters.Equal).
		WithValueString(state.ConnectorID)

	resp, err := w.client.GraphQL().Get().
		WithClassName(stateClassName).
		WithFields([]graphql.Field{
			{Name: "_additional", Fields: []graphql.Field{{Name: "id"}}},
		}...).
		WithWhere(where).
		Do(ctx)
	if err != nil {
		return err
	}

	if resp.Data["Get"] == nil || len(resp.Data["Get"].(map[string]interface{})[stateClassName].([]interface{})) == 0 {
		log.Printf("Creating new connector state for %s %s", state.ConnectorType, state.ConnectorID)
		_, err := w.client.Data().Creator().WithClassName(stateClassName).WithProperties(map[string]interface{}{
			"connector_id": state.ConnectorID,
			"type":         state.ConnectorType,
			"user":         state.User,
			"syncing":      state.Syncing,
			"auth_valid":   state.AuthValid,
			"lastSync":     state.LastSync,
			"numDocuments": state.NumDocuments,
			"numChunks":    state.NumChunks,
			"numErrors":    state.NumErrors,
		}).
			WithID(state.ConnectorID).
			Do(ctx)
		return err
	}

	get := resp.Data["Get"].(map[string]interface{})
	states := get["ConnectorState"].([]interface{})
	c := states[0].(map[string]interface{})
	addl := c["_additional"].(map[string]interface{})
	objID := addl["id"].(string)

	err = w.client.Data().Updater(). // replaces the entire object
						WithID(objID).
						WithClassName(stateClassName).
						WithProperties(map[string]interface{}{
			"connector_id": state.ConnectorID,
			"type":         state.ConnectorType,
			"user":         state.User,
			"syncing":      state.Syncing,
			"auth_valid":   state.AuthValid,
			"lastSync":     state.LastSync,
			"numDocuments": state.NumDocuments,
			"numChunks":    state.NumChunks,
			"numErrors":    state.NumErrors,
		}).
		Do(ctx)

	return err
}

// Fetches all stored connector states from Weaviate, used to initialize the syncer after restart
func (w *WeaviateStore) AllConnectorStates(ctx context.Context) ([]*types.ConnectorState, error) {
	resp, err := w.client.GraphQL().Get().
		WithClassName(stateClassName).
		WithFields(
			[]graphql.Field{
				{Name: "connector_id"},
				{Name: "type"},
				{Name: "user"},
				{Name: "syncing"},
				{Name: "auth_valid"},
				{Name: "lastSync"},
				{Name: "numDocuments"},
				{Name: "numChunks"},
				{Name: "numErrors"},
			}...).
		Do(ctx)
	if err != nil {
		return nil, err
	}

	if resp.Data["Get"] == nil {
		return nil, nil
	}

	get := resp.Data["Get"].(map[string]interface{})
	states, ok := get["ConnectorState"].([]interface{})
	if !ok {
		return nil, nil
	}

	if len(states) == 0 {
		return nil, nil
	}

	res := []*types.ConnectorState{}
	for _, state := range states {
		c := state.(map[string]interface{})
		lastSync, err := time.Parse(time.RFC3339, c["lastSync"].(string))
		if err != nil {
			log.Printf("Failed to parse last sync time: %s\n", err)
		}
		res = append(res, &types.ConnectorState{
			ConnectorID:   c["connector_id"].(string),
			ConnectorType: c["type"].(string),
			User:          c["user"].(string),
			Syncing:       c["syncing"].(bool),
			AuthValid:     c["auth_valid"].(bool),
			LastSync:      lastSync,
			NumDocuments:  int(c["numDocuments"].(float64)),
			NumChunks:     int(c["numChunks"].(float64)),
			NumErrors:     int(c["numErrors"].(float64)),
		})
	}
	return res, nil
}

var ErrNoStateFound = errors.New("no connector state object found")

func IsStateNotFound(err error) bool {
	return errors.Is(err, ErrNoStateFound)
}

// Retrieve the connector state from Weaviate. Does not return AuthValid as it
// can be inferred from the presence of a token in keychain
func (w *WeaviateStore) GetConnectorState(ctx context.Context, connectorID string) (*types.ConnectorState, error) {
	where := filters.Where().
		WithPath([]string{"connector_id"}).
		WithOperator(filters.Equal).
		WithValueString(connectorID)

	resp, err := w.client.GraphQL().Get().
		WithClassName(stateClassName).
		WithFields(
			[]graphql.Field{
				{Name: "connector_id"},
				{Name: "type"},
				{Name: "user"},
				{Name: "syncing"},
				{Name: "auth_valid"},
				{Name: "lastSync"},
				{Name: "numDocuments"},
				{Name: "numChunks"},
				{Name: "numErrors"},
			}...).
		WithWhere(where).
		Do(ctx)
	if err != nil {
		return nil, err
	}

	if resp.Data["Get"] == nil {
		return nil, nil
	}

	get := resp.Data["Get"].(map[string]interface{})
	states, ok := get[stateClassName].([]interface{})
	if !ok {
		return nil, fmt.Errorf("no connector state class found")
	}

	if len(states) == 0 {
		return nil, ErrNoStateFound // This is handled gracefully during init
	}

	if len(states) > 1 {
		return nil, fmt.Errorf("multiple connector state objects found")
	}

	c := states[0].(map[string]interface{})
	lastSync, err := time.Parse(time.RFC3339, c["lastSync"].(string))
	if err != nil {
		log.Printf("Failed to parse last sync time: %s\n", err)
	}

	return &types.ConnectorState{
		ConnectorID:   c["connector_id"].(string),
		ConnectorType: c["type"].(string),
		User:          c["user"].(string),
		Syncing:       c["syncing"].(bool),
		AuthValid:     c["auth_valid"].(bool),
		LastSync:      lastSync,
		NumDocuments:  int(c["numDocuments"].(float64)),
		NumChunks:     int(c["numChunks"].(float64)),
		NumErrors:     int(c["numErrors"].(float64)),
	}, nil
}

func (w *WeaviateStore) DeleteDocumentById(ctx context.Context, documentId string) error {
	// Cascade delete children chunks
	w.DeleteDocumentChunksById(ctx, documentId)

	// Delete the document
	docDeleteErr := w.client.Data().Deleter().
		WithClassName(documentClassName).
		WithID(documentId).
		Do(ctx)

	if docDeleteErr != nil {
		return fmt.Errorf("unable to delete document: %v", docDeleteErr)
	}
	log.Printf("Deleted document %s", documentId)

	return nil
}

func (w *WeaviateStore) DeleteDocumentChunksById(ctx context.Context, documentId string) error {
	// Note: By default max objects that can be deleted is 10K
	// Reference: https://weaviate.io/developers/weaviate/manage-data/delete#delete-multiple-objects
	response, err := w.client.Batch().ObjectsBatchDeleter().
		WithClassName(chunkClassName).
		WithOutput("verbose").
		WithWhere(filters.Where().
			WithPath([]string{"documentid"}).
			WithOperator(filters.Equal).
			WithValueText(documentId)).
		Do(ctx)

	if err != nil {
		return fmt.Errorf("unable to delete chunks: %v", err)
	}
	log.Printf("For Document %s, deleted %v chunks", documentId, response.Results.Successful)
	return nil
}

func (w *WeaviateStore) DeleteDocumentChunks(ctx context.Context, uniqueID string, connectorID string) error {
	docid, err := getDocumentIDFromUniqueID(ctx, w.client, uniqueID)
	if err != nil {
		return err
	}

	if docid == "" {
		// Document doesn't already exist, skip
		return nil
	}

	resp, err := w.client.Batch().ObjectsBatchDeleter().
		WithClassName(chunkClassName).
		WithOutput("verbose").
		WithWhere(filters.Where().
			WithPath([]string{"documentid"}).
			WithOperator(filters.Equal).
			WithValueText(docid)).
		Do(ctx)
	if err != nil {
		return fmt.Errorf("unable to delete chunks: %v", err)
	}

	log.Printf("%+v", resp)

	numDeletedChunks := resp.Results.Successful

	// Reduce the chunk count for the connector
	state, err := w.GetConnectorState(ctx, connectorID)
	if err != nil {
		return fmt.Errorf("unable to get connector state: %v", err)
	}

	if state == nil {
		return fmt.Errorf("connector state not found, unable to update chunk count")
	}

	state.NumChunks = state.NumChunks - int(numDeletedChunks)
	err = w.UpdateConnectorState(ctx, state)
	if err != nil {
		return fmt.Errorf("unable to update connector state: %v", err)
	}

	return nil
}

func (w *WeaviateStore) DeleteConnector(ctx context.Context, connector types.Connector) error {
	// TODO Mark connector for deletion. Cancel ongoing syncs, and exclude from future ones
	connectorID := connector.ID()

	// Collect documents for connector
	where := filters.Where().
		WithPath([]string{"connectorID"}).
		WithOperator(filters.Equal).
		WithValueString(connectorID)

	limit := 100
	offset := 0

	for {
		docs, err := w.client.GraphQL().Get().
			WithClassName(documentClassName).
			WithFields([]graphql.Field{
				{
					Name: "_additional",
					Fields: []graphql.Field{
						{Name: "id"},
					},
				},
			}...).
			WithWhere(where).
			WithLimit(limit).
			WithOffset(offset).
			Do(ctx)

		if err != nil {
			return err
		}

		// No documents found
		if docs.Data["Get"] == nil {
			log.Println("No documents found for connector:", connectorID)
			break
		} else {
			// Ensure docs.Data["Get"] is a map
			getData, ok := docs.Data["Get"].(map[string]interface{})
			if !ok {
				return fmt.Errorf("failed to assert Get data as map[string]interface{}")
			}

			// Ensure getData[documentClassName] is a slice
			classDocs, ok := getData[documentClassName].([]interface{})
			if !ok {
				return fmt.Errorf("failed to assert class documents as []interface{}")
			}

			// print number of documents found
			log.Printf("Number of documents found: %v", len(classDocs))
			if len(classDocs) == 0 {
				break
			}

			// Iterate over the documents
			for _, doc := range classDocs {
				// Ensure each doc is a map
				document, ok := doc.(map[string]interface{})
				if !ok {
					log.Println("Failed to assert document as map[string]interface{}")
					continue
				}

				// Ensure unique_id is a string
				documentId, ok := document["_additional"].(map[string]interface{})["id"].(string)
				if !ok {
					log.Println("Failed to assert unique_id as string")
					continue
				}

				// Delete document
				w.DeleteDocumentById(ctx, documentId)
			}
		}
	}

	// Delete connector now that all docs and chunks were deleted
	connectorDeleteWhere := filters.Where().
		WithPath([]string{"connector_id"}).
		WithOperator(filters.Equal).
		WithValueString(connectorID)
	_, connectorDeletionErr := w.client.Batch().ObjectsBatchDeleter().
		WithClassName(stateClassName).
		WithOutput("verbose").
		WithWhere(connectorDeleteWhere).
		Do(ctx)
	if connectorDeletionErr != nil {
		log.Printf("Failed to delete connector %s: %v", connectorID, connectorDeletionErr)
	}

	// TODO Delete credentials for connector
	keychainDeletionErr := keychain.DeleteTokenFromKeychain(connectorID, connector.Type())
	if keychainDeletionErr != nil {
		return fmt.Errorf("failed to delete credentials for connector %s: %v", connectorID, keychainDeletionErr)
	}
	return nil
}
