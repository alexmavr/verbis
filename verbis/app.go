package main

import (
	"log"

	"github.com/verbis-ai/verbis/verbis/types"
)

var (
	//	httpClient          = &http.Client{Timeout: 10 * time.Second}
	generationModelName = "custom-mistral"
	embeddingsModelName = "snowflake-arctic-embed:137m"
	clean               = true
	KeepAliveTime       = "20m"

	// Will be populated by linker from .builder.env
	PosthogAPIKey     = "n/a"
	AzureSecretID     = "n/a"
	AzureSecretValue  = "n/a"
	SlackClientID     = "n/a"
	SlackClientSecret = "n/a"
)

func main() {
	creds := types.BuildCredentials{
		PosthogAPIKey:     PosthogAPIKey,
		AzureSecretID:     AzureSecretID,
		AzureSecretValue:  AzureSecretValue,
		SlackClientID:     SlackClientID,
		SlackClientSecret: SlackClientSecret,
	}
	// Start everything needed to let the user onboard connectors
	bootCtx, err := BootOnboard(creds)
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
