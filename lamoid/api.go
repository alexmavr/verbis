package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/weaviate/weaviate-go-client/v4/weaviate"
	"github.com/weaviate/weaviate-go-client/v4/weaviate/graphql"
	"github.com/weaviate/weaviate/entities/models"

	"github.com/epochlabs-ai/lamoid/lamoid/connectors"
)

var (
	weaviateClassName = "LamoidChunk"
	_fields           = []graphql.Field{
		{Name: "chunk"},
		{Name: "sourceURL"},
		{Name: "sourceName"},
		{Name: "updatedAt"},
		{Name: "createdAt"},
	}
	GoogleConnector = connectors.GoogleConnector{}
)

func getWeaviateClient() *weaviate.Client {
	// Initialize Weaviate client
	return weaviate.New(weaviate.Config{
		Host:   "localhost:8088",
		Scheme: "http",
	})
}

func setupRouter() *mux.Router {
	r := mux.NewRouter()
	r.HandleFunc("/google/init", googleInit).Methods("GET")
	r.HandleFunc("/google/callback", handleGoogleCallback).Methods("GET")
	r.HandleFunc("/google/sync", googleSync).Methods("GET")
	r.HandleFunc("/prompt", handlePrompt).Methods("POST")
	r.HandleFunc("/health", health).Methods("GET")

	r.HandleFunc("/connectors", connectorsList).Methods("GET")

	return r
}

func health(w http.ResponseWriter, r *http.Request) {
	// TODO: check for health of subprocesses - not needed for first boot
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func connectorsList(w http.ResponseWriter, r *http.Request) {
	status := GoogleConnector.Status(r.Context())

	b, err := json.Marshal(status)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Failed to marshal status: " + err.Error()))
		return
	}

	w.Write(b)
}

func googleInit(w http.ResponseWriter, r *http.Request) {
	err := GoogleConnector.Init(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Failed to perform initial auth with google: " + err.Error()))
		return
	}
}

func handleGoogleCallback(w http.ResponseWriter, r *http.Request) {
	queryParts := r.URL.Query()
	code := queryParts.Get("code")
	if code == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("No code in request"))
		return
	}

	errStr := queryParts.Get("error")
	if errStr != "" {
		log.Printf("Error in Google callback: %s\n", errStr)
	}

	err := GoogleConnector.AuthCallback(r.Context(), code)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Failed to authenticate with Google: " + err.Error()))
	}
}

func googleSync(w http.ResponseWriter, r *http.Request) {
	chunks, err := GoogleConnector.Sync(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Failed to sync with Google: " + err.Error()))
		return
	}
	log.Printf("Synced %d chunks from Google\n", len(chunks))

	chunkItems := []AddVectorItem{}
	for _, chunk := range chunks {
		log.Printf("Processing chunk: %s\n", chunk.SourceURL)
		resp, err := EmbedFromModel(chunk.Text)
		if err != nil {
			log.Printf("Failed to get embeddings: %s\n", err)
			continue
		}
		embedding := resp.Embedding

		chunkItems = append(chunkItems, AddVectorItem{
			Chunk:  chunk,
			Vector: embedding,
		})
	}

	err = addVectors(context.Background(), getWeaviateClient(), chunkItems)
	if err != nil {
		log.Fatalf("Failed to add vectors: %s\n", err)
	}
}

type PullRequestPayload struct {
	Name   string `json:"name"`
	Stream bool   `json:"stream"`
}

type PullApiResponse struct {
	Status string `json:"status"`
}

// pullModel makes a POST request to the specified URL with the given payload
// and returns nil only if the response status is "success".
func pullModel(name string, stream bool) error {
	url := "http://localhost:11434/api/pull"

	// Create the payload
	payload := PullRequestPayload{
		Name:   name,
		Stream: stream,
	}

	// Marshal the payload into JSON
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	// Create a new HTTP request
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}

	// Set the Content-Type header
	req.Header.Set("Content-Type", "application/json")

	// Make the HTTP request using the default client
	client := &http.Client{}
	response, err := client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	// Read the response body
	responseData, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}

	// Unmarshal JSON data into ApiResponse struct
	var apiResponse PullApiResponse
	if err := json.Unmarshal(responseData, &apiResponse); err != nil {
		return err
	}

	// Check if the status is "success"
	if apiResponse.Status != "success" {
		return fmt.Errorf("API response status is not 'success'")
	}

	return nil
}

func waitForOllama(ctx context.Context) error {
	ollama_url := "http://localhost:11434"

	// Poll the ollama URL every 5 seconds until the context is cancelled
	for {
		resp, err := httpClient.Get(ollama_url)
		log.Print(resp)
		if err == nil {
			log.Printf("Ollama is up and running")
			resp.Body.Close()
			return nil
		}
		select {
		case <-time.After(5 * time.Second):
			log.Printf("Waited 5 sec")
			continue
		case <-ctx.Done():
			return fmt.Errorf("context cancelled during wait: %w", ctx.Err())
		}
	}
}

// Struct to define the request payload
type EmbedRequestPayload struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

// Struct to define the API response format
type EmbedApiResponse struct {
	Embedding []float32 `json:"embedding"`
}

// Function to call ollama model
func EmbedFromModel(prompt string) (*EmbedApiResponse, error) {
	// URL of the API endpoint
	url := "http://localhost:11434/api/embeddings"

	// Create the payload
	payload := EmbedRequestPayload{
		Model:  "all-minilm",
		Prompt: prompt,
	}

	// Marshal the payload into JSON
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	// Create a new HTTP request
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}

	// Set the appropriate headers
	req.Header.Set("Content-Type", "application/json")

	// Make the HTTP request using the default client
	client := &http.Client{}
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	// Read the response body
	responseData, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	// Unmarshal JSON data into ApiResponse struct
	var apiResponse EmbedApiResponse
	if err := json.Unmarshal(responseData, &apiResponse); err != nil {
		return nil, err
	}

	// Return the structured response
	return &apiResponse, nil
}

// Struct to define the request payload
type RequestPayload struct {
	Model     string        `json:"model"`
	Messages  []HistoryItem `json:"messages"`
	Stream    bool          `json:"stream"`
	KeepAlive string        `json:"keep_alive"`
}

// Struct to define the API response format
type ApiResponse struct {
	Model              string      `json:"model"`
	CreatedAt          time.Time   `json:"created_at"`
	Message            HistoryItem `json:"message"`
	Done               bool        `json:"done"`
	Context            []int       `json:"context"`
	TotalDuration      int64       `json:"total_duration"`
	LoadDuration       int64       `json:"load_duration"`
	PromptEvalCount    int         `json:"prompt_eval_count"`
	PromptEvalDuration int64       `json:"prompt_eval_duration"`
	EvalCount          int         `json:"eval_count"`
	EvalDuration       int64       `json:"eval_duration"`
}

// Function to call ollama model
func chatWithModel(prompt string, history []HistoryItem) (*ApiResponse, error) {
	// URL of the API endpoint
	url := "http://localhost:11434/api/chat"

	messages := append(history, HistoryItem{
		Role:    "user",
		Content: prompt,
	})

	// TODO: pass history
	// Create the payload
	payload := RequestPayload{
		Model:     "llama3",
		Messages:  messages,
		Stream:    false,
		KeepAlive: "20m",
	}

	// Marshal the payload into JSON
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	// Create a new HTTP request
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}

	// Set the appropriate headers
	req.Header.Set("Content-Type", "application/json")

	// Make the HTTP request using the default client
	client := &http.Client{}
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	// Read the response body
	responseData, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	log.Printf("Response: %v", string(responseData))

	// Unmarshal JSON data into ApiResponse struct
	var apiResponse ApiResponse
	if err := json.Unmarshal(responseData, &apiResponse); err != nil {
		return nil, err
	}

	// Return the structured response
	return &apiResponse, nil
}

type HistoryItem struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type PromptRequest struct {
	Prompt  string        `json:"prompt"`
	History []HistoryItem `json:"history"`
}

func handlePrompt(w http.ResponseWriter, r *http.Request) {
	var promptReq PromptRequest
	defer r.Body.Close()
	err := json.NewDecoder(r.Body).Decode(&promptReq)
	if err != nil {
		// return HTTP 400 bad request
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Failed to decode request"))
	}

	// Call Ollama embeddings model to get embeddings for the prompt
	resp, err := EmbedFromModel(promptReq.Prompt)
	if err != nil {
		http.Error(w, "Failed to get embeddings", http.StatusInternalServerError)
		return
	}

	embeddings := resp.Embedding
	log.Printf("Performing vector search")

	// Perform vector similarity search and get list of most relevant results
	searchResults, err := vectorSearch(context.Background(), getWeaviateClient(), embeddings)
	if err != nil {
		http.Error(w, "Failed to search for vectors", http.StatusInternalServerError)
		return
	}

	// TODO: Use ollama LLM model to rerank the results, pick the top 5

	// Return the completion from the LLM model
	llmPrompt := MakePrompt(searchResults, promptReq.Prompt)
	log.Printf("LLM Prompt: %s", llmPrompt)
	genResp, err := chatWithModel(llmPrompt, promptReq.History)
	if err != nil {
		http.Error(w, "Failed to generate response", http.StatusInternalServerError)

	}
	log.Printf("Response: %s", genResp.Message.Content)

	b, err := json.Marshal(genResp.Message.Content)
	if err != nil {
		http.Error(w, "Failed to marshal search results", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(b)
}

// Promptsage?
func MakePrompt(dataChunks []string, query string) string {
	// Create a builder to efficiently concatenate strings
	var builder strings.Builder

	// Append introduction to guide the model's focus
	builder.WriteString("Based on the following data:\n\n")

	// Loop through each data chunk and append it followed by a newline
	for _, chunk := range dataChunks {
		builder.WriteString(chunk + "\n\n")
	}

	// Append the user query with an instruction
	builder.WriteString("Answer this question based on the data provided above: ")
	builder.WriteString(query)

	// Return the final combined prompt
	return builder.String()
}

// Add a vector to Weaviate
type AddVectorItem struct {
	connectors.Chunk
	Vector []float32
}

func cleanWhitespace(text string) string {
	// Trim leading and trailing whitespace
	text = strings.TrimSpace(text)

	// Replace internal sequences of whitespace with a single space
	spacePattern := regexp.MustCompile(`\s+`)
	return spacePattern.ReplaceAllString(text, " ")
}

func addVectors(ctx context.Context, client *weaviate.Client, items []AddVectorItem) error {
	log.Printf("Adding %d vectors to vector store", len(items))
	objects := []*models.Object{}
	for _, item := range items {
		objects = append(objects, &models.Object{
			Class: weaviateClassName,
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
func vectorSearch(ctx context.Context, client *weaviate.Client, vector []float32) ([]string, error) {
	fmt.Println("Query vector length: ", len(vector))

	all, _ := client.GraphQL().Get().WithClassName(weaviateClassName).Do(ctx)
	log.Print(all.Data)

	nearVector := client.GraphQL().NearVectorArgBuilder().WithVector(vector)

	resp, err := client.GraphQL().
		Get().
		WithClassName(weaviateClassName).
		WithNearVector(nearVector).
		WithLimit(5).
		WithFields(_fields...).
		Do(ctx)
	if err != nil {
		return nil, err
	}

	log.Print(resp)
	log.Print(resp.Data)
	log.Print(resp.Data["Get"])

	res := []string{}
	if resp.Data["Get"] != nil {
		get := resp.Data["Get"].(map[string]interface{})
		chunks := get[weaviateClassName].([]interface{})
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
func createWeaviateSchema(ctx context.Context, client *weaviate.Client) error {
	// attempt to delete the class, don't fail if it doesn't exist
	client.Schema().ClassDeleter().WithClassName(weaviateClassName).Do(ctx)

	class := &models.Class{
		Class:      weaviateClassName,
		Vectorizer: "none",
	}

	// Create the class in Weaviate
	err := client.Schema().ClassCreator().WithClass(class).Do(ctx)
	if err != nil {
		return fmt.Errorf("failed to create Weaviate class: %v", err)
	}

	return nil
}
