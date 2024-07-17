package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/posthog/posthog-go"

	"github.com/verbis-ai/verbis/verbis/connectors"
	"github.com/verbis-ai/verbis/verbis/types"
)

var (
	PromptLogFile = ".verbis/logs/prompt.log" // Relative to home
)

type API struct {
	Syncer            *Syncer
	Context           *BootContext
	Posthog           posthog.Client
	PosthogDistinctID string
	Version           string
	store             types.Store
}

func (a *API) SetupRouter() *mux.Router {
	r := mux.NewRouter()
	r.HandleFunc("/connectors", a.connectorsList).Methods("GET")
	r.HandleFunc("/connectors/{type}/init", a.connectorInit).Methods("GET")
	r.HandleFunc("/connectors/{type}/request", a.connectorRequest).Methods("GET")
	// TODO: auth_setup and callback are theoretically per connector and not per
	// connector type. The ID of the connector should be inferred and passed as
	// a state variable in the oauth flow.
	r.HandleFunc("/connectors/{connector_id}/auth_setup", a.connectorAuthSetup).Methods("GET")
	r.HandleFunc("/connectors/{connector_id}/callback", a.handleConnectorCallback).Methods("GET")
	r.HandleFunc("/connectors/{connector_id}", a.handleConnectorDelete).Methods("DELETE")
	r.HandleFunc("/connectors/auth_complete", a.authComplete).Methods("GET")

	r.HandleFunc("/conversations", a.listConversations).Methods("GET")
	r.HandleFunc("/conversations/{conversation_id}", a.getConversation).Methods("GET")
	r.HandleFunc("/conversations", a.createConversation).Methods("POST")
	r.HandleFunc("/conversations/{conversation_id}/prompt", a.handlePrompt).Methods("POST")

	r.HandleFunc("/config", a.getConfig).Methods("GET")
	r.HandleFunc("/config", a.updateConfig).Methods("POST")

	r.HandleFunc("/health", a.health).Methods("GET")
	r.HandleFunc("/sync/force", a.forceSync).Methods("GET")
	r.HandleFunc("/internal/reinit", a.reInit).Methods("POST")

	return r
}

type HealthResponse struct {
	BootState BootState `json:"boot_state"`
	Version   string    `json:"version"`
}

func (a *API) health(w http.ResponseWriter, r *http.Request) {
	// TODO: check for health of subprocesses
	// TODO: return state of syncs and model downloads, to be used during init
	json.NewEncoder(w).Encode(HealthResponse{
		BootState: a.Context.State,
		Version:   a.Version,
	})
}

func (a *API) getConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := a.store.GetConfig(r.Context())
	if err != nil {
		log.Printf("Failed to get config: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Failed to get config: " + err.Error()))
		return
	}

	err = json.NewEncoder(w).Encode(cfg)
	if err != nil {
		log.Printf("Failed to encode config: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Failed to encode config: " + err.Error()))
		return
	}
}

func (a *API) updateConfig(w http.ResponseWriter, r *http.Request) {
	var cfg *types.Config
	defer r.Body.Close()
	err := json.NewDecoder(r.Body).Decode(&cfg)
	if err != nil {
		// return HTTP 400 bad request
		http.Error(w, "Failed to decode request", http.StatusBadRequest)
		return
	}

	if cfg == nil {
		http.Error(w, "No config provided", http.StatusBadRequest)
		return
	}

	err = a.store.UpdateConfig(r.Context(), cfg)
	if err != nil {
		log.Printf("Failed to update config: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Failed to update config: " + err.Error()))
		return
	}

	if cfg.EnableTelemetry && a.Posthog == nil {
		postHogClient, err := posthog.NewWithConfig(
			PosthogAPIKey,
			posthog.Config{
				PersonalApiKey:                     PosthogAPIKey,
				Endpoint:                           "https://eu.i.posthog.com",
				DefaultFeatureFlagsPollingInterval: math.MaxInt64,
			},
		)
		if err != nil {
			log.Printf("Failed to create posthog client: %s", err)
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Failed to create posthog client: " + err.Error()))
		}
		a.Posthog = postHogClient
		a.Syncer.posthogClient = postHogClient
	}

	if !cfg.EnableTelemetry && a.Posthog != nil {
		a.Posthog = nil
		a.Syncer.posthogClient = nil
	}
}

func (a *API) reInit(w http.ResponseWriter, r *http.Request) {
	// Re-init the syncer, which reloads all connectors from weaviate. Used
	// during the restore operation from a weaviate backup.
	err := a.Syncer.Init(a.Context)
	if err != nil {
		log.Printf("Failed to reinit syncer: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Failed to reinit syncer: " + err.Error()))
		return
	}
	log.Printf("Syncer reinitialized")
	w.WriteHeader(http.StatusOK)
}

func (a *API) connectorRequest(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	connectorType, ok := vars["type"]
	if !ok {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("No connector name provided"))
		return
	}

	if a.Posthog == nil {
		return
	}

	err := a.Posthog.Enqueue(posthog.Capture{
		DistinctId: a.PosthogDistinctID,
		Event:      "ConnectorRequest",
		Properties: posthog.NewProperties().
			Set("connector_type", connectorType).
			Set("version", a.Version),
	})
	if err != nil {
		log.Printf("Failed to enqueue connector request: %s\n", err)
		http.Error(w, "Failed to enqueue connector request", http.StatusInternalServerError)
		return
	}
}

func (a *API) listConversations(w http.ResponseWriter, r *http.Request) {
	conversations, err := a.store.ListConversations(r.Context())
	if err != nil {
		log.Printf("Failed to list conversations: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Failed to list conversations: " + err.Error()))
		return
	}

	b, err := json.Marshal(conversations)
	if err != nil {
		log.Printf("Failed to marshal conversations: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Failed to marshal conversations: " + err.Error()))
		return
	}

	w.Write(b)
}

func (a *API) getConversation(w http.ResponseWriter, r *http.Request) {
	conversationID := mux.Vars(r)["conversation_id"]
	conversation, err := a.store.GetConversation(r.Context(), conversationID)
	if err != nil {
		log.Printf("Failed to get conversation: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Failed to get conversation: " + err.Error()))
		return
	}

	b, err := json.Marshal(conversation)
	if err != nil {
		log.Printf("Failed to marshal conversation: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Failed to marshal conversation: " + err.Error()))
		return
	}

	w.Write(b)
}

func (a *API) createConversation(w http.ResponseWriter, r *http.Request) {
	conversationID, err := a.store.CreateConversation(r.Context())
	if err != nil {
		log.Printf("Failed to create conversation: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Failed to create conversation: " + err.Error()))
		return
	}

	b, err := json.Marshal(map[string]string{"id": conversationID})
	if err != nil {
		log.Printf("Failed to marshal conversation: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Failed to marshal conversation: " + err.Error()))
		return
	}

	w.Write(b)
}

func (a *API) connectorsList(w http.ResponseWriter, r *http.Request) {
	fetch_all := r.URL.Query().Get("all") == "true"
	states, err := a.Syncer.GetConnectorStates(r.Context(), fetch_all)

	if err != nil {
		log.Printf("Failed to list connectors: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Failed to list connectors: " + err.Error()))
		return
	}

	b, err := json.Marshal(states)
	if err != nil {
		log.Printf("Failed to marshal connectors: %s", err)
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
	conn := constructor(a.Context.Credentials, a.store)

	log.Printf("Initializing connector type: %s id: %s", conn.Type(), conn.ID())

	err := conn.Init(a.Context, "")
	if err != nil {
		log.Printf("Failed to init connector: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Failed to init connector: " + err.Error()))
		return
	}
	// Add the connector to the syncer so that it may start syncing
	err = a.Syncer.AddConnector(conn)
	if err != nil {
		log.Printf("Failed to add connector: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Failed to add connector: " + err.Error()))
		return
	}
	log.Printf("Connector %s %s initialized", conn.Type(), conn.ID())

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
		log.Printf("Failed to perform initial auth with google: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Failed to perform initial auth with google: " + err.Error()))
		return
	}

}

func (a *API) handleConnectorDelete(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	connectorID, ok := vars["connector_id"]
	if !ok {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("No connector ID provided"))
		return
	}
	err := a.Syncer.DeleteConnector(a.Context, connectorID)
	if err != nil {
		log.Printf("Failed to remove connector: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Failed to remove connector: " + err.Error()))
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
	stateParam := queryParts.Get("state")

	// For some connectors, the redirectURI must be static. In that case we
	// expect the callback URL to be the connector type.
	if connectors.IsConnectorType(connectorID) {
		// If any state is provided it must match the connector ID
		connectorID = stateParam
	} else {
		if stateParam != "" && stateParam != connectorID {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("State does not match connector ID"))
			return
		}
	}

	conn := a.Syncer.GetConnector(connectorID)
	if conn == nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Unknown connector ID"))
		return
	}
	err := conn.AuthCallback(r.Context(), code)
	if err != nil {
		log.Printf("Failed to complete auth callback: %s\n", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Failed to complete auth callback : " + err.Error()))
		return
	}

	state, err := conn.Status(a.Context)
	if err != nil {
		log.Printf("Failed to get connector state: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Failed to get connector state: " + err.Error()))
		return
	}
	state.AuthValid = true // TODO: delegate this logic to the connector implementation
	err = conn.UpdateConnectorState(a.Context, state)
	if err != nil {
		log.Printf("Failed to update connector state: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Failed to update connector state: " + err.Error()))
		return
	}

	// Trigger a background sync, it should silently quit if a sync is already
	// running for this connector
	a.Syncer.ASyncNow(a.Context)

	// TODO: Render a proper done page
	w.Write([]byte("Application authentication is complete, you may close this tab and return to the Verbis desktop app"))
}

func (a *API) forceSync(w http.ResponseWriter, r *http.Request) {
	err := a.Syncer.SyncNow(a.Context)
	if err != nil {
		log.Printf("Failed to sync: %s", err)
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
	url := fmt.Sprintf("http://%s/api/pull", OllamaHost)

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
	Prompt string `json:"prompt"`
}

type StreamResponseHeader struct {
	Sources []map[string]string `json:"sources"` // Only returned on the first response
}

func (a *API) handlePrompt(w http.ResponseWriter, r *http.Request) {
	log.Printf("Start of handlePrompt")
	startTime := time.Now()

	vars := mux.Vars(r)
	conversationID, ok := vars["conversation_id"]
	if !ok {
		http.Error(w, "No conversation ID provided", http.StatusBadRequest)
		return
	}

	var promptReq PromptRequest
	defer r.Body.Close()
	err := json.NewDecoder(r.Body).Decode(&promptReq)
	if err != nil {
		// return HTTP 400 bad request
		http.Error(w, "Failed to decode request", http.StatusBadRequest)
	}

	conversation, err := a.store.GetConversation(r.Context(), conversationID)
	if err != nil {
		log.Printf("Failed to get conversation: %s", err)
		http.Error(w, "Failed to get conversation: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	// Call Ollama embeddings model to get embeddings for the prompt
	resp, err := EmbedFromModel(promptReq.Prompt)
	if err != nil {
		log.Printf("Failed to get embeddings: %s", err)
		http.Error(w, "Failed to get embeddings "+err.Error(), http.StatusInternalServerError)
		return
	}
	embedTime := time.Now()

	embeddings := resp.Embedding
	log.Printf("Performing vector search")

	// Perform vector similarity search and get list of most relevant results
	searchResults, err := a.store.HybridSearch(
		r.Context(),
		promptReq.Prompt,
		embeddings,
	)
	if err != nil {
		http.Error(w, "Failed to search for vectors", http.StatusInternalServerError)
		return
	}
	searchTime := time.Now()

	// Add all previous conversation chunks for reranking
	for _, chunkHash := range conversation.ChunkHashes {
		chunk, err := a.store.GetChunkByHash(r.Context(), chunkHash)
		if err != nil {
			log.Printf("Failed to get chunk by hash: %s", err)
			http.Error(w, "Failed to get chunk by hash", http.StatusInternalServerError)
			return
		}
		searchResults = append(searchResults, chunk)
	}

	hashes := map[string]bool{}
	for _, chunk := range searchResults {
		if chunk.Hash == "" {
			log.Printf("Pre-rerank Chunk has no hash")
		}
		_, ok := hashes[chunk.Hash]
		if ok {
			log.Printf("Pre-rerank duplicate hash " + chunk.Hash)
		}

		hashes[chunk.Hash] = true
	}

	// Rerank the results
	rerankedChunks, err := Rerank(r.Context(), searchResults, promptReq.Prompt)
	if err != nil {
		log.Printf("Failed to rerank search results: %s", err)
		http.Error(w, "Failed to rerank search results", http.StatusInternalServerError)
		return
	}
	rerankTime := time.Now()

	hashes = map[string]bool{}
	for _, chunk := range rerankedChunks {
		if chunk.Hash == "" {
			log.Printf("Post-rerank Chunk has no hash")
		}
		_, ok := hashes[chunk.Hash]
		if ok {
			log.Printf("Post-rerank duplicate hash " + chunk.Hash)
		}

		hashes[chunk.Hash] = true
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
	err = chatWithModelStream(r.Context(), llmPrompt, generationModelName, conversation.History, streamChan)
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

	sourcesObj := sourcesFromChunks(rerankedChunks)
	sourcesObjJSON, marshalSourcesErr := json.Marshal(sourcesObj)
	if marshalSourcesErr != nil {
		log.Printf("Failed to marshal sources: %s", marshalSourcesErr)
		http.Error(w, "Failed to marshal sources", http.StatusInternalServerError)
		return
	}
	var sources []map[string]string
	unmarshalSourcesErr := json.Unmarshal(sourcesObjJSON, &sources)
	if unmarshalSourcesErr != nil {
		log.Printf("Failed to unmarshal sources: %s", unmarshalSourcesErr)
		http.Error(w, "Failed to unmarshal sources", http.StatusInternalServerError)
		return
	}

	// First write the header response
	err = json.NewEncoder(w).Encode(StreamResponseHeader{
		Sources: sources,
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

	// Find out which chunks are not already part of the conversation history
	newChunks := []*types.Chunk{}
	for _, chunk := range rerankedChunks {
		found := false
		for _, chunkHash := range conversation.ChunkHashes {
			if chunkHash == chunk.Hash {
				found = true
				break
			}
		}

		if !found {
			newChunks = append(newChunks, chunk)
		}
	}

	err = a.store.ConversationAppend(r.Context(), conversationID, []types.HistoryItem{
		{
			Role:    "user",
			Content: promptReq.Prompt,
		},
		{
			Role:    "assistant",
			Content: responseAcc,
			Sources: sourcesObj,
		},
	}, newChunks)
	if err != nil {
		log.Printf("Failed to append to conversation: %s", err)
		http.Error(w, "Failed to append to conversation", http.StatusInternalServerError)
		return
	}

	if a.Posthog == nil {
		return
	}

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
			Set("num_streamed_events", streamCount).
			Set("version", a.Version),
	})
	if err != nil {
		log.Printf("Failed to enqueue event: %s\n", err)
		http.Error(w, "Failed to enqueue event", http.StatusInternalServerError)
		return
	}
	log.Printf("End of handlePrompt")
}
