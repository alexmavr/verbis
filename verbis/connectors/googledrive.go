package connectors

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"

	"github.com/verbis-ai/verbis/verbis/keychain"
	"github.com/verbis-ai/verbis/verbis/store"
	"github.com/verbis-ai/verbis/verbis/types"
	"github.com/verbis-ai/verbis/verbis/util"
)

const (
	googleCredentialFile = "credentials.json"
)

func NewGoogleDriveConnector() types.Connector {
	return &GoogleDriveConnector{
		id:   "",
		user: "",
	}
}

type GoogleDriveConnector struct {
	id   string
	user string
}

func (g *GoogleDriveConnector) ID() string {
	return g.id
}

func (g *GoogleDriveConnector) User() string {
	return g.user
}

func (g *GoogleDriveConnector) Type() types.ConnectorType {
	return types.ConnectorTypeGoogleDrive
}

func (g *GoogleDriveConnector) Status(ctx context.Context) (*types.ConnectorState, error) {
	state, err := store.GetConnectorState(ctx, store.GetWeaviateClient(), g.ID())
	if err != nil {
		return nil, fmt.Errorf("failed to get connector state: %v", err)
	}

	if state == nil {
		// No stored state, only happens if sync() is called before init()
		return nil, fmt.Errorf("connector state not found")
	}
	return state, nil
}

func (g *GoogleDriveConnector) getClient(ctx context.Context, config *oauth2.Config) (*http.Client, error) {
	// Token from Keychain
	tok, err := keychain.TokenFromKeychain(g.ID(), g.Type())
	if err != nil {
		return nil, err
	}
	return config.Client(ctx, tok), nil
}

func (g *GoogleDriveConnector) requestOauthWeb(config *oauth2.Config) error {
	config.RedirectURL = fmt.Sprintf("http://127.0.0.1:8081/connectors/%s/callback", g.ID())
	log.Printf("Requesting token from web with redirectURL: %v", config.RedirectURL)
	authURL := config.AuthCodeURL(g.ID(), oauth2.AccessTypeOffline)
	fmt.Printf("Your browser has been opened to visit:\n%v\n", authURL)

	// Open URL in the default browser
	return exec.Command("open", authURL).Start()
}

var driveScopes []string = []string{
	drive.DriveMetadataReadonlyScope,
	drive.DriveReadonlyScope,
	"https://www.googleapis.com/auth/userinfo.email",
}

func (g *GoogleDriveConnector) Init(ctx context.Context, connectorID string) error {
	if connectorID != "" {
		// connectorID is passed only when Init is called to re-create the
		// connector from a state object during initial load
		g.id = connectorID
	}
	if g.id == "" {
		g.id = uuid.New().String()
	}

	log.Printf("Initializing connector type: %s id: %s", g.Type(), g.ID())
	state, err := store.GetConnectorState(ctx, store.GetWeaviateClient(), g.ID())
	if err != nil {
		return fmt.Errorf("failed to get connector state: %v", err)
	}

	if state == nil {
		state = &types.ConnectorState{}
	}

	state.ConnectorID = g.ID()
	state.Syncing = false
	// state.User is unknown until auth is complete
	state.ConnectorType = string(g.Type())
	token, err := keychain.TokenFromKeychain(g.ID(), g.Type())
	state.AuthValid = (err == nil && token != nil) // TODO: check for expiry of refresh token
	log.Printf("AuthValid: %v", state.AuthValid)

	err = store.UpdateConnectorState(ctx, store.GetWeaviateClient(), state)
	if err != nil {
		return fmt.Errorf("failed to set connector state: %v", err)
	}
	log.Printf("Initialized connector type %s: %s", g.Type(), g.ID())
	return nil
}

func (g *GoogleDriveConnector) UpdateConnectorState(ctx context.Context, state *types.ConnectorState) error {
	return store.UpdateConnectorState(ctx, store.GetWeaviateClient(), state)
}

func driveConfigFromJSON() (*oauth2.Config, error) {
	path, err := util.GetDistPath()
	if err != nil {
		return nil, fmt.Errorf("failed to get dist path: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(path, googleCredentialFile))
	if err != nil {
		return nil, fmt.Errorf("unable to read client secret file: %v", err)
	}
	return google.ConfigFromJSON(b, driveScopes...)
}

func (g *GoogleDriveConnector) AuthSetup(ctx context.Context) error {
	config, err := driveConfigFromJSON()
	if err != nil {
		return fmt.Errorf("unable to get google config: %s", err)
	}
	_, err = keychain.TokenFromKeychain(g.ID(), g.Type())
	if err == nil {
		// TODO: check for expiry of refresh token
		log.Print("Token found in keychain.")
		return nil
	}
	log.Print("No token found in keychain. Getting token from web.")
	err = g.requestOauthWeb(config)
	if err != nil {
		log.Printf("Unable to request token from web: %v", err)
	}
	return nil
}

// TODO: handle token expiries
func (g *GoogleDriveConnector) AuthCallback(ctx context.Context, authCode string) error {
	config, err := driveConfigFromJSON()
	if err != nil {
		return fmt.Errorf("unable to get google config: %s", err)
	}

	config.RedirectURL = fmt.Sprintf("http://127.0.0.1:8081/connectors/%s/callback", g.ID())
	log.Printf("Config: %v", config)
	tok, err := config.Exchange(ctx, authCode)
	if err != nil {
		return fmt.Errorf("unable to retrieve token from web: %v", err)
	}

	err = keychain.SaveTokenToKeychain(tok, g.ID(), g.Type())
	if err != nil {
		return fmt.Errorf("unable to save token to keychain: %v", err)
	}

	client := config.Client(ctx, tok)
	email, err := getUserEmail(client)
	if err != nil {
		return fmt.Errorf("unable to get user email: %v", err)
	}
	log.Printf("User email: %s", email)
	g.user = email

	state, err := g.Status(ctx)
	if err != nil {
		return fmt.Errorf("unable to get connector state: %v", err)
	}

	state.User = g.User()
	return g.UpdateConnectorState(ctx, state)
}

func getUserEmail(client *http.Client) (string, error) {
	resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo?alt=json")
	if err != nil {
		return "", fmt.Errorf("unable to get user info: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to get user info: status %s", resp.Status)
	}

	var userInfo struct {
		Email string `json:"email"`
	}

	err = json.NewDecoder(resp.Body).Decode(&userInfo)
	if err != nil {
		return "", fmt.Errorf("unable to decode user info: %v", err)
	}

	return userInfo.Email, nil
}

func (g *GoogleDriveConnector) Sync(ctx context.Context, lastSync time.Time, chunkChan chan types.Chunk, errChunkChan chan error, errChan chan error) {
	defer close(errChan)
	defer close(chunkChan)

	config, err := driveConfigFromJSON()
	if err != nil {
		errChan <- fmt.Errorf("unable to get google config: %s", err)
		return
	}

	client, err := g.getClient(ctx, config)
	if err != nil {
		errChan <- fmt.Errorf("unable to get client: %v", err)
		return
	}

	srv, err := drive.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		errChan <- fmt.Errorf("unable to retrieve Drive client: %v", err)
		return
	}

	err = g.listFiles(ctx, srv, lastSync, chunkChan, errChunkChan)
	if err != nil {
		errChan <- fmt.Errorf("unable to list files: %v", err)
		return
	}
}

func (g *GoogleDriveConnector) processFile(ctx context.Context, service *drive.Service, file *drive.File, chunkChan chan types.Chunk, errChunkChan chan error) {
	var content string
	var err error
	if file.MimeType == "application/vnd.google-apps.document" {
		content, err = exportFile(service, file.Id, "text/plain")
	} else if file.MimeType == "application/vnd.google-apps.spreadsheet" {
		content, err = exportFile(service, file.Id, "application/csv")
	} else {
		content, err = downloadAndParseBinaryFile(ctx, service, file)
		if err != nil {
			errChunkChan <- fmt.Errorf("unable to process binary file %s: %v", file.Name, err)
			return
		}
	}
	if err != nil {
		errChunkChan <- fmt.Errorf("unable to export file: %v", err)
		return
	}

	log.Printf("Document: %s, %s, %s", file.Name, file.CreatedTime, file.ModifiedTime)
	createdAt, err := time.Parse(time.RFC3339, file.CreatedTime)
	if err != nil {
		log.Printf("Error parsing created time %s: %v", file.CreatedTime, err)
		createdAt = time.Now()
	}

	updatedAt, err := time.Parse(time.RFC3339, file.ModifiedTime)
	if err != nil {
		log.Printf("Error parsing modified time %s: %v", file.ModifiedTime, err)
		updatedAt = time.Now()
	}

	numChunks := 0
	document := types.Document{
		UniqueID:    file.Id,
		Name:        file.Name,
		SourceURL:   file.WebViewLink,
		ConnectorID: g.ID(),
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}

	// TODO: ideally this should live at the top level but we need to refactor the syncer first
	err = store.DeleteDocumentChunks(ctx, store.GetWeaviateClient(), document.UniqueID, g.ID())
	if err != nil {
		// Not a fatal error, just log it and leave the old chunks behind
		log.Printf("Unable to delete chunks for document %s: %v", document.UniqueID, err)
	}

	// Split contents into chunks of MaxChunkSize characters
	for i := 0; i < len(content); i += MaxChunkSize {
		end := i + MaxChunkSize
		if end > len(content) {
			end = len(content)
		}

		// TODO: add chunk overlaps
		chunk := types.Chunk{
			Text:     content[i:end],
			Document: document,
		}
		numChunks += 1
		log.Printf("Processing chunk %d of document %s", numChunks, file.Name)
		chunkChan <- chunk
	}
}

func (g *GoogleDriveConnector) listFiles(ctx context.Context, service *drive.Service, lastSync time.Time, chunkChan chan types.Chunk, errChunkChan chan error) error {
	pageToken := ""
	for {
		q := service.Files.List().
			PageSize(10).
			Fields("nextPageToken, files(id, name, webViewLink, createdTime, modifiedTime, mimeType)").
			OrderBy("modifiedTime desc").Context(ctx)
		if !lastSync.IsZero() {
			q = q.Q("modifiedTime > '" + lastSync.Format(time.RFC3339) + "'")
		}
		if pageToken != "" {
			q = q.PageToken(pageToken)
		}
		r, err := q.Do()
		if err != nil {
			return fmt.Errorf("unable to retrieve files: %v", err)
		}

		// Max parallelism is number of files per page (10)
		wg := sync.WaitGroup{}
		for _, file := range r.Files {
			wg.Add(1)
			go func(f *drive.File) {
				defer wg.Done()
				g.processFile(ctx, service, f, chunkChan, errChunkChan)
			}(file)
		}
		wg.Wait()

		pageToken = r.NextPageToken
		if pageToken == "" {
			break
		}
	}
	return nil
}

func exportFile(service *drive.Service, fileId string, mimeType string) (string, error) {
	resp, err := service.Files.Export(fileId, mimeType).Download()
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func downloadFile(service *drive.Service, fileId string) (string, error) {
	resp, err := service.Files.Get(fileId).Download()
	if err != nil {
		return "", fmt.Errorf("failed to download file: %v", err)
	}
	defer resp.Body.Close()

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %v", err)
	}

	tempDir := filepath.Join(homeDir, ".verbis", "tmp")
	err = os.MkdirAll(tempDir, os.ModePerm)
	if err != nil {
		return "", fmt.Errorf("failed to create temporary directory: %v", err)
	}

	tempFilePath := filepath.Join(tempDir, fileId)
	outFile, err := os.Create(tempFilePath)
	if err != nil {
		return "", fmt.Errorf("failed to create temporary file: %v", err)
	}
	defer outFile.Close()

	_, err = io.Copy(outFile, resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to write file to disk: %v", err)
	}

	return tempFilePath, nil
}

func downloadAndParseBinaryFile(ctx context.Context, service *drive.Service, file *drive.File) (string, error) {
	_, ok := SupportedMimeTypes[file.MimeType]
	if !ok {
		log.Printf("Unsupported MIME type: %s", file.MimeType)
		return "", nil
	}
	log.Printf("Processing binary file: %s", file.Name)

	tempFilePath, err := downloadFile(service, file.Id)
	if err != nil {
		return "", fmt.Errorf("failed to download file: %v", err)
	}
	log.Printf("Finished downloading binary file: %s", file.Name)

	request := &ParseRequest{
		Type: file.MimeType,
		Path: tempFilePath,
	}
	content, err1 := ParseBinaryFile(ctx, request)
	err2 := os.Remove(tempFilePath) // Delete the file after processing
	log.Printf("Finished parsing binary file %s", file.Name)

	if err1 != nil {
		return "", fmt.Errorf("failed to parse binary file: %v", err)
	}
	if err2 != nil {
		log.Printf("Error deleting file %s: %v", tempFilePath, err)
	}

	return content, nil
}
