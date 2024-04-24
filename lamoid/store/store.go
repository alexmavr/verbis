package store

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
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

	_chunk_fields = []graphql.Field{
		{Name: "chunk"},
		{Name: "sourceURL"},
		{Name: "sourceName"},
		{Name: "updatedAt"},
		{Name: "createdAt"},
	}
)

func GetWeaviateClient() *weaviate.Client {
	// Initialize Weaviate client
	return weaviate.New(weaviate.Config{
		Host:   "localhost:8088",
		Scheme: "http",
	})
}

func cleanWhitespace(text string) string {
	// Trim leading and trailing whitespace
	text = strings.TrimSpace(text)

	// Replace internal sequences of whitespace with a single space
	spacePattern := regexp.MustCompile(`\s+`)
	return spacePattern.ReplaceAllString(text, " ")
}

func AddVectors(ctx context.Context, client *weaviate.Client, items []types.AddVectorItem) error {
	log.Printf("Adding %d vectors to vector store", len(items))
	objects := []*models.Object{}
	for _, item := range items {
		objects = append(objects, &models.Object{
			Class: chunkClassName,
			Properties: map[string]string{
				"chunk":      cleanWhitespace(item.Chunk.Text),
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
func VectorSearch(ctx context.Context, client *weaviate.Client, vector []float32) ([]string, error) {
	fmt.Println("Query vector length: ", len(vector))

	all, _ := client.GraphQL().Get().WithClassName(chunkClassName).Do(ctx)
	log.Print(all.Data)

	nearVector := client.GraphQL().NearVectorArgBuilder().WithVector(vector)

	resp, err := client.GraphQL().
		Get().
		WithClassName(chunkClassName).
		WithNearVector(nearVector).
		WithLimit(5).
		WithFields(_chunk_fields...).
		Do(ctx)
	if err != nil {
		return nil, err
	}

	log.Print(resp.Data["Get"])

	res := []string{}
	if resp.Data["Get"] != nil {
		get := resp.Data["Get"].(map[string]interface{})
		chunks := get[chunkClassName].([]interface{})
		fmt.Println(chunks)

		for _, chunkMap := range chunks {
			c := chunkMap.(map[string]interface{})
			for _, v := range c {
				res = append(res, v.(string))
			}
		}
	}

	return res, nil
}

// Create Weaviate schema for vector storage
func CreateChunkClass(ctx context.Context, client *weaviate.Client) error {
	// DEBUG: attempt to delete the class, don't fail if it doesn't exist
	client.Schema().ClassDeleter().WithClassName(chunkClassName).Do(ctx)

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
func CreateConnectorStateClass(ctx context.Context, client *weaviate.Client) error {
	// DEBUG: attempt to delete the class, don't fail if it doesn't exist
	client.Schema().ClassDeleter().WithClassName(stateClassName).Do(ctx)

	class := &models.Class{
		Class:      stateClassName,
		Vectorizer: "none",
		Properties: []*models.Property{
			{
				Name:     "name",
				DataType: []string{"string"},
			},
			{
				Name:     "syncing",
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

// Add or update the connector state in Weaviate
func UpdateConnectorState(ctx context.Context, client *weaviate.Client, state *types.ConnectorState) error {
	where := filters.Where().
		WithPath([]string{"name"}).
		WithOperator(filters.Equal).
		WithValueString(state.Name)

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

	if resp.Data["Get"] == nil {
		log.Print("Creating new connector state")
		_, err := client.Data().Creator().WithClassName(stateClassName).WithProperties(map[string]interface{}{
			"name":         state.Name,
			"syncing":      state.Syncing,
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
			"name":         state.Name,
			"syncing":      state.Syncing,
			"lastSync":     state.LastSync,
			"numDocuments": state.NumDocuments,
			"numChunks":    state.NumChunks,
		}).
		Do(ctx)

	return err
}

// Retrieve the connector state from Weaviate
func GetConnectorState(ctx context.Context, client *weaviate.Client, name string) (*types.ConnectorState, error) {
	where := filters.Where().
		WithPath([]string{"name"}).
		WithOperator(filters.Equal).
		WithValueString(name)

	resp, err := client.GraphQL().Get().
		WithClassName(stateClassName).
		WithFields(
			[]graphql.Field{
				{Name: "name"},
				{Name: "syncing"},
				{Name: "lastSync"},
				{Name: "numDocuments"},
				{Name: "numChunks"},
			}...).
		WithWhere(where).
		Do(ctx)
	if err != nil {
		return nil, err
	}

	fmt.Println(resp.Data)

	if resp.Data["Get"] == nil {
		return nil, nil
	}

	get := resp.Data["Get"].(map[string]interface{})
	states := get["ConnectorState"].([]interface{})
	fmt.Println(states)

	if len(states) == 0 {
		return nil, nil
	}

	c := states[0].(map[string]interface{})

	lastSync, err := time.Parse(time.RFC3339, c["lastSync"].(string))
	if err != nil {
		log.Printf("Failed to parse last sync time: %s\n", err)
	}

	return &types.ConnectorState{
		Name:         c["name"].(string),
		Syncing:      c["syncing"].(bool),
		LastSync:     lastSync,
		NumDocuments: int(c["numDocuments"].(float64)),
		NumChunks:    int(c["numChunks"].(float64)),
	}, nil
}
