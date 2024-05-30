package keychain

import (
	"encoding/json"
	"fmt"

	"github.com/verbis-ai/verbis/verbis/types"
	"github.com/zalando/go-keyring"
	"golang.org/x/oauth2"
)

const (
	keyringService = "VerbisAI"
)

func TokenFromKeychain(connectorID string, connectorType types.ConnectorType) (*oauth2.Token, error) {
	tokenKey := fmt.Sprintf("%s-%s-token", string(connectorType), connectorID)
	tokenJSON, err := keyring.Get(keyringService, tokenKey)
	if err != nil {
		return nil, fmt.Errorf("unable to get token from keyring: %s", err)
	}
	var token oauth2.Token
	err = json.Unmarshal([]byte(tokenJSON), &token)
	return &token, err
}

func SaveTokenToKeychain(token *oauth2.Token, connectorID string, connectorType types.ConnectorType) error {
	tokenKey := fmt.Sprintf("%s-%s-token", string(connectorType), connectorID)
	bytes, err := json.Marshal(token)
	if err != nil {
		return fmt.Errorf("unable to marshal token: %v", err)
	}
	err = keyring.Set(keyringService, tokenKey, string(bytes))
	if err != nil {
		return fmt.Errorf("unable to save token to keychain: %v", err)
	}

	return nil
}
