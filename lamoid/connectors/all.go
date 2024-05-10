package connectors

import "github.com/epochlabs-ai/lamoid/lamoid/types"

var AllConnectors = map[string]types.Connector{
	string(types.ConnectorTypeGoogleDrive): &GoogleDriveConnector{},
}
