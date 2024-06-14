package connectors

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/slack-go/slack"
	"golang.org/x/oauth2"
	oauthslack "golang.org/x/oauth2/slack"

	"github.com/verbis-ai/verbis/verbis/keychain"
	"github.com/verbis-ai/verbis/verbis/store"
	"github.com/verbis-ai/verbis/verbis/types"
	"github.com/verbis-ai/verbis/verbis/util"
)

const (
	slackRateLimitBackoff = 11 * time.Second
)

func NewSlackConnector(creds types.BuildCredentials) types.Connector {
	return &SlackConnector{
		id:           "",
		user:         "",
		clientID:     creds.SlackClientID,
		clientSecret: creds.SlackClientSecret,
	}
}

type SlackConnector struct {
	id           string
	user         string
	clientID     string
	clientSecret string

	messageBuffer string
}

func (s *SlackConnector) ID() string {
	return s.id
}

func (s *SlackConnector) User() string {
	return s.user
}

func (s *SlackConnector) Type() types.ConnectorType {
	return types.ConnectorTypeSlack
}

func (s *SlackConnector) Status(ctx context.Context) (*types.ConnectorState, error) {
	state, err := store.GetConnectorState(ctx, store.GetWeaviateClient(), s.ID())
	if err != nil {
		return nil, fmt.Errorf("failed to get connector state: %v", err)
	}

	if state == nil {
		// No stored state, only happens if sync() is called before init()
		return nil, fmt.Errorf("connector state not found")
	}
	return state, nil
}

func (s *SlackConnector) getClient() (*slack.Client, error) {
	// Token from Keychain
	tok, err := keychain.TokenFromKeychain(s.ID(), s.Type())
	if err != nil {
		return nil, err
	}

	return slack.New(tok.AccessToken), nil
}

func (g *SlackConnector) requestOauthWeb(config *oauth2.Config) error {
	log.Printf("Requesting token from web with redirectURL: %v", config.RedirectURL)
	authURL := config.AuthCodeURL(g.ID(), oauth2.AccessTypeOffline)
	fmt.Printf("Your browser has been opened to visit:\n%v\n", authURL)

	// Open URL in the default browser
	return exec.Command("open", authURL).Start()
}

var slackScopes = []string{
	"channels:history",
	"channels:read",
	"groups:history",
	"groups:read",
	"im:history",
	"im:read",
	"mpim:history",
	"mpim:read",
}

func (s *SlackConnector) slackConfig() (*oauth2.Config, error) {
	return &oauth2.Config{
		ClientID:     s.clientID,
		ClientSecret: s.clientSecret,
		RedirectURL:  fmt.Sprintf("https://localhost:8082/connectors/%s/callback", s.Type()),
		Scopes:       slackScopes,
		Endpoint:     oauthslack.Endpoint,
	}, nil
}

func (g *SlackConnector) Init(ctx context.Context, connectorID string) error {
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

func (s *SlackConnector) UpdateConnectorState(ctx context.Context, state *types.ConnectorState) error {
	return store.UpdateConnectorState(ctx, store.GetWeaviateClient(), state)
}

func (s *SlackConnector) AuthSetup(ctx context.Context) error {
	config, err := s.slackConfig()
	if err != nil {
		return fmt.Errorf("unable to get slack config: %v", err)
	}

	_, err = keychain.TokenFromKeychain(s.ID(), s.Type())
	if err == nil {
		// TODO: check for expiry of refresh token
		log.Print("Token found in keychain.")
		return nil
	}
	log.Print("No token found in keychain. Getting token from web.")
	err = s.requestOauthWeb(config)
	if err != nil {
		log.Printf("Unable to request token from web: %v", err)
	}
	return nil
}

func (s *SlackConnector) getUserString(client *slack.Client) (string, error) {
	resp, err := client.AuthTest()
	if err != nil {
		return "", fmt.Errorf("unable to get user identity: %v", err)
	}

	return fmt.Sprintf("%s @ %s", resp.User, resp.Team), nil
}

// TODO: handle token expiries
func (s *SlackConnector) AuthCallback(ctx context.Context, authCode string) error {
	config, err := s.slackConfig()
	if err != nil {
		return fmt.Errorf("unable to get slack config: %v", err)
	}

	tok, err := config.Exchange(ctx, authCode)
	if err != nil {
		return fmt.Errorf("unable to retrieve token from web: %v", err)
	}

	err = keychain.SaveTokenToKeychain(tok, s.ID(), s.Type())
	if err != nil {
		return fmt.Errorf("unable to save token to keychain: %v", err)
	}

	client := slack.New(tok.AccessToken)

	user, err := s.getUserString(client)
	if err != nil {
		return fmt.Errorf("unable to get user email: %v", err)
	}
	log.Printf("User string: %s", user)
	s.user = user

	state, err := s.Status(ctx)
	if err != nil {
		return fmt.Errorf("unable to get connector state: %v", err)
	}

	state.User = s.user
	return s.UpdateConnectorState(ctx, state)
}

func (s *SlackConnector) Sync(ctx context.Context, lastSync time.Time, chunkChan chan types.ChunkSyncResult, errChan chan error) {
	defer close(chunkChan)

	log.Printf("Starting slack sync")
	client, err := s.getClient()
	if err != nil {
		errChan <- fmt.Errorf("unable to get client: %v", err)
		return
	}

	err = s.fetchAllMessages(ctx, client, lastSync, chunkChan)
	if err != nil {
		errChan <- fmt.Errorf("error fetching messages: %v", err)
	}
}

func (s *SlackConnector) fetchAllMessages(ctx context.Context, client *slack.Client, lastSync time.Time, chunkChan chan types.ChunkSyncResult) error {
	log.Printf("Fetching channels")
	channels, err := s.fetchAllChannels(client)
	if err != nil {
		return err
	}

	log.Printf("Processing messages in %d channels", len(channels))
	for _, channel := range channels {
		err = s.fetchAndProcessChannelMessages(ctx, client, channel, lastSync, chunkChan)
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *SlackConnector) fetchAllChannels(client *slack.Client) ([]slack.Channel, error) {
	params := &slack.GetConversationsParameters{
		Types: []string{"public_channel", "private_channel", "im"},
		Limit: 100,
	}

	var channels []slack.Channel
	var cursor string
	for {
		params.Cursor = cursor
		ch, nextCursor, err := client.GetConversations(params)
		if err != nil {
			return nil, fmt.Errorf("error fetching channels: %v", err)
		}
		channels = append(channels, ch...)
		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}
	return channels, nil
}

func (s *SlackConnector) TimestampToTime(ts string) (time.Time, error) {
	// Looks like 1355517523.000005

	parts := strings.Split(ts, ".")
	unixMilli, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("unable to parse timestamp: %v", err)
	}

	return time.UnixMilli(unixMilli), nil
}

func IsErrSlackRateLimit(err error) bool {
	if err == nil {
		return false
	}
	if e, ok := err.(*slack.RateLimitedError); ok {
		log.Printf("Slack rate limit error: %v", e)
		return true
	}
	return false
}

func (s *SlackConnector) fetchAndProcessChannelMessages(ctx context.Context, client *slack.Client, channel slack.Channel, lastSync time.Time, chunkChan chan types.ChunkSyncResult) error {
	since := "0"
	if !lastSync.IsZero() {
		since = fmt.Sprintf("%d", lastSync.UnixMilli())
	}
	params := slack.GetConversationHistoryParameters{
		ChannelID: channel.ID,
		Limit:     100,
		Oldest:    since,
	}

	// Each channel is stored as a single document
	var doc *types.Document
	doc, err := store.GetDocument(ctx, channel.ID)
	if err != nil && !store.IsErrDocumentNotFound(err) {
		return fmt.Errorf("unable to get document: %v", err)
	}

	if doc == nil {
		if channel.ID == "" {
			return fmt.Errorf("channel ID is empty")
		}
		doc = &types.Document{
			UniqueID:      channel.ID,
			Name:          channel.ID, // TODO: store channel name instead?
			SourceURL:     "",         // Sent with the first chunk as it needs a timestamp
			ConnectorID:   s.ID(),
			ConnectorType: string(s.Type()),
			// TODO: CreatedAt
			UpdatedAt: time.Now(),
		}
	}

	for {
		log.Printf("Fetching conversation history for channel %s since %s", channel.ID, since)
		history, err := client.GetConversationHistory(&params)
		if err != nil {
			if IsErrSlackRateLimit(err) {
				time.Sleep(slackRateLimitBackoff)
				continue
			}

			return fmt.Errorf("error fetching channel history for channel %s: %v", channel.ID, err)
		}
		if !history.Ok {
			log.Printf("History error: %s", history.Error)
		}
		log.Printf("Fetched %d messages, latest: %s", len(history.Messages), history.Latest)

		// Slack messages are much shorter than a chunk, so we can batch them to maintain conversation context
		s.messageBuffer = ""
		for _, message := range history.Messages {
			err := s.processMessage(*doc, client, channel.ID, message, chunkChan)
			if err != nil {
				return err
			}
		}

		if !history.HasMore {
			break
		}
		params.Cursor = history.ResponseMetaData.NextCursor
	}
	s.flushMessageBuffer(*doc, chunkChan)
	return nil
}

func (s *SlackConnector) flushMessageBuffer(document types.Document, chunkChan chan types.ChunkSyncResult) {
	if s.messageBuffer != "" {
		chunkChan <- types.ChunkSyncResult{
			Chunk: types.Chunk{
				Text:     s.messageBuffer,
				Document: document,
			},
			SkipClean: true,
		}
		s.messageBuffer = ""
	}
}

func (s *SlackConnector) processMessage(document types.Document, client *slack.Client, channelID string, message slack.Message, chunkChan chan types.ChunkSyncResult) error {

	// In the slack connector we do not delete a previous document's chunks as
	// we are not expecting to re-index the entire document/channel.
	content := util.CleanChunk(message.Text)
	log.Printf("Processing %s message %s: %s", document.UniqueID, message.User, content)
	if len(content)+len(s.messageBuffer) <= MaxChunkSize {
		s.messageBuffer += fmt.Sprintf("%s: %s \n", message.User, content)
		return nil
	}

	// Buffer about to overflow, flush and start a new one
	link, err := client.GetPermalink(&slack.PermalinkParameters{
		Channel: channelID,
		Ts:      message.Timestamp,
	})
	if err != nil {
		return fmt.Errorf("unable to get permalink: %v", err)
	}

	document.SourceURL = link
	s.flushMessageBuffer(document, chunkChan)
	s.messageBuffer = fmt.Sprintf("%s: %s | \n", message.User, content)
	return nil
}
