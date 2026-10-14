package httpapi

import (
	"slices"
)

// capability is one integration feature a partner checks for by name at /meta,
// with the operations and topics it promises; docs/integration.md lists them.
type capability struct {
	name, promise string
	// operations are "METHOD /path" as the operations table spells them.
	operations []string
	topics     []string
}

// integrationCapabilities may only use names from Dynamo's shared list.
var integrationCapabilities = []capability{}

// capabilities is what this build announces beyond its API version; a client
// checks for a name here rather than comparing versions.
func capabilities() []string {
	names := make([]string, 0, len(integrationCapabilities))
	for _, c := range integrationCapabilities {
		names = append(names, c.name)
	}
	slices.Sort(names)
	return names
}
