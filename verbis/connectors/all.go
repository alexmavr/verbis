package connectors

import (
	"github.com/verbis-ai/verbis/verbis/types"
)

var AllConnectors = map[string]types.ConnectorConstructor{
	string(types.ConnectorTypeGoogleDrive): NewGoogleDriveConnector,
	string(types.ConnectorTypeGmail):       NewGmailConnector,
}

const (
	MaxChunkSize = 2000 // Maximum number of characters in a chunk
)
