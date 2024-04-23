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
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	//   "google.golang.org/api/calendar/v3"
	//    "google.golang.org/api/gmail/v1"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"

	"github.com/zalando/go-keyring"
)

const (
	credentialPath = "../Resources/credentials.json"
	tokenKey       = "user-google-oauth-token"
	keyringService = "LamoidApp"
	MaxChunkSize   = 10000 // Maximum number of characters in a chunk
)

type GoogleConnector struct {
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
	config.RedirectURL = "http://127.0.0.1:8081/google/callback"
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

func (g *GoogleConnector) Init(ctx context.Context) error {
	b, err := os.ReadFile(credentialPath)
	if err != nil {
		return fmt.Errorf("unable to read client secret file: %v", err)
	}

	config, err := google.ConfigFromJSON(b,
		scopes...,
	)
	if err != nil {
		return fmt.Errorf("unable to parse client secret file to config: %v", err)
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

func (g *GoogleConnector) AuthCallback(ctx context.Context, authCode string) error {
	b, err := os.ReadFile(credentialPath)
	if err != nil {
		return fmt.Errorf("unable to read client secret file: %v", err)
	}
	config, err := google.ConfigFromJSON(b, scopes...)
	if err != nil {
		return fmt.Errorf("unable to parse client secret file to config: %v", err)
	}

	log.Printf("Auth code: %s", authCode)

	config.RedirectURL = "http://127.0.0.1:8081/google/callback"
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

func (g *GoogleConnector) Sync(ctx context.Context) ([]Chunk, error) {
	res := []Chunk{}
	b, err := os.ReadFile(credentialPath)
	if err != nil {
		return res, fmt.Errorf("unable to read client secret file: %v", err)
	}
	config, err := google.ConfigFromJSON(b, scopes...)
	if err != nil {
		return res, fmt.Errorf("unable to parse client secret file to config: %v", err)
	}

	client, err := getClient(ctx, config)
	if err != nil {
		return res, fmt.Errorf("unable to get client: %v", err)
	}

	srv, err := drive.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return res, fmt.Errorf("unable to retrieve Drive client: %v", err)
	}

	return listFiles(srv)
}

var lastSyncTime time.Time = time.UnixMicro(0)

func listFiles(service *drive.Service) ([]Chunk, error) {
	var chunks []Chunk
	r, err := service.Files.List().
		PageSize(10).
		Fields("nextPageToken, files(id, name, webViewLink, createdTime, modifiedTime, mimeType)").
		Q("modifiedTime > '" + lastSyncTime.Format(time.RFC3339) + "'").
		Do()
	if err != nil {
		return chunks, fmt.Errorf("unable to retrieve files: %v", err)
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

		log.Printf("File: %s, %s, %s", file.Name, file.CreatedTime, file.ModifiedTime)
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

		// Split contents into chunks of MaxChunkSize characters
		for i := 0; i < len(content); i += MaxChunkSize {
			end := i + 1000
			if end > len(content) {
				end = len(content)
			}
			chunk := Chunk{
				Text: content[i:end],
				Document: Document{
					SourceURL:  file.WebViewLink,
					SourceName: "Google Drive",
					CreatedAt:  createdAt,
					UpdatedAt:  updatedAt,
				},
			}
			chunks = append(chunks, chunk)
		}
	}
	lastSyncTime = time.Now() // Update last sync time after retrieving files
	return chunks, nil
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
