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
	"sort"
	"strings"
	"sync"

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

type RerankResponse struct {
	Relevant bool `json:"relevant"`
}

// returns titles ordered by relevance
func rerankModel(prompt string) (bool, error) {
	// URL of the API endpoint
	url := "http://localhost:11434/api/chat"

	messages := []types.HistoryItem{
		{
			Role:    "user",
			Content: prompt,
		},
	}

	// TODO: pass history
	// Create the payload
	payload := RequestPayload{
		Model:     rerankModelName,
		Messages:  messages,
		Stream:    false,
		KeepAlive: KeepAliveTime,
		Format:    "json",
	}

	// Marshal the payload into JSON
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return false, err
	}

	// Create a new HTTP request
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return false, err
	}

	// Set the appropriate headers
	req.Header.Set("Content-Type", "application/json")

	// Make the HTTP request using the default client
	client := &http.Client{}
	response, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()

	// Read the response body
	responseData, err := io.ReadAll(response.Body)
	if err != nil {
		return false, err
	}
	log.Printf("Response: %v", string(responseData))

	// Unmarshal JSON data into ApiResponse struct
	var res ApiResponse
	if err := json.Unmarshal(responseData, &res); err != nil {
		return false, err
	}

	resp := RerankResponse{}
	err = json.Unmarshal([]byte(res.Message.Content), &resp)
	if err != nil {
		return false, fmt.Errorf("failed to unmarshal content: %s", err)
	}

	return resp.Relevant, nil
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
	// Skip chunks with a very low score
	minScore := 0.4

	log.Printf("Rerank: initial chunks %d", len(chunks))
	// Sort chunks by score
	sort.Slice(chunks, func(i, j int) bool {
		return chunks[i].Score > chunks[j].Score
	})

	new_chunks := []*types.Chunk{}
	for _, chunk := range chunks {
		if chunk.Score > minScore {
			new_chunks = append(new_chunks, chunk)
		}
	}
	log.Printf("Rerank: filtered chunks: %d", len(new_chunks))

	rerankedChunks, err := rerankLLM(ctx, new_chunks, query)
	if err != nil {
		return nil, err
	}
	log.Printf("Rerank: reranked chunks: %d", len(rerankedChunks))

	if len(rerankedChunks) == 0 {
		return rerankedChunks, nil
	}

	log.Printf("Rerank: winner chunk: %s", rerankedChunks[0].Name)

	// Keep only the one top result
	// TODO: better reranking and LLMs performance will be needed to avoid
	// mixing up results across chunks
	return rerankedChunks[:1], nil
}

type ChunkForLLM struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

func evalChunk(chunk *types.Chunk, query string) (bool, error) {
	var builder strings.Builder
	builder.WriteString(`You are an AI model designed to determine if a document is relevant to answering a user query. Here is the document:`)
	builder.WriteString("")

	// Loop through each data chunk and append it followed by a newline
	llmChunk := &ChunkForLLM{
		Title:   chunk.Name,
		Content: chunk.Text,
	}

	jsonData, err := json.Marshal(llmChunk)
	if err != nil {
		return false, fmt.Errorf("unable to marshal json: %s", err)
	}

	builder.Write(jsonData)
	builder.WriteString("")

	builder.WriteString(`The query to which these documents should be relevant is: \n`)
	builder.WriteString(query)

	// Append the user query with an instruction
	builder.WriteString(`Determine if the content in the document is directly
	relevant and helpful to answering the user's query. Return a json response
	in the following format if the document is relevant:
		{
			"relevant": True
		}

		Or in the following format if the document is not relevant:
		{
			"relevant": False
		}
		` + "```json\n")

	// Return the final combined prompt
	finalPrompt := builder.String()
	relevant, err := rerankModel(finalPrompt)
	if err != nil {
		return false, err
	}

	logEntry := fmt.Sprintf("%s\n%t\n", finalPrompt, relevant)
	err = WritePromptLog(logEntry)
	if err != nil {
		return false, fmt.Errorf("unable to write prompt to log: %s", err)
	}
	return relevant, nil
}

func rerankLLM(ctx context.Context, chunks []*types.Chunk, query string) ([]*types.Chunk, error) {
	var wg sync.WaitGroup
	chunkChan := make(chan *types.Chunk, len(chunks))
	relevantChan := make(chan *types.Chunk, len(chunks))
	errorChan := make(chan error, len(chunks))

	// Producer: Puts all chunks in the channel to be consumed by workers
	for _, chunk := range chunks {
		chunkChan <- chunk
	}
	close(chunkChan)

	newCtx, cancel := context.WithCancel(ctx)

	// Worker pool
	for i := 0; i < NumConcurrentInferences; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			log.Printf("Starting worker %d", i)
			for chunk := range chunkChan {
				select {
				case <-newCtx.Done():
					return
				default:
				}
				log.Printf("Worker %d, Chunk: %s", i, chunk.Name)
				relevant, err := evalChunk(chunk, query)
				if err != nil {
					errorChan <- fmt.Errorf("unable to evaluate chunk: %s", err)
				} else if relevant {
					relevantChan <- chunk
					cancel()
					return
				}
			}
		}(i)
	}

	// Wait for all workers to complete
	wg.Wait()
	close(relevantChan)
	close(errorChan)

	// Check if there were any errors
	var finalError error
	for err := range errorChan {
		log.Println(err)
		finalError = err
	}

	// Return the relevant chunks or just an empty list
	var relevantChunks []*types.Chunk
	for chunk := range relevantChan {
		relevantChunks = append(relevantChunks, chunk)
	}

	if len(relevantChunks) > 0 {
		return relevantChunks, finalError
	}
	return []*types.Chunk{}, finalError
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
