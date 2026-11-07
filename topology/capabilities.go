package topology

import "github.com/gatework/go-net-connector/vendors/dialect"

// NeighborDiscovery is the part of a platform policy needed to collect and
// parse neighbor evidence. It is asserted by the topology workflow.
type NeighborDiscovery interface {
	NeighborStatusCommand() string
	NeighborCommand(status, prompt string) (string, error)
	ParseNeighbors(output string) ([]dialect.Neighbor, error)
}

// DescriptionReader parses the platform's running configuration.
type DescriptionReader interface {
	ParseDescriptions(config string) (map[string]string, error)
}

// InterfaceIdentity normalizes discovered interface names for matching and
// for use in configuration commands.
type InterfaceIdentity interface {
	InterfaceKey(name string) string
	ConfigName(name string) string
}

// DescriptionWriter owns vendor command syntax for one description
// change. The shared topology package still controls ordering and verification.
type DescriptionWriter interface {
	WriteMode() dialect.WriteMode
	ChangeCommands(interfaceName, description string) ([]string, error)
}

// WriteRestriction provides a stable platform-specific reason when a
// platform has read support but cannot safely use the generic write workflow.
type WriteRestriction interface {
	UnavailableCode() string
}
