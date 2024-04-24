package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"github.com/epochlabs-ai/lamoid/lamoid/connectors"
	"github.com/epochlabs-ai/lamoid/lamoid/store"
	"github.com/epochlabs-ai/lamoid/lamoid/types"
)

var (
	GoogleConnector = connectors.GoogleConnector{}
)

func setupRouter() *mux.Router {
	r := mux.NewRouter()
	r.HandleFunc("/google/init", googleInit).Methods("GET")
	r.HandleFunc("/google/callback", handleGoogleCallback).Methods("GET")
	r.HandleFunc("/google/sync", googleSync).Methods("GET")
	r.HandleFunc("/prompt", handlePrompt).Methods("POST")
	r.HandleFunc("/health", health).Methods("GET")

	r.HandleFunc("/connectors", connectorsList).Methods("GET")
	r.HandleFunc("/mock", mockConnectorState).Methods("GET")

	return r
}

func health(w http.ResponseWriter, r *http.Request) {
	// TODO: check for health of subprocesses - not needed for first boot
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func mockConnectorState(w http.ResponseWriter, r *http.Request) {
	state := &types.ConnectorState{
		Name:         "Google Drive",
		Syncing:      true,
		LastSync:     time.Now(),
		NumDocuments: 15,
		NumChunks:    1005,
	}

	err := store.UpdateConnectorState(context.Background(), store.GetWeaviateClient(), state)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Failed to update state: " + err.Error()))
		return
	}
}

func connectorsList(w http.ResponseWriter, r *http.Request) {
	status, err := GoogleConnector.Status(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Failed to get status: " + err.Error()))
		return
	}

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

	chunkItems := []types.AddVectorItem{}
	for _, chunk := range chunks {
		log.Printf("Processing chunk: %s\n", chunk.SourceURL)
		resp, err := EmbedFromModel(chunk.Text)
		if err != nil {
			log.Printf("Failed to get embeddings: %s\n", err)
			continue
		}
		embedding := resp.Embedding

		chunkItems = append(chunkItems, types.AddVectorItem{
			Chunk:  chunk,
			Vector: embedding,
		})
	}

	err = store.AddVectors(context.Background(), store.GetWeaviateClient(), chunkItems)
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
	Model     string              `json:"model"`
	Messages  []types.HistoryItem `json:"messages"`
	Stream    bool                `json:"stream"`
	KeepAlive string              `json:"keep_alive"`
}

// Struct to define the API response format
type ApiResponse struct {
	Model              string            `json:"model"`
	CreatedAt          time.Time         `json:"created_at"`
	Message            types.HistoryItem `json:"message"`
	Done               bool              `json:"done"`
	Context            []int             `json:"context"`
	TotalDuration      int64             `json:"total_duration"`
	LoadDuration       int64             `json:"load_duration"`
	PromptEvalCount    int               `json:"prompt_eval_count"`
	PromptEvalDuration int64             `json:"prompt_eval_duration"`
	EvalCount          int               `json:"eval_count"`
	EvalDuration       int64             `json:"eval_duration"`
}

type PromptRequest struct {
	Prompt  string              `json:"prompt"`
	History []types.HistoryItem `json:"history"`
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
	searchResults, err := store.VectorSearch(context.Background(), store.GetWeaviateClient(), embeddings)
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
