package connectors

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"time"

	msal "github.com/AzureAD/microsoft-authentication-library-for-go/apps/public"
	abstractions "github.com/microsoft/kiota-abstractions-go"
	msgraph "github.com/microsoftgraph/msgraph-sdk-go"
	graphcore "github.com/microsoftgraph/msgraph-sdk-go-core"
	"github.com/microsoftgraph/msgraph-sdk-go/models"
	msusers "github.com/microsoftgraph/msgraph-sdk-go/users"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/microsoft"

	"github.com/verbis-ai/verbis/verbis/keychain"
	"github.com/verbis-ai/verbis/verbis/types"
)

func NewOutlookConnector(creds types.BuildCredentials, st types.Store) types.Connector {
	return &OutlookConnector{
		BaseConnector: BaseConnector{
			connectorType: types.ConnectorTypeOutlook,
			store:         st,
		},
		secretValue: creds.AzureSecretValue,
		secretID:    creds.AzureSecretID,
	}
}

type OutlookConnector struct {
	BaseConnector
	secretValue string
	secretID    string
}

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

func (o *OutlookConnector) Sync(lastSync time.Time, chunkChan chan types.ChunkSyncResult, errChan chan error) {
	defer close(chunkChan)
	if err := o.context.Err(); err != nil {
		errChan <- fmt.Errorf("context error: %s", err)
		return
	}

	log.Printf("Starting outlook sync")
	config, err := o.outlookConfig()
	if err != nil {
		errChan <- fmt.Errorf("unable to get outlook config: %s", err)
		return
	}

	graphClient, err := o.getClient(o.context, config)
	if err != nil {
		errChan <- fmt.Errorf("unable to get client: %v", err)
		return
	}

	err = o.listEmails(o.context, graphClient, lastSync, chunkChan)
	if err != nil {
		errChan <- fmt.Errorf("unable to list emails: %v", err)
		return
	}
}

func (o *OutlookConnector) processEmail(ctx context.Context, email models.Messageable, chunkChan chan types.ChunkSyncResult) {
	content := *email.GetBody().GetContent()

	receivedAt := *email.GetReceivedDateTime()
	emailURL := fmt.Sprintf("https://outlook.live.com/mail/inbox/id/%s", *email.GetId())
	/*
			    location, err := time.LoadLocation("Local")
		    if err != nil {
		        log.Panicf("Error getting local timezone: %v", err)
		    }
	*/

	email_id := "N/A"
	email_id_ptr := email.GetId()
	if email_id_ptr != nil {
		email_id = *email_id_ptr
	}
	email_subject := "N/A"
	email_subject_ptr := email.GetSubject()
	if email_subject_ptr != nil {
		email_subject = *email_subject_ptr
	}

	document := types.Document{
		UniqueID: email_id,
		Name:     email_subject,
		// message.GetFrom().GetEmailAddress().GetName())
		// *message.GetReceivedDateTime()).In(location))
		SourceURL:     emailURL,
		ConnectorID:   o.ID(),
		ConnectorType: string(o.Type()),
		CreatedAt:     receivedAt,
		UpdatedAt:     receivedAt,
	}

	err := o.store.DeleteDocumentChunks(ctx, document.UniqueID, o.ID())
	if err != nil {
		log.Printf("Unable to delete chunks for document %s: %v", document.UniqueID, err)
	}

	log.Printf("Processing email of size %d: title: %s", len(content), document.Name)

	emitChunks(email_subject, content, document, chunkChan)
	chunkChan <- types.ChunkSyncResult{DocumentDone: document.UniqueID}
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
		ctx,
		func(message *models.Message) bool {
			// TODO: process many in parallel
			o.processEmail(ctx, message, chunkChan)
			// Return true to continue the iteration
			return true
		})
	if err != nil {
		return fmt.Errorf("unable to iterate over emails: %v", err)
	}

	return nil
}
