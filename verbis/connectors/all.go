package connectors

import (
	"github.com/verbis-ai/verbis/verbis/types"
)

var AllConnectors = map[string]types.ConnectorConstructor{
	string(types.ConnectorTypeGoogleDrive): NewGoogleDriveConnector,
	string(types.ConnectorTypeGmail):       NewGmailConnector,
	string(types.ConnectorTypeOutlook):     NewOutlookConnector,
	string(types.ConnectorTypeSlack):       NewSlackConnector,
}

const (
	// MaxChunkSize in number of characters, pre-sanitization
	// Needs to fit in the embedding context window
	MaxChunkSize = 2000
)

func IsConnectorType(s string) bool {
	_, ok := AllConnectors[s]
	return ok
}
