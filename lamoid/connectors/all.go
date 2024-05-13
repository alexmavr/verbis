package connectors

import (
	"github.com/epochlabs-ai/lamoid/lamoid/types"
)

var AllConnectors = map[string]types.ConnectorConstructor{
	string(types.ConnectorTypeGoogleDrive): NewGoogleDriveConnector,
}
