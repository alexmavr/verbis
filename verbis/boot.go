package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"math"
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

const (
	masterLogPath      = ".verbis/logs/full.log"
	WeaviatePersistDir = ".verbis/synced_data"
	OllamaModelsDir    = ".verbis/ollama/models"
	OllamaRunnersDir   = ".verbis/ollama/runners"
	OllamaTmpDir       = ".verbis/ollama/tmp"

	miscModelsPath    = ".verbis/models"
	rerankerModelName = "ms-marco-MiniLM-L-12-v2"
)

type BootState string

const (
	BootStateStarted = "started"
	BootStateOnboard = "onboard" // Can add connectors
	BootStateSyncing = "syncing" // Pulling and ingesting data from connectors
	BootStateGen     = "generating"
)

type BootContext struct {
	context.Context
	Timers
	State             BootState
	PosthogDistinctID string
	PosthogClient     posthog.Client
	Syncer            *Syncer
	Logfile           *os.File
}

type Timers struct {
	StartTime   time.Time
	OnboardTime time.Time
	SyncingTime time.Time
	GenTime     time.Time
}

func NewBootContext(ctx context.Context) *BootContext {
	startTime := time.Now()
	return &BootContext{
		Context: ctx,
		Timers: Timers{
			StartTime: startTime,
		},
		State:             BootStateStarted,
		PosthogDistinctID: uuid.New().String(),
	}
}

func BootOnboard() (*BootContext, error) {
	path, err := GetMasterLogDir()
	if err != nil {
		log.Fatalf("Failed to get master log directory: %s", err)
	}

	err = os.MkdirAll(filepath.Dir(path), 0755)
	if err != nil && !os.IsExist(err) {
		log.Fatalf("Failed to create log directory: %s", err)
	}

	// Open a file for logging
	logFile, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("Failed to open log file: %s", err)
	}

	os.Stderr = logFile
	os.Stdout = logFile
	log.SetOutput(logFile)

	// Main context attacked to application runtime, everything in the
	// background should terminate when cancelled
	ctx, cancel := context.WithCancel(context.Background())

	bootCtx := NewBootContext(ctx)
	bootCtx.Logfile = logFile

	// Define the commands to be executed
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	// Start syncer as separate goroutine

	postHogClient, err := posthog.NewWithConfig(
		PosthogAPIKey,
		posthog.Config{
			PersonalApiKey:                     PosthogAPIKey,
			Endpoint:                           "https://eu.i.posthog.com",
			DefaultFeatureFlagsPollingInterval: math.MaxInt64, // Max value of time.Duration at 280 years, effectively disabling feature flag polling
		},
	)
	if err != nil {
		log.Fatalf("Failed to create PostHog client: %s\n", err)
	}

	bootCtx.PosthogClient = postHogClient

	syncer := NewSyncer(bootCtx.PosthogClient, bootCtx.PosthogDistinctID)
	if PosthogAPIKey == "n/a" {
		log.Fatalf("Posthog API key not set\n")
	}
	bootCtx.Syncer = syncer
	api := API{
		Syncer:            syncer,
		Posthog:           postHogClient,
		PosthogDistinctID: bootCtx.PosthogDistinctID,
		Context:           bootCtx,
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

	go func() {
		select {
		case <-sigChan:
			log.Print("Sigchan closed")
			cancel()
			server.Close()
		case <-ctx.Done():
			server.Close()
		}
	}()

	path, err = util.GetDistPath()
	if err != nil {
		log.Fatalf("Failed to get dist path: %s\n", err)
	}
	ollamaPath := filepath.Join(path, util.OllamaFile)
	weaviatePath := filepath.Join(path, util.WeaviateFile)

	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("unable to get user home directory: %s", err)
	}
	weaviatePersistDir := filepath.Join(home, WeaviatePersistDir)
	ollamaModelsPath := filepath.Join(home, OllamaModelsDir)
	ollamaRunnersPath := filepath.Join(home, OllamaRunnersDir)
	ollamaTmpDirPath := filepath.Join(home, OllamaTmpDir)

	commands := []CmdSpec{
		{
			ollamaPath,
			[]string{"serve"},
			[]string{
				"OLLAMA_HOST=" + OllamaHost,
				"OLLAMA_KEEP_ALIVE=" + KeepAliveTime,
				"OLLAMA_MAX_LOADED_MODELS=2", // Embeddings & LLM
				"OLLAMA_NUM_PARALLEL=11",     // Max num of parallel items across connectors + 1 for active prompts
				"OLLAMA_FLASH_ATTENTION=1",   // Speeds up token generation for apple silicon macs
				"OLLAMA_MODELS=" + ollamaModelsPath,
				"OLLAMA_RUNNERS_DIR=" + ollamaRunnersPath,
				"OLLAMA_TMPDIR=" + ollamaTmpDirPath,
			},
		},
		{
			weaviatePath,
			[]string{"--host", "0.0.0.0", "--port", "8088", "--scheme", "http"},
			[]string{
				"LIMIT_RESOURCES=true",
				"PERSISTENCE_DATA_PATH=" + weaviatePersistDir,
				"AUTHENTICATION_ANONYMOUS_ACCESS_ENABLED=true",
			},
		},
	}

	// Start subprocesses
	startSubprocesses(ctx, commands, logFile, logFile)

	err = waitForWeaviate(ctx)
	if err != nil {
		log.Fatalf("Failed to wait for Weaviate: %s\n", err)
	}

	// Create store schemas
	weavClient := store.GetWeaviateClient()
	store.CreateDocumentClass(ctx, weavClient, clean)
	store.CreateConnectorStateClass(ctx, weavClient, clean)
	store.CreateChunkClass(ctx, weavClient, clean)
	store.CreateConversationClass(ctx, weavClient, clean)

	// Start HTTP server
	go func() {
		log.Print("Starting server on port 8081")
		log.Fatal(server.ListenAndServe())
	}()

	bootCtx.State = BootStateOnboard
	bootCtx.OnboardTime = time.Now()
	return bootCtx, nil
}

func waitForOllama(ctx context.Context) error {
	ollama_url := fmt.Sprintf("http://%s", OllamaHost)
	httpClient := &http.Client{Timeout: 10 * time.Second}

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

func startSubprocesses(ctx context.Context, commands []CmdSpec, stdout *os.File, stderr *os.File) {
	for _, cmdConfig := range commands {
		go func(c CmdSpec) {
			cmd := exec.Command(c.Name, c.Args...)
			cmd.Env = append(os.Environ(), c.Env...)
			cmd.Stdout = stdout
			cmd.Stderr = stderr

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

func BootSyncing(ctx *BootContext) error {
	err := waitForOllama(ctx)
	if err != nil {
		log.Fatalf("Failed to wait for ollama: %s\n", err)
	}

	err = initModels([]string{embeddingsModelName})
	if err != nil {
		log.Fatalf("Failed to initialize models: %s\n", err)
	}

	err = ctx.Syncer.Init(ctx)
	if err != nil {
		log.Fatalf("Failed to initialize syncer: %s\n", err)
	}
	go ctx.Syncer.Run(ctx)

	ctx.State = BootStateSyncing
	ctx.SyncingTime = time.Now()
	return nil
}

func BootGen(ctx *BootContext) error {
	err := copyRerankerModel()
	if err != nil {
		log.Fatalf("Failed to copy reranker model: %s\n", err)
	}

	err = initModels([]string{generationModelName})
	if err != nil {
		log.Fatalf("Failed to initialize models: %s\n", err)
	}

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

	// Identify user to posthog
	systemStats, err := getSystemStats()
	if err != nil {
		log.Fatalf("Failed to get system stats: %s\n", err)
	}
	err = ctx.PosthogClient.Enqueue(posthog.Identify{
		DistinctId: ctx.PosthogDistinctID,
		Properties: posthog.NewProperties().
			Set("chipset", systemStats.Chipset).
			Set("macos", systemStats.MacOS).
			Set("memsize", systemStats.Memsize),
		// TODO: version
	})
	if err != nil {
		log.Fatalf("Failed to enqueue identify event: %s\n", err)
	}

	ctx.GenTime = time.Now()
	err = ctx.PosthogClient.Enqueue(posthog.Capture{
		DistinctId: ctx.PosthogDistinctID,
		Event:      "Started",
		Properties: posthog.NewProperties().
			// TODO: connector states
			Set("boot_total_duration", ctx.GenTime.Sub(ctx.StartTime).String()).
			Set("boot_onboard_duration", ctx.OnboardTime.Sub(ctx.StartTime).String()).
			Set("boot_syncing_duration", ctx.SyncingTime.Sub(ctx.OnboardTime).String()).
			Set("boot_gen_duration", ctx.GenTime.Sub(ctx.SyncingTime).String()),
	})
	if err != nil {
		log.Fatalf("Failed to enqueue event: %s\n", err)
	}

	ctx.State = BootStateGen
	return nil
}

func waitForWeaviate(ctx context.Context) error {
	weaviate_url := "http://localhost:8088/v1/.well-known/ready"
	httpClient := &http.Client{Timeout: 10 * time.Second}

	for {
		resp, err := httpClient.Get(weaviate_url)
		log.Print(resp)
		if err == nil {
			log.Printf("Weaviate is up and running")
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
func Halt(bootCtx *BootContext, sigChan chan os.Signal, cancel context.CancelFunc) {
	signal.Stop(sigChan)
	cancel()
	close(sigChan)
	defer bootCtx.PosthogClient.Close()
}

func GetMasterLogDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("unable to get user home directory: %w", err)
	}
	return filepath.Join(home, masterLogPath), nil
}

func copyRerankerModel() error {
	distPath, err := util.GetDistPath()
	if err != nil {
		return fmt.Errorf("failed to get dist path: %w", err)
	}
	rerankerDirPath := filepath.Join(distPath, rerankerModelName)

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("unable to get user home directory: %w", err)
	}
	targetModelDir := filepath.Join(home, miscModelsPath, rerankerModelName)

	err = os.Mkdir(targetModelDir, 0755)
	if err != nil && !os.IsExist(err) {
		return fmt.Errorf("failed to create target model directory: %w", err)
	}

	err = copyDir(rerankerDirPath, targetModelDir)
	if err != nil {
		return fmt.Errorf("failed to copy reranker model: %w", err)
	}

	return nil
}

// copyDir recursively copies a directory from src to dst.
func copyDir(src string, dst string) error {
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		destPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(destPath, info.Mode())
		}

		return copyFile(path, destPath)
	})

	if err != nil {
		return err
	}

	return nil
}

// copyFile copies a file from src to dst.
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		return err
	}

	info, err := os.Stat(src)
	if err != nil {
		return err
	}

	return os.Chmod(dst, info.Mode())
}
