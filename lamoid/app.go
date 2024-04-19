package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/weaviate/weaviate-go-client/v4/weaviate"
	"github.com/weaviate/weaviate-go-client/v4/weaviate/graphql"
	"github.com/weaviate/weaviate/entities/models"

	"github.com/epochlabs-ai/lamoid/lamoid/connectors"
)

var (
	httpClient = &http.Client{Timeout: 10 * time.Second}
)

func getWeaviateClient() *weaviate.Client {
	// Initialize Weaviate client
	return weaviate.New(weaviate.Config{
		Host:   "localhost:8088",
		Scheme: "http",
	})
}

func main() {
	// Define the commands to be executed
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(context.Background())

	router := setupRouter()
	server := http.Server{
		Addr:    ":8081",
		Handler: router,
	}

	// Cancel context when sigChan receives a signal
	defer func() {
		signal.Stop(sigChan)
		cancel()
		close(sigChan)
	}()

	go func() {
		select {
		case <-sigChan:
			cancel()
			server.Close()
		case <-ctx.Done():
			server.Close()
		}
	}()

	// Weaviate flags
	os.Setenv("PERSISTENCE_DATA_PATH", "/tmp/lamoid")
	os.Setenv("AUTHENTICATION_ANONYMOUS_ACCESS_ENABLED", "true")
	commands := []struct {
		Name string
		Args []string
	}{
		{"ollama", []string{"serve"}},
		{"../dist/weaviate", []string{"--host", "0.0.0.0", "--port", "8088", "--scheme", "http"}},
	}

	// Start subprocesses
	startSubprocesses(ctx, commands)

	// TODO: start background sync process to sync data from external sources

	err := waitForOllama(ctx)
	if err != nil {
		log.Fatalf("Failed to wait for ollama: %s\n", err)
	}

	err = pullModel("llama3", false)
	if err != nil {
		log.Fatalf("Failed to pull model: %s\n", err)
	}

	err = pullModel("all-minilm", false)
	if err != nil {
		log.Fatalf("Failed to pull model: %s\n", err)
	}

	// Create index for vector search
	createWeaviateSchema(getWeaviateClient())

	// Perform a test generation with ollama to first pull the model
	resp, err := generateFromModel("What is the capital of France?")
	if err != nil {
		log.Fatalf("Failed to generate response: %s\n", err)
	}
	if !resp.Done {
		log.Fatalf("Response not done: %v\n", resp)
	}
	if !strings.Contains(resp.Response, "Paris") {
		log.Fatalf("Response does not contain Paris: %v\n", resp)
	}

	// Start HTTP server
	log.Print("Starting server on port 8081")
	log.Fatal(server.ListenAndServe())
}

func startSubprocesses(ctx context.Context, commands []struct {
	Name string
	Args []string
}) {

	for _, cmdConfig := range commands {
		go func(c struct {
			Name string
			Args []string
		}) {
			cmd := exec.Command(c.Name, c.Args...)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr

			if err := cmd.Start(); err != nil {
				log.Printf("Error starting command %s: %s\n", c.Name, err)
				return
			}

			go func() {
				<-ctx.Done()
				if err := cmd.Process.Kill(); err != nil {
					log.Printf("Failed to kill process %s: %s\n", c.Name, err)
				}
			}()

			if err := cmd.Wait(); err != nil {
				log.Printf("Command %s finished with error: %s\n", c.Name, err)
			}
		}(cmdConfig)
	}

}

func setupRouter() *mux.Router {
	r := mux.NewRouter()
	r.HandleFunc("/google/init", googleInit).Methods("GET")
	r.HandleFunc("/google/callback", handleGoogleCallback).Methods("GET")
	r.HandleFunc("/google/sync", googleSync).Methods("GET")
	r.HandleFunc("/prompt", handlePrompt).Methods("GET")

	return r
}

func googleInit(w http.ResponseWriter, r *http.Request) {
	connectors.GoogleInitialConfig()
}

func handleGoogleCallback(w http.ResponseWriter, r *http.Request) {
	queryParts := r.URL.Query()
	code := queryParts.Get("code")
	if code == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("No code in request"))
		return
	}

	err := queryParts.Get("error")
	if err != "" {
		log.Printf("Error in Google callback: %s\n", err)
	}

	connectors.GoogleAuthCallback(code)
}

func googleSync(w http.ResponseWriter, r *http.Request) {
	chunks := connectors.GoogleSync()
	log.Printf("Synced %d chunks from Google\n", len(chunks))
	for _, chunk := range chunks {
		log.Print(chunk)
		resp, err := EmbedFromModel(chunk)
		if err != nil {
			log.Printf("Failed to get embeddings: %s\n", err)
			continue
		}
		embedding := resp.Embedding

		// Add the embedding to the vector index
		addVector(context.Background(), getWeaviateClient(), chunk, embedding)
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
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

// Struct to define the API response format
type ApiResponse struct {
	Model              string    `json:"model"`
	CreatedAt          time.Time `json:"created_at"`
	Response           string    `json:"response"`
	Done               bool      `json:"done"`
	Context            []int     `json:"context"`
	TotalDuration      int64     `json:"total_duration"`
	LoadDuration       int64     `json:"load_duration"`
	PromptEvalCount    int       `json:"prompt_eval_count"`
	PromptEvalDuration int64     `json:"prompt_eval_duration"`
	EvalCount          int       `json:"eval_count"`
	EvalDuration       int64     `json:"eval_duration"`
}

// Function to call ollama model
func generateFromModel(prompt string) (*ApiResponse, error) {
	// URL of the API endpoint
	url := "http://localhost:11434/api/generate"

	// Create the payload
	payload := RequestPayload{
		Model:  "llama3",
		Prompt: prompt,
		Stream: false,
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

func handlePrompt(w http.ResponseWriter, r *http.Request) {
	prompt := r.URL.Query().Get("prompt")
	if prompt == "" {
		http.Error(w, "Missing prompt query parameter", http.StatusBadRequest)
		return
	}
	log.Printf("Prompt: %s", prompt)

	// Call Ollama embeddings model to get embeddings for the prompt
	resp, err := EmbedFromModel(prompt)
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
	llmPrompt := MakePrompt(searchResults, prompt)
	log.Printf("LLM Prompt: %s", llmPrompt)
	genResp, err := generateFromModel(llmPrompt)
	if err != nil {
		http.Error(w, "Failed to generate response", http.StatusInternalServerError)

	}
	log.Printf("Response: %s", genResp.Response)

	b, err := json.Marshal(genResp.Response)
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
func addVector(ctx context.Context, client *weaviate.Client, chunk string, vector []float32) error {
	w, err := client.Data().Creator().WithClassName("WeavChunk").WithProperties(map[string]interface{}{
		"chunk": chunk,
	}).WithVector(vector).Do(ctx)
	if err != nil {
		return err
	}

	// the returned value is a wrapped object
	b, err := json.MarshalIndent(w.Object, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

// Search for a vector in Weaviate
func vectorSearch(ctx context.Context, client *weaviate.Client, vector []float32) ([]string, error) {
	///	respAll, _ := client.GraphQL().Get().WithClassName("Chunk").WithFields(graphql.Field{Name: "chunk"}).Do(ctx)
	///	fmt.Println("XXXXXXX")
	///	fmt.Println(respAll.Data)
	fmt.Println("XXXXXXX")
	fmt.Println(len(vector))
	fmt.Println("YYYY")

	nearVector := client.GraphQL().NearVectorArgBuilder().WithVector(vector)

	resp, err := client.GraphQL().
		Get().
		WithClassName("WeavChunk").
		WithNearVector(nearVector).
		WithLimit(5).
		WithFields(graphql.Field{Name: "chunk"}).
		Do(ctx)
	if err != nil {
		return nil, err
	}

	get := resp.Data["Get"].(map[string]interface{})
	chunks := get["WeavChunk"].([]interface{})
	fmt.Println(chunks)

	res := []string{}
	for _, chunkMap := range chunks {
		c := chunkMap.(map[string]interface{})
		for _, v := range c {
			res = append(res, v.(string))
		}
	}

	return res, nil
}

// Create Weaviate schema for vector storage
func createWeaviateSchema(client *weaviate.Client) error {
	class := &models.Class{
		Class:      "WeavChunk",
		Vectorizer: "none",
	}

	// Create the class in Weaviate
	err := client.Schema().ClassCreator().WithClass(class).Do(context.Background())
	if err != nil {
		return fmt.Errorf("failed to create Weaviate class: %v", err)
	}

	return nil
}
