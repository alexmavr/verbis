package store

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	"github.com/weaviate/weaviate-go-client/v4/weaviate"
	"github.com/weaviate/weaviate-go-client/v4/weaviate/filters"
	"github.com/weaviate/weaviate-go-client/v4/weaviate/graphql"
	"github.com/weaviate/weaviate/entities/models"

	"github.com/verbis-ai/verbis/verbis/types"
)

var (
	chunkClassName    = "VerbisChunk"
	documentClassName = "Document"
	stateClassName    = "ConnectorState"
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

func ChunkHashExists(ctx context.Context, client *weaviate.Client, hash string) (bool, error) {
	where := filters.Where().
		WithPath([]string{"hash"}).
		WithOperator(filters.Equal).
		WithValueString(hash)

	resp, err := client.GraphQL().Get().
		WithClassName(chunkClassName).
		WithFields([]graphql.Field{{Name: "hash"}}...).
		WithWhere(where).
		Do(ctx)
	if err != nil {
		return false, err
	}

	if resp.Data["Get"] == nil {
		return false, nil
	}

	get := resp.Data["Get"].(map[string]interface{})
	chunks, ok := get[chunkClassName].([]interface{})
	if !ok {
		return false, nil
	}

	return len(chunks) > 0, nil
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
	log.Printf("Adding %d vectors to vector store", len(items))
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
					"unique_id":   item.Document.UniqueID,
					"name":        item.Document.Name,
					"sourceURL":   item.Document.SourceURL,
					"connectorID": item.Document.ConnectorID,
					"createdAt":   item.Document.CreatedAt.Format(time.RFC3339),
					"updatedAt":   item.Document.UpdatedAt.Format(time.RFC3339),
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
	res := []*types.Chunk{}
	if resp.Data["Get"] != nil {
		get := resp.Data["Get"].(map[string]interface{})
		if get[chunkClassName] == nil {
			// return empty result
			return res, nil
		}

		chunks := get[chunkClassName].([]interface{})
		for _, chunkMap := range chunks {
			c := chunkMap.(map[string]interface{})

			// Parse additional info
			addl := c["_additional"].(map[string]interface{})
			scoreStr := addl["score"].(string)
			log.Printf("ScoreStr: %s\n", scoreStr)
			score, err := strconv.ParseFloat(scoreStr, 64)
			if err != nil {
				log.Printf("Failed to parse score: %s\n", err)
				continue
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
			log.Print(docData)
			createdAt, _ := time.Parse(time.RFC3339, docData["createdAt"].(string))
			updatedAt, _ := time.Parse(time.RFC3339, docData["updatedAt"].(string))

			res = append(res, &types.Chunk{
				Document: types.Document{
					Name:        docData["name"].(string),
					SourceURL:   docData["sourceURL"].(string),
					ConnectorID: docData["connectorID"].(string),
					CreatedAt:   createdAt,
					UpdatedAt:   updatedAt,
				},
				Text:  c["chunk"].(string),
				Hash:  c["hash"].(string),
				Score: score,
			})
		}
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

func LockConnector(ctx context.Context, client *weaviate.Client, connectorID string) error {
	state, err := GetConnectorState(ctx, client, connectorID)
	if err != nil {
		return err
	}

	if state == nil {
		return fmt.Errorf("connector state not found")
	}

	// Wait until the state is not syncing
	for state.Syncing {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled")
		case <-time.After(5 * time.Second):
			state, err = GetConnectorState(ctx, client, connectorID)
			if err != nil {
				return err
			}
		}
	}

	state.Syncing = true
	return UpdateConnectorState(ctx, client, state)
}

func UnlockConnector(ctx context.Context, client *weaviate.Client, connectorID string) error {
	state, err := GetConnectorState(ctx, client, connectorID)
	if err != nil {
		return err
	}

	if state == nil {
		return fmt.Errorf("connector state not found")
	}

	state.Syncing = false
	return UpdateConnectorState(ctx, client, state)
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
		log.Print("Creating new connector state")
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
	log.Print("Updating existing connector state")

	get := resp.Data["Get"].(map[string]interface{})
	states := get["ConnectorState"].([]interface{})
	fmt.Println(states)

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

// Retrieve the connector state from Weaviate
// Does not return AuthValid as it can be inferred from the presence of a token
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
	states, ok := get["ConnectorState"].([]interface{})
	if !ok {
		return nil, nil
	}

	if len(states) == 0 {
		return nil, nil
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
