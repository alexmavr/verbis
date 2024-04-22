package connectors

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	//   "google.golang.org/api/calendar/v3"
	"google.golang.org/api/drive/v3"
	//    "google.golang.org/api/gmail/v1"
	"github.com/zalando/go-keyring"
)

const (
	credentialPath = "../Resources/credentials.json"
	tokenKey       = "user-google-oauth-token"
	keyringService = "LamoidApp"
)

func getClient(config *oauth2.Config) (*http.Client, error) {
	// Token from Keychain
	tok, err := tokenFromKeychain()
	if err != nil {
		return nil, err
	}
	return config.Client(context.Background(), tok), nil
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

func GoogleInitialConfig() {
	b, err := os.ReadFile(credentialPath)
	if err != nil {
		log.Fatalf("Unable to read client secret file: %v", err)
	}

	config, err := google.ConfigFromJSON(b, drive.DriveMetadataReadonlyScope)
	if err != nil {
		log.Fatalf("Unable to parse client secret file to config: %v", err)
	}
	_, err = tokenFromKeychain()
	if err == nil {
		// TODO: check for expiry of refresh token
		log.Print("Token found in keychain.")
		return
	}
	log.Print("No token found in keychain. Getting token from web.")
	err = requestOauthWeb(config)
	if err != nil {
		log.Printf("Unable to request token from web: %v", err)
	}
}

func GoogleAuthCallback(authCode string) {
	b, err := os.ReadFile(credentialPath)
	if err != nil {
		log.Fatalf("Unable to read client secret file: %v", err)
	}
	config, err := google.ConfigFromJSON(b, drive.DriveMetadataReadonlyScope)
	if err != nil {
		log.Fatalf("Unable to parse client secret file to config: %v", err)
	}

	log.Printf("Auth code: %s", authCode)

	config.RedirectURL = "http://127.0.0.1:8081/google/callback"
	log.Printf("Config: %v", config)
	tok, err := config.Exchange(oauth2.NoContext, authCode)
	if err != nil {
		log.Fatalf("Unable to retrieve token from web: %v", err)
	}

	saveTokenToKeychain(tok)

	// TODO: redirect to a "done" page - don't just render it because the
	// authorization code is shown in the browser URL
}

func GoogleSync() []string {
	b, err := os.ReadFile(credentialPath)
	if err != nil {
		log.Fatalf("Unable to read client secret file: %v", err)
	}
	config, err := google.ConfigFromJSON(b, drive.DriveMetadataReadonlyScope)
	if err != nil {
		log.Fatalf("Unable to parse client secret file to config: %v", err)
	}

	client, err := getClient(config)
	if err != nil {
		log.Fatalf("Unable to get client: %v", err)
	}

	srv, err := drive.New(client)
	if err != nil {
		log.Fatalf("Unable to retrieve Drive client: %v", err)
	}

	return listFiles(srv)
}

func listFiles(service *drive.Service) []string {
	r, err := service.Files.List().PageSize(10).Fields("nextPageToken, files(id, name)").Do()
	if err != nil {
		log.Fatalf("Unable to retrieve files: %v", err)
	}
	if len(r.Files) == 0 {
		return []string{}
	}
	fmt.Println("Files:")
	res := []string{}
	for _, i := range r.Files {
		res = append(res, fmt.Sprintf("%s (%s)\n", i.Name, i.Id))
	}
	return res
}
