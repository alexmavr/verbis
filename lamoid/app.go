package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/epochlabs-ai/lamoid/lamoid/store"
	"github.com/epochlabs-ai/lamoid/lamoid/types"
)

var (
	httpClient          = &http.Client{Timeout: 10 * time.Second}
	generationModelName = "phi3"
)

func main() {
	// Define the commands to be executed
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Main context attacked to application runtime, everything in the
	// background should terminate when cancelled
	ctx, cancel := context.WithCancel(context.Background())

	// Start syncer as separate goroutine
	syncer := NewSyncer()
	go syncer.Run(ctx)
	api := API{
		Syncer:  syncer,
		Context: ctx,
	}

	router := api.SetupRouter()
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
		{"../Resources/ollama", []string{"serve"}},
		{"../Resources/weaviate", []string{"--host", "0.0.0.0", "--port", "8088", "--scheme", "http"}},
	}

	// Start subprocesses
	startSubprocesses(ctx, commands)

	// TODO: start background sync process to sync data from external sources

	err := waitForOllama(ctx)
	if err != nil {
		log.Fatalf("Failed to wait for ollama: %s\n", err)
	}

	err = pullModel(generationModelName, false)
	if err != nil {
		log.Fatalf("Failed to pull model: %s\n", err)
	}

	err = pullModel("all-minilm", false)
	if err != nil {
		log.Fatalf("Failed to pull model: %s\n", err)
	}

	// Create index for vector search
	weavClient := store.GetWeaviateClient()
	store.CreateChunkClass(ctx, weavClient)
	store.CreateConnectorStateClass(ctx, weavClient)

	// Perform a test generation with ollama to load the model in memory
	resp, err := chatWithModel("What is the capital of France? Respond in one word only", []types.HistoryItem{})
	if err != nil {
		log.Fatalf("Failed to generate response: %s\n", err)
	}
	if !resp.Done {
		log.Fatalf("Response not done: %v\n", resp)
	}
	if !strings.Contains(resp.Message.Content, "Paris") {
		log.Fatalf("Response does not contain Paris: %v\n", resp.Message.Content)
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

// Function to call ollama model
func chatWithModel(prompt string, history []types.HistoryItem) (*ApiResponse, error) {
	// URL of the API endpoint
	url := "http://localhost:11434/api/chat"

	messages := append(history, types.HistoryItem{
		Role:    "user",
		Content: prompt,
	})

	// TODO: pass history
	// Create the payload
	payload := RequestPayload{
		Model:     generationModelName,
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
