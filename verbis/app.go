package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/handlers"
	"github.com/posthog/posthog-go"

	"github.com/verbis-ai/verbis/verbis/store"
	"github.com/verbis-ai/verbis/verbis/types"
	"github.com/verbis-ai/verbis/verbis/util"
)

var (
	httpClient          = &http.Client{Timeout: 10 * time.Second}
	generationModelName = "custom-mistral"
	embeddingsModelName = "nomic-embed-text"
	clean               = true
	KeepAliveTime       = "20m"

	PosthogAPIKey = "n/a" // Will be populated by linker from builder's env
)

func main() {
	startTime := time.Now()
	postHogDistinctID := uuid.New().String()

	// Define the commands to be executed
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Main context attacked to application runtime, everything in the
	// background should terminate when cancelled
	ctx, cancel := context.WithCancel(context.Background())

	// Start syncer as separate goroutine
	syncer := NewSyncer()
	if PosthogAPIKey == "n/a" {
		log.Fatalf("Posthog API key not set\n")
	}

	postHogClient, err := posthog.NewWithConfig(
		PosthogAPIKey,
		posthog.Config{
			PersonalApiKey: PosthogAPIKey,
			Endpoint:       "https://eu.i.posthog.com",
		},
	)
	if err != nil {
		log.Fatalf("Failed to create PostHog client: %s\n", err)
	}
	defer postHogClient.Close()

	api := API{
		Syncer:            syncer,
		Context:           ctx,
		Posthog:           postHogClient,
		PosthogDistinctID: postHogDistinctID,
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
	commands := []CmdSpec{
		{
			ollamaPath,
			[]string{"serve"},
			[]string{"OLLAMA_KEEP_ALIVE=" + KeepAliveTime},
		},
		{
			weaviatePath,
			[]string{"--host", "0.0.0.0", "--port", "8088", "--scheme", "http"},
			[]string{
				"PERSISTENCE_DATA_PATH=/tmp/verbis",
				"AUTHENTICATION_ANONYMOUS_ACCESS_ENABLED=true",
			},
		},
	}

	// Start subprocesses
	startSubprocesses(ctx, commands)

	err = waitForOllama(ctx)
	if err != nil {
		log.Fatalf("Failed to wait for ollama: %s\n", err)
	}

	err = initModels([]string{generationModelName, embeddingsModelName})
	if err != nil {
		log.Fatalf("Failed to initialize models: %s\n", err)
	}

	// Create indices for vector search
	weavClient := store.GetWeaviateClient()
	store.CreateDocumentClass(ctx, weavClient, clean)
	store.CreateConnectorStateClass(ctx, weavClient, clean)
	store.CreateChunkClass(ctx, weavClient, clean)

	err = syncer.Init(ctx)
	if err != nil {
		log.Fatalf("Failed to initialize syncer: %s\n", err)
	}
	go syncer.Run(ctx)

	// Perform a test generation with ollama to load the model in memory
	resp, err := chatWithModel("What is the capital of France? Respond in one word only", generationModelName, []types.HistoryItem{})
	if err != nil {
		log.Fatalf("Failed to generate response: %s\n", err)
	}
	if !resp.Done {
		log.Fatalf("Response not done: %v\n", resp)
	}
	if !strings.Contains(resp.Message.Content, "Paris") {
		log.Fatalf("Response does not contain Paris: %v\n", resp.Message.Content)
	}

	// Perform a test rerank to download the model
	rerankOutput, err := RunRerankModel(ctx, []byte{})
	if err != nil {
		log.Fatalf("Failed to run rerank model: %s\n", err)
	}
	log.Print(string(rerankOutput))
	log.Print("Rerank model loaded successfully")

	endTime := time.Now()

	// Identify user to posthog
	systemStats, err := getSystemStats()
	if err != nil {
		log.Fatalf("Failed to get system stats: %s\n", err)
	}
	err = postHogClient.Enqueue(posthog.Identify{
		DistinctId: postHogDistinctID,
		Properties: posthog.NewProperties().
			Set("chipset", systemStats.Chipset).
			Set("macos", systemStats.MacOS).
			Set("memsize", systemStats.Memsize),
		// TODO: version
	})
	if err != nil {
		log.Fatalf("Failed to enqueue identify event: %s\n", err)
	}

	err = postHogClient.Enqueue(posthog.Capture{
		DistinctId: postHogDistinctID,
		Event:      "Started",
		Properties: posthog.NewProperties().
			// TODO: connector states
			Set("start_duration", endTime.Sub(startTime).String()),
	})
	if err != nil {
		log.Fatalf("Failed to enqueue event: %s\n", err)
	}

	// Start HTTP server
	log.Print("Starting server on port 8081")
	log.Fatal(server.ListenAndServe())
}

type SystemStats struct {
	Chipset string
	MacOS   string
	Memsize string
}

func getSystemStats() (*SystemStats, error) {
	chipsetCmd := exec.Command("sysctl", "-n", "machdep.cpu.brand_string")
	chipsetOut, err := chipsetCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get chipset info: %v", err)
	}
	chipset := strings.TrimSpace(string(chipsetOut))

	// Retrieve macOS version
	versionCmd := exec.Command("sw_vers", "-productVersion")
	versionOut, err := versionCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get macOS version: %v", err)
	}
	macos := strings.TrimSpace(string(versionOut))

	// Retrieve system memory information
	memCmd := exec.Command("sysctl", "-n", "hw.memsize")
	memOut, err := memCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get memory info: %v", err)
	}
	memGB := strings.TrimSpace(string(memOut))

	return &SystemStats{
		Chipset: chipset,
		MacOS:   macos,
		Memsize: memGB,
	}, nil
}

type CmdSpec struct {
	Name string
	Args []string
	Env  []string
}

func startSubprocesses(ctx context.Context, commands []CmdSpec) {
	for _, cmdConfig := range commands {
		go func(c CmdSpec) {
			cmd := exec.Command(c.Name, c.Args...)
			cmd.Env = append(os.Environ(), c.Env...)
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

func initModels(models []string) error {
	for _, modelName := range models {
		if IsCustomModel(modelName) {
			err := createModel(modelName)
			if err != nil {
				return fmt.Errorf("failed to create model %s: %v", modelName, err)
			}
		} else {
			err := pullModel(modelName, false)
			if err != nil {
				return fmt.Errorf("failed to pull model %s: %v", modelName, err)
			}
		}
	}
	return nil
}
