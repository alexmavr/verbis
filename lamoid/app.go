package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/handlers"

	"github.com/epochlabs-ai/lamoid/lamoid/store"
	"github.com/epochlabs-ai/lamoid/lamoid/types"
	"github.com/epochlabs-ai/lamoid/lamoid/util"
)

var (
	httpClient          = &http.Client{Timeout: 10 * time.Second}
	generationModelName = "custom-mistral"
	rerankModelName     = "custom-mistral"
	embeddingsModelName = "nomic-embed-text"
	clean               = false
	KeepAliveTime       = "20m"
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

	// Apply CORS middleware for npm run start
	// TODO: only do this in development
	corsHeaders := handlers.CORS(
		handlers.AllowedOrigins([]string{"http://localhost:3000"}),                   // Allow requests from Electron app
		handlers.AllowedMethods([]string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}), // Allow these methods
		handlers.AllowedHeaders([]string{"Content-Type", "Authorization"}),           // Allow these headers
	)
	handler := corsHeaders(router)

	server := http.Server{
		Addr:    ":8081",
		Handler: handler,
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

	path, err := util.GetDistPath()
	if err != nil {
		log.Fatalf("Failed to get dist path: %s\n", err)
	}
	ollamaPath := filepath.Join(path, util.OllamaFile)
	weaviatePath := filepath.Join(path, util.WeaviateFile)

	// Weaviate flags
	os.Setenv("PERSISTENCE_DATA_PATH", "/tmp/lamoid")
	os.Setenv("AUTHENTICATION_ANONYMOUS_ACCESS_ENABLED", "true")
	commands := []struct {
		Name string
		Args []string
	}{
		{ollamaPath, []string{"serve"}},
		{weaviatePath, []string{"--host", "0.0.0.0", "--port", "8088", "--scheme", "http"}},
	}

	// Start subprocesses
	startSubprocesses(ctx, commands)

	err = waitForOllama(ctx)
	if err != nil {
		log.Fatalf("Failed to wait for ollama: %s\n", err)
	}

	if IsCustomModel(generationModelName) {
		err = createModel(generationModelName)
		if err != nil {
			log.Fatalf("Failed to create model: %s\n", err)
		}
	} else {
		err = pullModel(generationModelName, false)
		if err != nil {
			log.Fatalf("Failed to pull model: %s\n", err)
		}
	}

	err = pullModel(embeddingsModelName, false)
	if err != nil {
		log.Fatalf("Failed to pull model: %s\n", err)
	}

	// Create indices for vector search
	weavClient := store.GetWeaviateClient()
	store.CreateChunkClass(ctx, weavClient, clean)
	store.CreateConnectorStateClass(ctx, weavClient, clean)

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
