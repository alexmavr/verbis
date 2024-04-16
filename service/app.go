package main

import (
	"bytes"
	"context"
	"encoding/binary"
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

	"github.com/go-redis/redis/v8"
	"github.com/gorilla/mux"

	"github.com/epochlabs-ai/lamoid/service/connectors"
)

var (
	redisClient *redis.Client
	httpClient  = &http.Client{Timeout: 10 * time.Second}
)

func init() {
	// Initialize Redis client
	redisClient = redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
}

func main() {
	// Define the commands to be executed
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel context when sigChan receives a signal
	defer func() {
		signal.Stop(sigChan)
		cancel()
	}()

	go func() {
		select {
		case <-sigChan:
			cancel()
		case <-ctx.Done():
		}
	}()

	commands := []struct {
		Name string
		Args []string
	}{
		{"ollama", []string{"serve"}},
		{"redis-stack-server", []string{}},
	}

	// Start subprocesses
	startSubprocesses(ctx, commands)

	// TODO: start background sync process to sync data from external sources to redis

	err := waitForOllama(ctx)
	if err != nil {
		log.Fatalf("Failed to wait for ollama: %s\n", err)
	}

	err = pullModel("llama2", false)
	if err != nil {
		log.Fatalf("Failed to pull model: %s\n", err)
	}

	err = pullModel("mxbai-embed-large", false)
	if err != nil {
		log.Fatalf("Failed to pull model: %s\n", err)
	}

	err = waitForRedis(ctx, redisClient)
	if err != nil {
		log.Fatalf("Failed to wait for redis: %s\n", err)
	}

	// Create index for vector search
	err = createVectorIndex(ctx, redisClient, "lamoidVectorIndex")
	if err != nil {
		log.Fatalf("Failed to create vector index: %s\n", err)
	}

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
	router := setupRouter()
	log.Print("Starting server on port 8081")
	log.Fatal(http.ListenAndServe(":8081", router))
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
	for _, chunk := range chunks {
		resp, err := EmbedFromModel(chunk)
		if err != nil {
			log.Printf("Failed to get embeddings: %s\n", err)
			continue
		}
		embedding := resp.Embedding

		// Add the embedding to the vector index
		addVector(r.Context(), redisClient, "lamoidVector:"+chunk, embedding)
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

func waitForRedis(ctx context.Context, redisClient *redis.Client) error {
	// Poll the Redis client every 5 seconds until the context is cancelled
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled while waiting for Redis: %v", ctx.Err())
		case <-time.After(2 * time.Second): // Check every 2 seconds
			_, err := redisClient.Ping(ctx).Result()
			if err == nil {
				fmt.Println("Redis is now running.")
				return nil
			}
			log.Printf("Waiting for Redis to become healthy: %v", err)
		}
	}
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
			return fmt.Errorf("Context cancelled during wait: %w", ctx.Err())
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
		Model:  "mxbai-embed-large",
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
	log.Printf("Response: %v", string(responseData))

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
		Model:  "llama2",
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

	// Call Ollama embeddings model to get embeddings for the prompt
	resp, err := EmbedFromModel(prompt)
	if err != nil {
		http.Error(w, "Failed to get embeddings", http.StatusInternalServerError)
		return
	}

	embeddings := resp.Embedding
	log.Printf("Embeddings: %v", embeddings)

	// Perform vector similarity search on redis and get list of most relevant results
	vectorSearch(r.Context(), redisClient, "lamoidVectorIndex", embeddings)

	// Use ollama LLM model to rerank the results, pick the top 5

	// Use ollama LLM model with the top 5 results as context, and the prompt

	// Return the completion from the LLM model

	w.WriteHeader(http.StatusOK)
}

// Function to create a vector index
func createVectorIndex(ctx context.Context, client *redis.Client, indexName string) error {
	err := client.Do(ctx, "FT.CREATE", indexName, "ON", "HASH", "PREFIX", 1, "myVector:", "SCHEMA", "vector", "VECTOR", "FLAT", "6", "TYPE", "FLOAT32", "DIM", "4", "DISTANCE_METRIC", "L2").Err()
	if err != nil {
		if strings.Contains(err.Error(), "Index already exists") {
			return nil
		}
		return fmt.Errorf("failed to create vector index: %v", err)
	}
	return nil
}

// Helper function to convert float32 slice to byte slice
func float32SliceToByteSlice(floats []float32) []byte {
	buf := new(bytes.Buffer)
	err := binary.Write(buf, binary.LittleEndian, floats)
	if err != nil {
		log.Fatal("binary.Write failed:", err)
	}
	return buf.Bytes()
}

// Function to add a vector to Redis
func addVector(ctx context.Context, client *redis.Client, key string, vector []float32) {
	// Assuming vector is already in the correct binary format
	byteVector := float32SliceToByteSlice(vector) // Convert vector to byte slice
	err := client.HSet(ctx, key, "vector", byteVector).Err()
	if err != nil {
		log.Fatalf("Failed to add vector: %v", err)
	}
}

// Function to perform a KNN vector search
func vectorSearch(ctx context.Context, client *redis.Client, indexName string, vector []float32) {
	byteVector := float32SliceToByteSlice(vector) // Convert vector to byte slice
	results, err := client.Do(ctx, "FT.SEARCH", indexName, "=>[KNN 3 @vector $vec]", "PARAMS", 2, "vec", byteVector).Result()
	if err != nil {
		log.Fatalf("Failed to search vector: %v", err)
	}
	for _, result := range results.([]interface{}) {
		fmt.Println(result) // Each result might be another slice or a map depending on your query and response
	}
}
