package connectors

import (
	"github.com/epochlabs-ai/lamoid/lamoid/types"
)

var AllConnectors = map[string]types.Connector{
	"google": &GoogleConnector{},
}
