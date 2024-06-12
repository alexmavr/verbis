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
)

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

func ChunkHashExists(ctx context.Context, client *weaviate.Client, hash string) (bool, error) {
	chunk, err := GetChunkByHash(ctx, client, hash)
	if err != nil {
		return false, err
	}
	if chunk == nil {
		return false, nil
	}

	return true, nil
}

func GetChunkByHash(ctx context.Context, client *weaviate.Client, hash string) (*types.Chunk, error) {
	where := filters.Where().
		WithPath([]string{"hash"}).
		WithOperator(filters.Equal).
		WithValueString(hash)

	resp, err := client.GraphQL().Get().
		WithClassName(chunkClassName).
		WithFields([]graphql.Field{
			{Name: "hash"},
			{Name: "documentid"},
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

	parsedChunks, err := parseChunks(ctx, client, chunks, false)
	if err != nil {
		return nil, err
	}

	return parsedChunks[0], nil
}

var ErrDocumentNotFound = errors.New("document not found")

func IsErrDocumentNotFound(err error) bool {
	return errors.Is(err, ErrDocumentNotFound)
}

func GetDocument(ctx context.Context, uniqueID string) (*types.Document, error) {
	client := GetWeaviateClient()
	docID, err := getDocumentIDFromUniqueID(ctx, client, uniqueID)
	if err != nil {
		return nil, fmt.Errorf("unable to get document ID: %v", err)
	}
	if docID == "" {
		return nil, ErrDocumentNotFound
	}

	docData, err := getDocument(ctx, client, docID)
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
		CreatedAt:     createdAt,
		UpdatedAt:     updatedAt,
	}
	return doc, nil
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

type AddVectorResponse struct {
	NumChunksAdded int
	NumDocsAdded   int
}

func AddVectors(ctx context.Context, client *weaviate.Client, items []types.AddVectorItem) (*AddVectorResponse, error) {
	objects := []*models.Object{}

	for _, item := range items {
		// Look if a document with the same ID exists
		docID, err := getDocumentIDFromUniqueID(ctx, client, item.Document.UniqueID)
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
					"createdAt":     item.Document.CreatedAt.Format(time.RFC3339),
					"updatedAt":     item.Document.UpdatedAt.Format(time.RFC3339),
				},
			}
			objects = append(objects, documentObj)
		}

		// Create a new chunk
		chunkObj := &models.Object{
			Class: chunkClassName,
			ID:    strfmt.UUID(uuid.NewString()),
			Properties: map[string]interface{}{
				"chunk":      item.Chunk.Text,
				"hash":       item.Chunk.Hash,
				"documentid": docID,
			},
			Vector: item.Vector,
		}
		objects = append(objects, chunkObj)
	}

	_, err := client.Batch().ObjectsBatcher().WithObjects(objects...).Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to batch objects: %v", err)
	}

	return &AddVectorResponse{
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
func HybridSearch(ctx context.Context, client *weaviate.Client, query string, vector []float32) ([]*types.Chunk, error) {
	fmt.Println("Query vector length: ", len(vector))

	_chunk_fields := []graphql.Field{
		{Name: "chunk"},
		{Name: "hash"},
		{Name: "documentid"},
		{Name: "_additional", Fields: []graphql.Field{
			{Name: "score"},
			{Name: "explainScore"},
		}},
	}

	log.Printf("Searching for chunks with query: %s\n", query)
	hybrid := client.GraphQL().HybridArgumentBuilder().
		WithQuery(query).
		WithVector(vector).
		WithProperties([]string{"chunk"}). // TODO: ensure the document title is included
		WithAlpha(0.7).
		WithFusionType(graphql.RelativeScore)

	resp, err := client.GraphQL().
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

	return parseChunks(ctx, client, get[chunkClassName].([]interface{}), true)
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
				CreatedAt:     createdAt,
				UpdatedAt:     updatedAt,
			},
			Text: c["chunk"].(string),
			Hash: c["hash"].(string),
		}
		if withScore {
			chunk.Score = score
		}
		res = append(res, chunk)
	}
	return res, nil
}

func CreateDocumentClass(ctx context.Context, client *weaviate.Client, force bool) error {
	// DEBUG: attempt to delete the class, don't fail if it doesn't exist
	if force {
		client.Schema().ClassDeleter().WithClassName(documentClassName).Do(ctx)
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
		},
	}

	// Create the class in Weaviate
	err := client.Schema().ClassCreator().WithClass(class).Do(ctx)
	if err != nil {
		return fmt.Errorf("failed to create chunk class: %v", err)
	}

	// Create the class in Weaviate
	return nil
}

func CreateChunkClass(ctx context.Context, client *weaviate.Client, force bool) error {
	// DEBUG: attempt to delete the class, don't fail if it doesn't exist
	if force {
		client.Schema().ClassDeleter().WithClassName(chunkClassName).Do(ctx)
	}

	class := &models.Class{
		Class:      chunkClassName,
		Vectorizer: "none",
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
		},
	}

	// Create the class in Weaviate
	err := client.Schema().ClassCreator().WithClass(class).Do(ctx)
	if err != nil {
		return fmt.Errorf("failed to create chunk class: %v", err)
	}

	return nil
}

func CreateConversation(ctx context.Context, client *weaviate.Client) (string, error) {
	// Create a new conversation object
	conversationID := uuid.NewString()
	_, err := client.Data().Creator().WithClassName(conversationClassName).WithID(conversationID).
		WithProperties(map[string]interface{}{
			"history": []interface{}{},
			"chunks":  []interface{}{},
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

func ListConversations(ctx context.Context, client *weaviate.Client) ([]*types.Conversation, error) {
	resp, err := client.GraphQL().Get().
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

func GetConversation(ctx context.Context, client *weaviate.Client, conversationID string) (*types.Conversation, error) {
	log.Printf("Looking for conversation with ID %s\n", conversationID)
	conversations, err := client.Data().ObjectsGetter().
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

	conversation := &types.Conversation{
		ID:          conversationID,
		History:     historyItems,
		ChunkHashes: chunkHashes,
	}
	return conversation, nil
}

func ConversationAppend(ctx context.Context, client *weaviate.Client, conversationID string, items []types.HistoryItem, chunks []*types.Chunk) error {
	conversation, err := GetConversation(ctx, client, conversationID)
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

	err = client.Data().Updater(). // replaces the entire object
					WithID(conversationID).
					WithClassName(conversationClassName).
					WithProperties(map[string]interface{}{
			"history": jsonHistory,
			"chunks":  conversation.ChunkHashes,
		}).
		Do(ctx)
	if err != nil {
		return fmt.Errorf("failed to update conversation: %v", err)
	}

	return nil
}

func CreateConversationClass(ctx context.Context, client *weaviate.Client, force bool) error {
	if force {
		client.Schema().ClassDeleter().WithClassName(conversationClassName).Do(ctx)
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
		},
	}

	// Create the class in Weaviate
	return client.Schema().ClassCreator().WithClass(class).Do(ctx)
}

// Create a Weaviate class schema for the connector state
func CreateConnectorStateClass(ctx context.Context, client *weaviate.Client, force bool) error {
	// DEBUG: attempt to delete the class, don't fail if it doesn't exist
	if force {
		client.Schema().ClassDeleter().WithClassName(stateClassName).Do(ctx)
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
	return client.Schema().ClassCreator().WithClass(class).Do(ctx)
}

var ErrSyncingAlreadyExpected = errors.New("syncing is already at the expected value")

func IsSyncingAlreadyExpected(err error) bool {
	return errors.Is(err, ErrSyncingAlreadyExpected)
}

func SetConnectorSyncing(ctx context.Context, client *weaviate.Client, connectorID string, syncing bool) (*types.ConnectorState, error) {
	state, err := GetConnectorState(ctx, client, connectorID)
	if err != nil {
		return nil, fmt.Errorf("unable to get connector state: %s", err)
	}

	if state.Syncing == syncing {
		return state, ErrSyncingAlreadyExpected
	}

	state.Syncing = syncing
	err = UpdateConnectorState(ctx, client, state)
	return state, err
}

// Add or update the connector state in Weaviate
func UpdateConnectorState(ctx context.Context, client *weaviate.Client, state *types.ConnectorState) error {
	where := filters.Where().
		WithPath([]string{"connector_id"}).
		WithOperator(filters.Equal).
		WithValueString(state.ConnectorID)

	resp, err := client.GraphQL().Get().
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
		_, err := client.Data().Creator().WithClassName(stateClassName).WithProperties(map[string]interface{}{
			"connector_id": state.ConnectorID,
			"type":         state.ConnectorType,
			"user":         state.User,
			"syncing":      state.Syncing,
			"auth_valid":   state.AuthValid,
			"lastSync":     state.LastSync,
			"numDocuments": state.NumDocuments,
			"numChunks":    state.NumChunks,
			"numErrors":    state.NumErrors,
		}).Do(ctx)
		return err
	}

	get := resp.Data["Get"].(map[string]interface{})
	states := get["ConnectorState"].([]interface{})
	c := states[0].(map[string]interface{})
	addl := c["_additional"].(map[string]interface{})
	objID := addl["id"].(string)

	err = client.Data().Updater(). // replaces the entire object
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
func AllConnectorStates(ctx context.Context, client *weaviate.Client) ([]*types.ConnectorState, error) {
	resp, err := client.GraphQL().Get().
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
func GetConnectorState(ctx context.Context, client *weaviate.Client, connectorID string) (*types.ConnectorState, error) {
	where := filters.Where().
		WithPath([]string{"connector_id"}).
		WithOperator(filters.Equal).
		WithValueString(connectorID)

	resp, err := client.GraphQL().Get().
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

func DeleteDocumentChunks(ctx context.Context, client *weaviate.Client, uniqueID string, connectorID string) error {
	docid, err := getDocumentIDFromUniqueID(ctx, client, uniqueID)
	if err != nil {
		return err
	}

	if docid == "" {
		// Document doesn't already exist, skip
		return nil
	}

	resp, err := client.Batch().ObjectsBatchDeleter().
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

	state, err := GetConnectorState(ctx, client, connectorID)
	if err != nil {
		return fmt.Errorf("unable to get connector state: %v", err)
	}

	if state == nil {
		return fmt.Errorf("connector state not found, unable to update chunk count")
	}

	state.NumChunks = state.NumChunks - int(numDeletedChunks)
	err = UpdateConnectorState(ctx, client, state)
	if err != nil {
		return fmt.Errorf("unable to update connector state: %v", err)
	}

	return nil
}

func DeleteConnector(ctx context.Context, connector types.Connector) error {
	// why do we need to get the client in the method signature. It is available within the package already.
	
	// TODO Mark connector for deletion. Cancel ongoing syncs, and exclude from future ones
	client := GetWeaviateClient()
	connectorID := connector.ID()
 
	where := filters.Where().
		WithPath([]string{"connectorID"}).
		WithOperator(filters.Equal).
		WithValueString(connectorID)

	docs, err := client.GraphQL().Get().
		WithClassName(documentClassName).
		WithFields([]graphql.Field{
			{
				Name: "unique_id",
			},
			{
				Name: "_additional", 
				Fields: []graphql.Field{
					{Name: "id"},
				},
			},
		}...).
		WithWhere(where).Do(ctx)
	
	if err != nil {
		return err
	}

	// No documents found
	if docs.Data["Get"] == nil {
		fmt.Println("No documents found for connector:", connectorID)
	} else {
		// print number of documents found
		fmt.Println("Number of documents found: ", len(docs.Data["Get"].(map[string]interface{})[documentClassName].([]interface{})))

		// Two options
		// 1. (Better) Collect chunk IDs for all docs, and delete in batches
		// 2. Iterate on docs and delete chunks for each, then the document
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

		// Iterate over the documents
		for _, doc := range classDocs {
			// Ensure each doc is a map
			document, ok := doc.(map[string]interface{})
			if !ok {
				log.Println("Failed to assert document as map[string]interface{}")
				continue
			}

			// Ensure unique_id is a string
			uniqueID, ok := document["unique_id"].(string)
			if !ok {
				log.Println("Failed to assert unique_id as string")
				continue
			}

			// Call your DeleteDocumentChunks method with the unique_id
			err := DeleteDocumentChunks(ctx, client, uniqueID, connectorID)
			if err != nil {
				log.Printf("Failed to delete document chunks for unique_id %s: %v", uniqueID, err)
				continue
			}

			fmt.Printf("Successfully deleted document chunks for unique_id %s\n", uniqueID)

			// Delete the document
			err = client.Data().Deleter().
				WithClassName(documentClassName).
				WithID(document["_additional"].(map[string]interface{})["id"].(string)).
				Do(ctx)
			
			if err != nil {
				log.Printf("Failed to delete document for unique_id %s: %v", uniqueID, err)
			}
		}
	}

	// Delete connector now that all docs and chunks were deleted
	connectorDeleteWhere := filters.Where().
		WithPath([]string{"connector_id"}).
		WithOperator(filters.Equal).
		WithValueString(connectorID)
	_, connectorDeletionErr := client.Batch().ObjectsBatchDeleter().
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
