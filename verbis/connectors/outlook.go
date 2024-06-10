package connectors

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"time"

	msal "github.com/AzureAD/microsoft-authentication-library-for-go/apps/public"
	"github.com/google/uuid"
	abstractions "github.com/microsoft/kiota-abstractions-go"
	msgraph "github.com/microsoftgraph/msgraph-sdk-go"
	graphcore "github.com/microsoftgraph/msgraph-sdk-go-core"
	"github.com/microsoftgraph/msgraph-sdk-go/models"
	msusers "github.com/microsoftgraph/msgraph-sdk-go/users"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/microsoft"

	"github.com/verbis-ai/verbis/verbis/keychain"
	"github.com/verbis-ai/verbis/verbis/store"
	"github.com/verbis-ai/verbis/verbis/types"
)

func NewOutlookConnector(creds types.BuildCredentials) types.Connector {
	return &OutlookConnector{
		id:          "",
		user:        "",
		secretValue: creds.AzureSecretValue,
		secretID:    creds.AzureSecretID,
	}
}

type OutlookConnector struct {
	id          string
	user        string
	secretValue string
	secretID    string
}

func (o *OutlookConnector) ID() string {
	return o.id
}

func (o *OutlookConnector) User() string {
	return o.user
}

func (o *OutlookConnector) Type() types.ConnectorType {
	return types.ConnectorTypeOutlook
}

func (o *OutlookConnector) Status(ctx context.Context) (*types.ConnectorState, error) {
	state, err := store.GetConnectorState(ctx, store.GetWeaviateClient(), o.ID())
	if err != nil {
		return nil, fmt.Errorf("failed to get connector state: %v", err)
	}

	if state == nil {
		// No stored state, only happens if sync() is called before init()
		return nil, fmt.Errorf("connector state not found")
	}
	return state, nil
}

// OAuthAuthenticationProvider implements the AuthenticationProvider interface
type OAuthAuthenticationProvider struct {
	TokenSource oauth2.TokenSource
}

// AuthenticateRequest adds the Authorization header to the request
func (a *OAuthAuthenticationProvider) AuthenticateRequest(ctx context.Context, requestInfo *abstractions.RequestInformation, additionalData map[string]interface{}) error {
	token, err := a.TokenSource.Token()
	if err != nil {
		return err
	}
	requestInfo.Headers.TryAdd("Authorization", "Bearer "+token.AccessToken)
	return nil
}

func (o *OutlookConnector) getClient(ctx context.Context, config *oauth2.Config) (*msgraph.GraphServiceClient, error) {
	// Token from Keychain
	tok, err := keychain.TokenFromKeychain(o.ID(), o.Type())
	if err != nil {
		return nil, err
	}

	tokenSource := config.TokenSource(ctx, tok)
	authProvider := &OAuthAuthenticationProvider{TokenSource: tokenSource}
	adapter, err := msgraph.NewGraphRequestAdapter(authProvider)
	if err != nil {
		return nil, err
	}

	graphClient := msgraph.NewGraphServiceClient(adapter)
	return graphClient, nil
}

func (o *OutlookConnector) requestOauthWeb(config *oauth2.Config) error {
	log.Printf("Requesting token from web with redirectURL: %v", config.RedirectURL)
	authURL := config.AuthCodeURL(o.ID(), oauth2.AccessTypeOffline)
	fmt.Printf("Your browser has been opened to visit:\n%v\n", authURL)

	// Open URL in the default browser
	return exec.Command("open", authURL).Start()
}

var outlookScopes = []string{
	"https://graph.microsoft.com/Mail.Read",
	"https://graph.microsoft.com/User.Read",
	"openid",
	"email",
}

var outlookScopesPlusOffline = append(outlookScopes, "offline_access")

func (o *OutlookConnector) Init(ctx context.Context, connectorID string) error {
	if connectorID != "" {
		// connectorID is passed only when Init is called to re-create the
		// connector from a state object during initial load
		o.id = connectorID
	}
	if o.id == "" {
		o.id = uuid.New().String()
	}

	state, err := store.GetConnectorState(ctx, store.GetWeaviateClient(), o.ID())
	if err != nil && !store.IsStateNotFound(err) {
		return fmt.Errorf("failed to get connector state: %v", err)
	}

	if state == nil {
		state = &types.ConnectorState{}
	}

	state.ConnectorID = o.ID()
	state.Syncing = false
	// state.User is unknown until auth is complete
	state.ConnectorType = string(o.Type())
	token, err := keychain.TokenFromKeychain(o.ID(), o.Type())
	state.AuthValid = (err == nil && token != nil) // TODO: check for expiry of refresh token

	err = store.UpdateConnectorState(ctx, store.GetWeaviateClient(), state)
	if err != nil {
		return fmt.Errorf("failed to set connector state: %v", err)
	}
	return nil
}

func (o *OutlookConnector) UpdateConnectorState(ctx context.Context, state *types.ConnectorState) error {
	return store.UpdateConnectorState(ctx, store.GetWeaviateClient(), state)
}

func (o *OutlookConnector) AuthSetup(ctx context.Context) error {
	config, err := o.outlookConfig()
	if err != nil {
		return fmt.Errorf("unable to get outlook config: %s", err)
	}
	_, err = keychain.TokenFromKeychain(o.ID(), o.Type())
	if err == nil {
		// TODO: check for expiry of refresh token
		log.Print("Token found in keychain.")
		return nil
	}
	log.Print("No token found in keychain. Getting token from web.")
	err = o.requestOauthWeb(config)
	if err != nil {
		log.Printf("Unable to request token from web: %v", err)
	}
	return nil
}

func (o *OutlookConnector) outlookConfig() (*oauth2.Config, error) {
	return &oauth2.Config{
		ClientID:     o.secretID,
		ClientSecret: o.secretValue,
		RedirectURL:  fmt.Sprintf("http://127.0.0.1:8081/connectors/%s/callback", o.Type()),
		Scopes:       outlookScopesPlusOffline,
		Endpoint:     microsoft.AzureADEndpoint("common"),
	}, nil
}

// TODO: handle token expiries
func (o *OutlookConnector) AuthCallback(ctx context.Context, authCode string) error {
	config, err := o.outlookConfig()
	if err != nil {
		return fmt.Errorf("unable to get outlook config: %s", err)
	}

	clientApp, err := msal.New(o.secretID, msal.WithAuthority("https://login.microsoftonline.com/common"))
	if err != nil {
		return fmt.Errorf("failed to create client app: %v", err)
	}

	// MSAL automatically adds the offline_access scope
	result, err := clientApp.AcquireTokenByAuthCode(ctx, authCode, "http://127.0.0.1:8081/connectors/outlook/callback", outlookScopes)
	if err != nil {
		return fmt.Errorf("unable to retrieve token from web: %v", err)
	}

	tok := &oauth2.Token{
		AccessToken: result.AccessToken,
	}

	err = keychain.SaveTokenToKeychain(tok, o.ID(), o.Type())
	if err != nil {
		return fmt.Errorf("unable to save token to keychain: %v", err)
	}

	client, err := o.getClient(ctx, config)
	if err != nil {
		return fmt.Errorf("unable to get client: %v", err)
	}

	email, err := getOutlookUserEmail(ctx, client)
	if err != nil {
		return fmt.Errorf("unable to get user email: %v", err)
	}
	log.Printf("User email: %s", email)
	o.user = email

	state, err := o.Status(ctx)
	if err != nil {
		return fmt.Errorf("unable to get connector state: %v", err)
	}

	state.User = o.User()
	return o.UpdateConnectorState(ctx, state)
}

func getOutlookUserEmail(ctx context.Context, client *msgraph.GraphServiceClient) (string, error) {
	userable, err := client.Me().Get(ctx, nil)
	if err != nil {
		return "", err
	}

	email := userable.GetMail()
	if email == nil {
		email = userable.GetUserPrincipalName()
	}
	if email == nil {
		return "", fmt.Errorf("unable to get user email")
	}

	return *email, nil
}

func (o *OutlookConnector) Sync(ctx context.Context, lastSync time.Time, chunkChan chan types.ChunkSyncResult, errChan chan error) {
	defer close(chunkChan)

	log.Printf("Starting outlook sync")
	config, err := o.outlookConfig()
	if err != nil {
		errChan <- fmt.Errorf("unable to get outlook config: %s", err)
		return
	}

	graphClient, err := o.getClient(ctx, config)
	if err != nil {
		errChan <- fmt.Errorf("unable to get client: %v", err)
		return
	}

	err = o.listEmails(ctx, graphClient, lastSync, chunkChan)
	if err != nil {
		errChan <- fmt.Errorf("unable to list emails: %v", err)
		return
	}
}

func (o *OutlookConnector) processEmail(ctx context.Context, email models.Messageable, chunkChan chan types.ChunkSyncResult) error {
	content := *email.GetBody().GetContent()

	receivedAt := *email.GetReceivedDateTime()
	emailURL := fmt.Sprintf("https://outlook.office.com/mail/inbox/id/%s", *email.GetId())

	/*
			    location, err := time.LoadLocation("Local")
		    if err != nil {
		        log.Panicf("Error getting local timezone: %v", err)
		    }
	*/

	document := types.Document{
		UniqueID: *email.GetId(),
		Name:     *email.GetSubject(),
		// message.GetFrom().GetEmailAddress().GetName())
		// *message.GetReceivedDateTime()).In(location))
		SourceURL:     emailURL,
		ConnectorID:   o.ID(),
		ConnectorType: string(o.Type()),
		CreatedAt:     receivedAt,
		UpdatedAt:     receivedAt,
	}

	err := store.DeleteDocumentChunks(ctx, store.GetWeaviateClient(), document.UniqueID, o.ID())
	if err != nil {
		log.Printf("Unable to delete chunks for document %s: %v", document.UniqueID, err)
	}

	const MaxChunkSize = 5000
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

func (o *OutlookConnector) listEmails(ctx context.Context, client *msgraph.GraphServiceClient, lastSync time.Time, chunkChan chan types.ChunkSyncResult) error {
	headers := abstractions.NewRequestHeaders()
	headers.Add("Prefer", "outlook.body-content-type=\"text\"")

	filter := fmt.Sprintf("receivedDateTime ge %s", lastSync.Format(time.RFC3339))
	var top int32 = 10
	requestConfig := &msusers.ItemMailfoldersItemMessagesRequestBuilderGetRequestConfiguration{
		Headers: headers,
		QueryParameters: &msusers.ItemMailfoldersItemMessagesRequestBuilderGetQueryParameters{
			Select:  []string{"id", "subject", "receivedDateTime", "body", "sender"},
			Filter:  &filter,
			Top:     &top,
			Orderby: []string{"receivedDateTime DESC"},
		},
	}

	result, err := client.Me().MailFolders().ByMailFolderId("inbox").Messages().Get(ctx, requestConfig)
	if err != nil {
		return fmt.Errorf("unable to list emails: %v", err)
	}

	pageIterator, err := graphcore.NewPageIterator[*models.Message](
		result,
		client.GetAdapter(),
		models.CreateMessageCollectionResponseFromDiscriminatorValue)
	if err != nil {
		return fmt.Errorf("unable to create page iterator: %v", err)
	}
	pageIterator.SetHeaders(headers)

	err = pageIterator.Iterate(
		context.Background(),
		func(message *models.Message) bool {
			// TODO: process many in parallel
			err := o.processEmail(ctx, message, chunkChan)
			if err != nil {
				log.Printf("unable to process email: %v", err)
			}
			// Return true to continue the iteration
			return true
		})
	if err != nil {
		return fmt.Errorf("unable to iterate over emails: %v", err)
	}

	return nil
}
