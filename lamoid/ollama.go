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
	"path/filepath"
	"strings"

	"github.com/epochlabs-ai/lamoid/lamoid/types"
	"github.com/epochlabs-ai/lamoid/lamoid/util"
)

const (
	CustomModelPrefix = "custom-"
)

func IsCustomModel(modelName string) bool {
	return strings.HasPrefix(modelName, "custom-")
}

type ModelCreateRequest struct {
	Name      string `json:"name"`
	Modelfile string `json:"modelfile"`
	Stream    bool   `json:"stream"`
}

func createModel(modelName string) error {
	url := "http://localhost:11434/api/create"

	path, err := util.GetDistPath()
	if err != nil {
		return fmt.Errorf("failed to get dist path: %v", err)
	}

	modelFileName := fmt.Sprintf("Modelfile.%s", modelName)
	modelFileData, err := os.ReadFile(filepath.Join(path, modelFileName))
	if err != nil {
		return fmt.Errorf("unable to read modelfile: %v", err)
	}

	log.Printf("Modelfile contents: %s", string(modelFileData))

	payload := ModelCreateRequest{
		Name:      generationModelName,
		Modelfile: string(modelFileData),
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

	// Set the appropriate headers
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
	log.Printf("Response: %v", string(responseData))
	return nil
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
		KeepAlive: KeepAliveTime,
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

type PromptResponse struct {
	Content    string   `json:"content"`
	SourceURLs []string `json:"urls"`
}

func urlsFromChunks(chunks []*types.Chunk) []string {
	urls := []string{}
	for _, chunk := range chunks {
		urls = append(urls, chunk.SourceURL)
	}
	return urls
}

func Rerank(ctx context.Context, chunks []*types.Chunk, query string) ([]*types.Chunk, error) {
	return []*types.Chunk{}, nil
}

// TODO: function calling?
func MakePrompt(chunks []*types.Chunk, query string) string {
	// Create a builder to efficiently concatenate strings
	var builder strings.Builder

	// Append introduction to guide the model's focus
	builder.WriteString("You are a personal chat assistant and you have to answer the following user query: ")
	builder.WriteString(query)

	if len(chunks) == 0 {
		builder.WriteString(`\nNo relevant documents were found to answer the
		user query. You may answer the query based on historical chat but you
		should prefer to say you don't know if you're not sure.`)
		return builder.String()
	}

	builder.WriteString(`You may only use information from the following
	documents to answer the user query. If none of them are relevant say you
	don't know. Answer directly and succintly, keeping a professional tone`)

	// Loop through each data chunk and append it followed by a newline
	for i, chunk := range chunks {
		builder.WriteString(fmt.Sprintf("\n===== Document %d ======\n", i))
		builder.WriteString(fmt.Sprintf("Title: %s\n", chunk.Name))
		builder.WriteString(fmt.Sprintf("Content: %s\n", chunk.Text))
	}

	// Return the final combined prompt
	return builder.String()
}

func WritePromptLog(prompt string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("unable to get user home directory: %w", err)
	}
	path := filepath.Join(home, PromptLogFile)
	// Open the file for writing, creating it if it doesn't exist
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}

	// Write the prompt to the file
	_, err = file.WriteString("\n===\n" + prompt + "\n")
	return err
}
