package main

import "log"

var (
	//	httpClient          = &http.Client{Timeout: 10 * time.Second}
	generationModelName = "custom-mistral"
	embeddingsModelName = "nomic-embed-text"
	clean               = true
	KeepAliveTime       = "20m"

	PosthogAPIKey = "n/a" // Will be populated by linker from builder's env
)

func main() {
	// Start everything needed to let the user onboard connectors
	bootCtx, err := BootOnboard()
	if err != nil {
		log.Fatalf("Failed to boot until onboarding: %s\n", err)
	}
	log.Printf("Boot: Ready to onboard connectors")
	defer bootCtx.Logfile.Close()

	// Start everything needed for syncing
	// Pulls embeddings model
	err = BootSyncing(bootCtx)
	if err != nil {
		log.Fatalf("Failed to boot until syncing: %s\n", err)
	}
	log.Printf("Boot: Ready to sync")

	// Start everything needed for generation
	// Pulls generation and reranking models
	err = BootGen(bootCtx)
	if err != nil {
		log.Fatalf("Failed to boot until generation: %s\n", err)
	}
	log.Printf("Boot: Ready to generate")

	<-bootCtx.Done() // Block until the app terminates
}
