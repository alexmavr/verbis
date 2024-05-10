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
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	//   "google.golang.org/api/calendar/v3"
	//    "google.golang.org/api/gmail/v1"

	"github.com/google/uuid"
	"github.com/zalando/go-keyring"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"

	"github.com/epochlabs-ai/lamoid/lamoid/store"
	"github.com/epochlabs-ai/lamoid/lamoid/types"
	"github.com/epochlabs-ai/lamoid/lamoid/util"
)

const (
	credentialFile = "credentials.json"
	tokenKey       = "user-google-oauth-token"
	keyringService = "LamoidApp"
	MaxChunkSize   = 2000 // Maximum number of characters in a chunk
)

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

func getClient(ctx context.Context, config *oauth2.Config) (*http.Client, error) {
	// Token from Keychain
	tok, err := tokenFromKeychain()
	if err != nil {
		return nil, err
	}
	return config.Client(ctx, tok), nil
}

func requestOauthWeb(config *oauth2.Config) error {
	config.RedirectURL = "http://127.0.0.1:8081/connectors/google/callback"
	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Printf("Your browser has been opened to visit:\n%v\n", authURL)

	// Open URL in the default browser
	return exec.Command("open", authURL).Start()
}

func tokenFromKeychain() (*oauth2.Token, error) {
	tokenJSON, err := keyring.Get(keyringService, tokenKey)
	if err != nil {
		return nil, err
	}
	var token oauth2.Token
	err = json.Unmarshal([]byte(tokenJSON), &token)
	return &token, err
}

func saveTokenToKeychain(token *oauth2.Token) {
	bytes, err := json.Marshal(token)
	if err != nil {
		log.Fatalf("Unable to marshal token %v", err)
	}
	err = keyring.Set(keyringService, tokenKey, string(bytes))
	if err != nil {
		log.Fatalf("Unable to save token to keychain %v", err)
	}
}

var scopes []string = []string{
	drive.DriveMetadataReadonlyScope,
	drive.DriveReadonlyScope,
}

func (g *GoogleDriveConnector) Init(ctx context.Context) error {
	if g.id == "" {
		g.id = uuid.New().String()
	}
	log.Printf("Initializing connector type: %s id: %s", g.Type(), g.ID())
	state, err := store.GetConnectorState(ctx, store.GetWeaviateClient(), g.ID())
	if err != nil {
		return fmt.Errorf("failed to get connector state: %v", err)
	}

	if state == nil {
		state = &types.ConnectorState{
			ConnectorID: g.ID(),
		}
	}

	state.Syncing = false
	state.ConnectorType = string(g.Type())
	token, err := tokenFromKeychain()
	state.AuthValid = (err == nil && token != nil) // TODO: check for expiry of refresh token
	log.Printf("token: %v, err: %v", token, err)
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

func get_config_from_json() (*oauth2.Config, error) {
	path, err := util.GetDistPath()
	if err != nil {
		return nil, fmt.Errorf("failed to get dist path: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(path, credentialFile))
	if err != nil {
		return nil, fmt.Errorf("unable to read client secret file: %v", err)
	}
	return google.ConfigFromJSON(b, scopes...)
}

func (g *GoogleDriveConnector) AuthSetup(ctx context.Context) error {
	config, err := get_config_from_json()
	if err != nil {
		return fmt.Errorf("unable to get google config: %s", err)
	}
	_, err = tokenFromKeychain()
	if err == nil {
		// TODO: check for expiry of refresh token
		log.Print("Token found in keychain.")
		return nil
	}
	log.Print("No token found in keychain. Getting token from web.")
	err = requestOauthWeb(config)
	if err != nil {
		log.Printf("Unable to request token from web: %v", err)
	}
	return nil
}

func (g *GoogleDriveConnector) AuthCallback(ctx context.Context, authCode string) error {
	config, err := get_config_from_json()
	if err != nil {
		return fmt.Errorf("unable to get google config: %s", err)
	}

	config.RedirectURL = "http://127.0.0.1:8081/connectors/google/callback"
	log.Printf("Config: %v", config)
	tok, err := config.Exchange(ctx, authCode)
	if err != nil {
		return fmt.Errorf("unable to retrieve token from web: %v", err)
	}

	saveTokenToKeychain(tok)

	// TODO: redirect to a "done" page - don't just render it because the
	// authorization code is shown in the browser URL
	return nil
}

func (g *GoogleDriveConnector) Sync(ctx context.Context, lastSync time.Time, resChan chan types.Chunk, errChan chan error) {
	defer close(errChan)
	defer close(resChan)

	config, err := get_config_from_json()
	if err != nil {
		errChan <- fmt.Errorf("unable to get google config: %s", err)
		return
	}

	client, err := getClient(ctx, config)
	if err != nil {
		errChan <- fmt.Errorf("unable to get client: %v", err)
		return
	}

	srv, err := drive.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		errChan <- fmt.Errorf("unable to retrieve Drive client: %v", err)
		return
	}

	err = listFiles(srv, lastSync, resChan)
	if err != nil {
		errChan <- fmt.Errorf("unable to list files: %v", err)
	}
}

func listFiles(service *drive.Service, lastSync time.Time, resChan chan types.Chunk) error {
	pageToken := ""
	for {
		q := service.Files.List().
			PageSize(10).
			Fields("nextPageToken, files(id, name, webViewLink, createdTime, modifiedTime, mimeType)")
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

		for _, file := range r.Files {
			var content string
			if file.MimeType == "application/vnd.google-apps.document" {
				content, err = exportFile(service, file.Id, "text/plain")
			} else if file.MimeType == "application/vnd.google-apps.spreadsheet" {
				content, err = exportFile(service, file.Id, "application/csv")
			} else {
				//content, err = downloadFile(service, file.Id)
				log.Printf("binary file found: %s with mimeType: %s", file.Name, file.MimeType)
			}
			if err != nil {
				log.Printf("Error processing file %s: %v", file.Name, err)
				continue
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
			// TODO: process documents as well

			// Split contents into chunks of MaxChunkSize characters
			for i := 0; i < len(content); i += MaxChunkSize {
				end := i + MaxChunkSize
				if end > len(content) {
					end = len(content)
				}

				chunk := types.Chunk{
					Text: content[i:end],
					Document: types.Document{
						Name:       file.Name,
						SourceURL:  file.WebViewLink,
						SourceName: "Google Drive",
						CreatedAt:  createdAt,
						UpdatedAt:  updatedAt,
					},
				}
				numChunks += 1
				log.Printf("Processing chunk %d of document %s", numChunks, file.Name)
				resChan <- chunk
			}
		}

		pageToken = r.NextPageToken
		if pageToken == "" {
			break
		}
	}
	return nil
}

// TODO: download PDFs and parse with unstructured
func downloadFile(service *drive.Service, fileId string) (string, error) {
	resp, err := service.Files.Get(fileId).Download()
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
