package connectors

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"

	"github.com/verbis-ai/verbis/verbis/keychain"
	"github.com/verbis-ai/verbis/verbis/store"
	"github.com/verbis-ai/verbis/verbis/types"
	"github.com/verbis-ai/verbis/verbis/util"
)

func NewGmailConnector(creds types.BuildCredentials) types.Connector {
	return &GmailConnector{
		id:   "",
		user: "",
	}
}

type GmailConnector struct {
	id   string
	user string
}

func (g *GmailConnector) ID() string {
	return g.id
}

func (g *GmailConnector) User() string {
	return g.user
}

func (g *GmailConnector) Type() types.ConnectorType {
	return types.ConnectorTypeGmail
}

func (g *GmailConnector) Status(ctx context.Context) (*types.ConnectorState, error) {
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

func (g *GmailConnector) getClient(ctx context.Context, config *oauth2.Config) (*http.Client, error) {
	// Token from Keychain
	tok, err := keychain.TokenFromKeychain(g.ID(), g.Type())
	if err != nil {
		return nil, err
	}
	return config.Client(ctx, tok), nil
}

func (g *GmailConnector) requestOauthWeb(config *oauth2.Config) error {
	config.RedirectURL = fmt.Sprintf("http://127.0.0.1:8081/connectors/%s/callback", g.ID())
	log.Printf("Requesting token from web with redirectURL: %v", config.RedirectURL)
	authURL := config.AuthCodeURL(g.ID(), oauth2.AccessTypeOffline)
	fmt.Printf("Your browser has been opened to visit:\n%v\n", authURL)

	// Open URL in the default browser
	return exec.Command("open", authURL).Start()
}

var gmailScopes []string = []string{
	gmail.GmailReadonlyScope,
	"https://www.googleapis.com/auth/userinfo.email",
}

func (g *GmailConnector) Init(ctx context.Context, connectorID string) error {
	if connectorID != "" {
		// connectorID is passed only when Init is called to re-create the
		// connector from a state object during initial load
		g.id = connectorID
	}
	if g.id == "" {
		g.id = uuid.New().String()
	}

	state, err := store.GetConnectorState(ctx, store.GetWeaviateClient(), g.ID())
	if err != nil && !store.IsStateNotFound(err) {
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

	err = store.UpdateConnectorState(ctx, store.GetWeaviateClient(), state)
	if err != nil {
		return fmt.Errorf("failed to set connector state: %v", err)
	}
	return nil
}

func (g *GmailConnector) UpdateConnectorState(ctx context.Context, state *types.ConnectorState) error {
	return store.UpdateConnectorState(ctx, store.GetWeaviateClient(), state)
}

func (g *GmailConnector) AuthSetup(ctx context.Context) error {
	config, err := gmailConfigFromJSON()
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

func gmailConfigFromJSON() (*oauth2.Config, error) {
	path, err := util.GetDistPath()
	if err != nil {
		return nil, fmt.Errorf("failed to get dist path: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(path, googleCredentialFile))
	if err != nil {
		return nil, fmt.Errorf("unable to read client secret file: %v", err)
	}
	return google.ConfigFromJSON(b, gmailScopes...)
}

// TODO: handle token expiries
func (g *GmailConnector) AuthCallback(ctx context.Context, authCode string) error {
	config, err := gmailConfigFromJSON()
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

func (g *GmailConnector) Sync(ctx context.Context, lastSync time.Time, chunkChan chan types.ChunkSyncResult, errChan chan error) {
	defer close(chunkChan)

	log.Printf("Starting gmail sync")
	config, err := gmailConfigFromJSON()
	if err != nil {
		errChan <- fmt.Errorf("unable to get google config: %s", err)
		return
	}

	client, err := g.getClient(ctx, config)
	if err != nil {
		errChan <- fmt.Errorf("unable to get client: %v", err)
		return
	}

	srv, err := gmail.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		errChan <- fmt.Errorf("unable to retrieve Gmail client: %v", err)
		return
	}

	err = g.listEmails(ctx, srv, lastSync, chunkChan)
	if err != nil {
		errChan <- fmt.Errorf("unable to list emails: %v", err)
		return
	}
}

func (g *GmailConnector) processEmail(ctx context.Context, srv *gmail.Service, email *gmail.Message, chunkChan chan types.ChunkSyncResult) error {
	var content string
	for _, part := range email.Payload.Parts {
		if part.MimeType == "text/plain" {
			data, err := decodeBase64(part.Body.Data)
			if err != nil {
				chunkChan <- types.ChunkSyncResult{
					Err: fmt.Errorf("unable to decode email body: %s", err),
				}
				continue
			}
			content += data
		}
		// Process attachments
		if part.Filename != "" && part.MimeType == "application/pdf" {
			data, err := downloadAttachment(ctx, srv, g.user, email.Id, part.Body.AttachmentId)
			if err != nil {
				chunkChan <- types.ChunkSyncResult{
					Err: fmt.Errorf("unable to download attachment for file %s: %s", part.Filename, err),
				}
				continue
			}
			content += data
		}
	}

	content = cleanText(content)

	receivedAt := time.Unix(email.InternalDate/1000, 0)
	emailURL := fmt.Sprintf("https://mail.google.com/mail/u/0/#inbox/%s", email.Id)

	document := types.Document{
		UniqueID:      email.Id,
		Name:          getEmailSubject(email.Payload.Headers),
		SourceURL:     emailURL, // Include the URL here
		ConnectorID:   g.ID(),
		ConnectorType: string(g.Type()),
		CreatedAt:     receivedAt,
		UpdatedAt:     receivedAt,
	}

	err := store.DeleteDocumentChunks(ctx, store.GetWeaviateClient(), document.UniqueID, g.ID())
	if err != nil {
		log.Printf("Unable to delete chunks for document %s: %v", document.UniqueID, err)
	}

	for i := 0; i < len(content); i += MaxChunkSize {
		end := i + MaxChunkSize
		if end > len(content) {
			end = len(content)
		}

		chunkChan <- types.ChunkSyncResult{
			Chunk: types.Chunk{
				Text:     content[i:end],
				Document: document,
			},
		}
	}
	return nil
}

func (g *GmailConnector) listEmails(ctx context.Context, srv *gmail.Service, lastSync time.Time, chunkChan chan types.ChunkSyncResult) error {
	user := "me"
	query := "in:inbox -category:spam"
	if !lastSync.IsZero() {
		query += fmt.Sprintf(" after:%d", lastSync.Unix())
	}

	req := srv.Users.Messages.List(user).Q(query).MaxResults(10)
	err := req.Pages(ctx, func(page *gmail.ListMessagesResponse) error {
		var wg sync.WaitGroup
		for _, m := range page.Messages {
			log.Printf("Processing message %s", m.Id)
			wg.Add(1)
			go func(messageID string) {
				defer wg.Done()
				email, err := srv.Users.Messages.Get(user, messageID).Format("full").Do()
				if err != nil {
					log.Printf("Unable to retrieve message %s: %v", messageID, err)
					return
				}
				err = g.processEmail(ctx, srv, email, chunkChan)
				if err != nil {
					log.Printf("Error processing email %s: %v", messageID, err)
				}
			}(m.Id)
		}
		wg.Wait()
		return nil
	})

	if err != nil {
		return fmt.Errorf("unable to retrieve emails: %v", err)
	}
	return nil
}

func getEmailSubject(headers []*gmail.MessagePartHeader) string {
	for _, h := range headers {
		if h.Name == "Subject" {
			return h.Value
		}
	}
	return "(no subject)"
}

func decodeBase64(encoded string) (string, error) {
	decoded, err := base64.URLEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

func downloadAttachment(ctx context.Context, srv *gmail.Service, userID, messageID, attachmentID string) (string, error) {
	att, err := srv.Users.Messages.Attachments.Get(userID, messageID, attachmentID).Context(ctx).Do()
	if err != nil {
		return "", err
	}
	data, err := base64.URLEncoding.DecodeString(att.Data)
	if err != nil {
		return "", err
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %v", err)
	}

	tempDir := filepath.Join(homeDir, ".verbis", "tmp")
	err = os.MkdirAll(tempDir, os.ModePerm)
	if err != nil {
		return "", fmt.Errorf("failed to create temporary directory: %v", err)
	}

	const maxFileNameLength = 255
	fileName := attachmentID
	if len(fileName) > maxFileNameLength {
		fileName = fileName[:maxFileNameLength]
	}

	tempFilePath := filepath.Join(tempDir, fileName)
	outFile, err := os.Create(tempFilePath)
	if err != nil {
		return "", fmt.Errorf("failed to create temporary file: %v", err)
	}
	defer outFile.Close()

	_, err = outFile.Write(data)
	if err != nil {
		return "", fmt.Errorf("failed to write file to disk: %v", err)
	}

	return tempFilePath, nil
}

func cleanText(input string) string {
	// Remove URLs
	urlRegex := regexp.MustCompile(`http[s]?://[^\s]+`)
	cleaned := urlRegex.ReplaceAllString(input, "")

	// Remove non-readable text and payloads (based on patterns in your example)
	payloadRegex := regexp.MustCompile(`[a-zA-Z0-9\-_]{20,}`)
	cleaned = payloadRegex.ReplaceAllString(cleaned, "")

	// Remove extra whitespace
	cleaned = strings.TrimSpace(cleaned)
	cleaned = regexp.MustCompile(`\s+`).ReplaceAllString(cleaned, " ")

	return cleaned
}
