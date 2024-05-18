package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/posthog/posthog-go"

	"github.com/verbis-ai/verbis/verbis/connectors"
	"github.com/verbis-ai/verbis/verbis/store"
	"github.com/verbis-ai/verbis/verbis/types"
)

var (
	PromptLogFile        = ".verbis/logs/prompt.log" // Relative to home
	MaxNumRerankedChunks = 3
)

type API struct {
	Syncer            *Syncer
	Context           context.Context
	Posthog           posthog.Client
	PosthogDistinctID string
}

func (a *API) SetupRouter() *mux.Router {
	r := mux.NewRouter()
	r.HandleFunc("/connectors", a.connectorsList).Methods("GET")
	r.HandleFunc("/connectors/{type}/init", a.connectorInit).Methods("GET")
	// TODO: auth_setup and callback are theoretically per connector and not per
	// connector type. The ID of the connector should be inferred and passed as
	// a state variable in the oauth flow.
	r.HandleFunc("/connectors/{connector_id}/auth_setup", a.connectorAuthSetup).Methods("GET")
	r.HandleFunc("/connectors/{connector_id}/callback", a.handleConnectorCallback).Methods("GET")
	r.HandleFunc("/connectors/auth_complete", a.authComplete).Methods("GET")
	r.HandleFunc("/prompt", a.handlePrompt).Methods("POST")
	r.HandleFunc("/health", a.health).Methods("GET")

	r.HandleFunc("/sync/force", a.forceSync).Methods("GET")

	return r
}

func (a *API) health(w http.ResponseWriter, r *http.Request) {
	// TODO: check for health of subprocesses
	// TODO: return state of syncs and model downloads, to be used during init
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func (a *API) connectorsList(w http.ResponseWriter, r *http.Request) {
	states, err := a.Syncer.GetConnectorStates(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Failed to list connectors: " + err.Error()))
		return
	}

	b, err := json.Marshal(states)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Failed to marshal connectors: " + err.Error()))
		return
	}

	w.Write(b)
}

func (a *API) authComplete(w http.ResponseWriter, r *http.Request) {
	// TODO: render page telling the user to go back to the desktop app
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Auth complete"))
}

func (a *API) connectorInit(w http.ResponseWriter, r *http.Request) {
	// Should not error when called accidentally multiple times
	// Can be re-invoked to re-init the connector (i.e. to reset stuck syncing state)
	vars := mux.Vars(r)
	connectorType, ok := vars["type"]
	if !ok {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("No connector name provided"))
		return
	}

	constructor, ok := connectors.AllConnectors[connectorType]
	if !ok {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Unknown connector name"))
		return
	}

	// Create a new connector object and initialize it
	// The Init method is responsible for picking up existing configuration from
	// the store, and discovering credentials
	conn := constructor()
	err := conn.Init(r.Context(), "")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Failed to init connector: " + err.Error()))
		return
	}

	// Add the connector to the syncer so that it may start syncing
	err = a.Syncer.AddConnector(conn)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Failed to add connector: " + err.Error()))
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(fmt.Sprintf(`{"id": "%s"}`, conn.ID())))
}

func (a *API) connectorAuthSetup(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	connectorID, ok := vars["connector_id"]
	if !ok {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("No connector ID provided"))
		return
	}

	conn := a.Syncer.GetConnector(connectorID)
	if conn == nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Unknown connector ID"))
		return
	}
	err := conn.AuthSetup(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Failed to perform initial auth with google: " + err.Error()))
		return
	}

	state, err := conn.Status(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Failed to get connector state: " + err.Error()))
		return
	}

	state.AuthValid = true
	err = conn.UpdateConnectorState(r.Context(), state)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Failed to update connector state: " + err.Error()))
		return
	}

}

func (a *API) handleConnectorCallback(w http.ResponseWriter, r *http.Request) {
	queryParts := r.URL.Query()
	// Google returns it as "code"
	code := queryParts.Get("code")
	if code == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("No code in request"))
		return
	}

	errStr := queryParts.Get("error")
	if errStr != "" {
		log.Printf("Error in Google callback: %s\n", errStr)
	}

	vars := mux.Vars(r)
	connectorID, ok := vars["connector_id"]
	if !ok {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("No connector name provided"))
		return
	}

	state := queryParts.Get("state")
	// If any state is provided it must match the connector ID
	if state != "" && state != connectorID {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("State does not match connector ID"))
		return
	}

	conn := a.Syncer.GetConnector(connectorID)
	if conn == nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Unknown connector ID"))
		return
	}
	err := conn.AuthCallback(r.Context(), code)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Failed to authenticate with Google: " + err.Error()))
		return
	}

	// TODO: Render a done page
	w.Write([]byte("Google authentication is complete, you may close this tab and return to the Verbis desktop app"))
}

func (a *API) forceSync(w http.ResponseWriter, r *http.Request) {
	err := a.Syncer.SyncNow(a.Context)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Failed to sync: " + err.Error()))
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
		Model:  embeddingsModelName,
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
	Model     string              `json:"model"`
	Messages  []types.HistoryItem `json:"messages"`
	Stream    bool                `json:"stream"`
	KeepAlive string              `json:"keep_alive"`
	Format    string              `json:"format"`
}

// Struct to define the API response format
type ApiResponse struct {
	Model              string            `json:"model"`
	CreatedAt          time.Time         `json:"created_at"`
	Message            types.HistoryItem `json:"message"`
	Done               bool              `json:"done"`
	Context            []int             `json:"context"`
	TotalDuration      int64             `json:"total_duration"`
	LoadDuration       int64             `json:"load_duration"`
	PromptEvalCount    int               `json:"prompt_eval_count"`
	PromptEvalDuration int64             `json:"prompt_eval_duration"`
	EvalCount          int               `json:"eval_count"`
	EvalDuration       int64             `json:"eval_duration"`
}

type PromptRequest struct {
	Prompt  string              `json:"prompt"`
	History []types.HistoryItem `json:"history"`
}

type StreamResponseHeader struct {
	Sources []map[string]string `json:"sources"` // Only returned on the first response
}

func (a *API) handlePrompt(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	var promptReq PromptRequest
	defer r.Body.Close()
	err := json.NewDecoder(r.Body).Decode(&promptReq)
	if err != nil {
		// return HTTP 400 bad request
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Failed to decode request"))
	}

	w.Header().Set("Content-Type", "application/json")

	// Call Ollama embeddings model to get embeddings for the prompt
	resp, err := EmbedFromModel(promptReq.Prompt)
	if err != nil {
		http.Error(w, "Failed to get embeddings", http.StatusInternalServerError)
		return
	}
	embedTime := time.Now()

	embeddings := resp.Embedding
	log.Printf("Performing vector search")

	// Perform vector similarity search and get list of most relevant results
	searchResults, err := store.HybridSearch(
		r.Context(),
		store.GetWeaviateClient(),
		promptReq.Prompt,
		embeddings,
	)
	if err != nil {
		http.Error(w, "Failed to search for vectors", http.StatusInternalServerError)
		return
	}
	searchTime := time.Now()

	// Rerank the results
	rerankedChunks, err := Rerank(r.Context(), searchResults, promptReq.Prompt)
	if err != nil {
		log.Printf("Failed to rerank search results: %s", err)
		http.Error(w, "Failed to rerank search results", http.StatusInternalServerError)
		return
	}
	rerankTime := time.Now()

	if len(rerankedChunks) > MaxNumRerankedChunks {
		rerankedChunks = rerankedChunks[:MaxNumRerankedChunks]
	}

	llmPrompt := MakePrompt(rerankedChunks, promptReq.Prompt)
	log.Printf("LLM Prompt: %s", llmPrompt)
	err = WritePromptLog(llmPrompt)
	if err != nil {
		log.Printf("Failed to write prompt to log: %s", err)
		http.Error(w, "Failed to write prompt to log", http.StatusInternalServerError)
		return
	}

	streamChan := make(chan StreamResponse)
	err = chatWithModelStream(r.Context(), llmPrompt, generationModelName, promptReq.History, streamChan)
	if err != nil {
		log.Printf("Failed to generate response: %s", err)
		http.Error(w, "Failed to generate response", http.StatusInternalServerError)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		// TODO: if we run into this, fall back to non-streaming
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// First write the header response
	err = json.NewEncoder(w).Encode(StreamResponseHeader{
		Sources: sourcesFromChunks(rerankedChunks),
	})
	if err != nil {
		http.Error(w, "Failed to write response", http.StatusInternalServerError)
		return
	}

	// Write a newline after the header
	_, err = w.Write([]byte("\n"))
	if err != nil {
		http.Error(w, "Failed to write newline", http.StatusInternalServerError)
		return
	}

	timeToFirstToken := time.Time{}
	responseAcc := ""
	streamCount := 0
	for item := range streamChan {
		if timeToFirstToken.IsZero() {
			timeToFirstToken = time.Now()
		}
		streamCount++
		responseAcc += item.Message.Content
		json.NewEncoder(w).Encode(item)
		_, err = w.Write([]byte("\n"))
		if err != nil {
			http.Error(w, "Failed to write newline", http.StatusInternalServerError)
			return
		}
		flusher.Flush()
	}

	err = WritePromptLog(responseAcc)
	if err != nil {
		log.Printf("Failed to write prompt to log: %s", err)
		http.Error(w, "Failed to write prompt to log", http.StatusInternalServerError)
		return
	}
	doneTime := time.Now()

	err = a.Posthog.Enqueue(posthog.Capture{
		DistinctId: a.PosthogDistinctID,
		Event:      "Prompt",
		Properties: posthog.NewProperties().
			Set("total_duration", doneTime.Sub(startTime).String()).
			Set("1.search_duration", searchTime.Sub(embedTime).String()).
			Set("2.embed_duration", embedTime.Sub(startTime).String()).
			Set("3.rerank_duration", rerankTime.Sub(searchTime).String()).
			Set("4.gen_ttft_duration", timeToFirstToken.Sub(rerankTime).String()).
			Set("5.gen_stream_duration", doneTime.Sub(timeToFirstToken).String()).
			Set("ttft_duration", timeToFirstToken.Sub(startTime).String()).
			Set("gen_sum_duration", doneTime.Sub(rerankTime).String()).
			Set("num_search_results", len(searchResults)).
			Set("num_reranked_results", len(rerankedChunks)).
			Set("num_streamed_events", streamCount),
	})
	if err != nil {
		log.Printf("Failed to enqueue event: %s\n", err)
		http.Error(w, "Failed to enqueue event", http.StatusInternalServerError)
		return
	}
	log.Printf("End of handlePrompt")
}
