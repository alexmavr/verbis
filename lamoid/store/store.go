package store

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/weaviate/weaviate-go-client/v4/weaviate"
	"github.com/weaviate/weaviate-go-client/v4/weaviate/filters"
	"github.com/weaviate/weaviate-go-client/v4/weaviate/graphql"
	"github.com/weaviate/weaviate/entities/models"

	"github.com/epochlabs-ai/lamoid/lamoid/types"
)

var (
	chunkClassName = "LamoidChunk"
	stateClassName = "ConnectorState"
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

func AddVectors(ctx context.Context, client *weaviate.Client, items []types.AddVectorItem) error {
	log.Printf("Adding %d vectors to vector store", len(items))
	objects := []*models.Object{}
	for _, item := range items {
		objects = append(objects, &models.Object{
			Class: chunkClassName,
			Properties: map[string]string{
				"chunk":      item.Chunk.Text,
				"docName":    item.Chunk.Document.Name,
				"sourceURL":  item.Chunk.Document.SourceURL,
				"sourceName": item.Chunk.Document.SourceName,
				"createdAt":  item.Chunk.Document.CreatedAt.String(),
				"updatedAt":  item.Chunk.Document.UpdatedAt.String(),
			},
			Vector: item.Vector,
		})
	}

	_, err := client.Batch().ObjectsBatcher().WithObjects(objects...).Do(ctx)
	return err
}

// Search for a vector in Weaviate
func HybridSearch(ctx context.Context, client *weaviate.Client, query string, vector []float32) ([]*types.Chunk, error) {
	fmt.Println("Query vector length: ", len(vector))

	_chunk_fields := []graphql.Field{
		{Name: "chunk"},
		{Name: "docName"},
		{Name: "sourceURL"},
		{Name: "sourceName"},
		{Name: "updatedAt"},
		{Name: "createdAt"},
		{Name: "_additional", Fields: []graphql.Field{
			{Name: "score"},
			{Name: "explainScore"},
		}},
	}

	hybrid := client.GraphQL().HybridArgumentBuilder().
		WithQuery(query).
		WithVector(vector).
		WithProperties([]string{"chunk", "docName^2"}).
		WithAlpha(0.7).
		WithFusionType(graphql.RelativeScore)

	resp, err := client.GraphQL().
		Get().
		WithClassName(chunkClassName).
		WithHybrid(hybrid).
		WithLimit(MaxNumSearchResults).
		WithFields(_chunk_fields...).
		//		WithAutocut(1).
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

			// Find similarity score
			// TODO: refactor
			addl := c["_additional"].(map[string]interface{})
			scoreStr := addl["score"].(string)
			log.Printf("ScoreStr: %s\n", scoreStr)
			score, err := strconv.ParseFloat(scoreStr, 64)
			if err != nil {
				log.Printf("Failed to parse score: %s\n", err)
				continue
			}
			log.Printf("Score: %f\n", score)

			res = append(res, &types.Chunk{
				Document: types.Document{
					Name:       c["docName"].(string),
					SourceURL:  c["sourceURL"].(string),
					SourceName: c["sourceName"].(string),
					CreatedAt:  time.Now(),
					UpdatedAt:  time.Now(),
				},
				Text:  c["chunk"].(string),
				Score: score,
			})
		}
	}

	return res, nil
}

// Create Weaviate schema for vector storage
func CreateChunkClass(ctx context.Context, client *weaviate.Client, force bool) error {
	// DEBUG: attempt to delete the class, don't fail if it doesn't exist
	if force {
		client.Schema().ClassDeleter().WithClassName(chunkClassName).Do(ctx)
	}

	class := &models.Class{
		Class:      chunkClassName,
		Vectorizer: "none",
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
				DataType: []string{"string"},
			},
			{
				Name:     "type",
				DataType: []string{"string"},
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
			"syncing":      state.Syncing,
			"auth_valid":   state.AuthValid,
			"lastSync":     state.LastSync,
			"numDocuments": state.NumDocuments,
			"numChunks":    state.NumChunks,
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
			"syncing":      state.Syncing,
			"auth_valid":   state.AuthValid,
			"lastSync":     state.LastSync,
			"numDocuments": state.NumDocuments,
			"numChunks":    state.NumChunks,
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
				{Name: "syncing"},
				{Name: "auth_valid"},
				{Name: "lastSync"},
				{Name: "numDocuments"},
				{Name: "numChunks"},
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
			Syncing:       c["syncing"].(bool),
			AuthValid:     c["auth_valid"].(bool),
			LastSync:      lastSync,
			NumDocuments:  int(c["numDocuments"].(float64)),
			NumChunks:     int(c["numChunks"].(float64)),
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
				{Name: "syncing"},
				{Name: "auth_valid"},
				{Name: "lastSync"},
				{Name: "numDocuments"},
				{Name: "numChunks"},
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
		Syncing:       c["syncing"].(bool),
		AuthValid:     c["auth_valid"].(bool),
		LastSync:      lastSync,
		NumDocuments:  int(c["numDocuments"].(float64)),
		NumChunks:     int(c["numChunks"].(float64)),
	}, nil
}
