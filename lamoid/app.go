package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

var (
	httpClient = &http.Client{Timeout: 10 * time.Second}
)

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

	err = pullModel("llama3", false)
	if err != nil {
		log.Fatalf("Failed to pull model: %s\n", err)
	}

	err = pullModel("all-minilm", false)
	if err != nil {
		log.Fatalf("Failed to pull model: %s\n", err)
	}

	// Create index for vector search
	createWeaviateSchema(ctx, getWeaviateClient())

	// Perform a test generation with ollama to load the model in memory
	resp, err := chatWithModel("What is the capital of France?", []HistoryItem{})
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
