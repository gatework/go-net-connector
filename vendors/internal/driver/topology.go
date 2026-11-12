package driver

import (
	"github.com/gatework/go-net-connector/internal/ifname"
	"github.com/gatework/go-net-connector/vendors/dialect"
	"github.com/gatework/go-net-connector/vendors/internal/topology"
)

// NeighborPolicy owns one platform's neighbor command selection and parsing.
type NeighborPolicy struct {
	StatusCommand         string
	CommandText           string
	Parser                topology.Parser
	SelectNeighborCommand func(status, prompt string) (string, error)
}

// NewNeighborPolicy returns a policy with a fixed neighbor command.
func NewNeighborPolicy(command string, parser topology.Parser) NeighborPolicy {
	return NeighborPolicy{CommandText: command, Parser: parser}
}

// NeighborStatusCommand returns the command whose output selects the neighbor
// command, or "".
func (p NeighborPolicy) NeighborStatusCommand() string { return p.StatusCommand }

// NeighborCommand returns the neighbor command for the status output and
// prompt.
func (p NeighborPolicy) NeighborCommand(status, prompt string) (string, error) {
	if p.SelectNeighborCommand != nil {
		return p.SelectNeighborCommand(status, prompt)
	}
	if p.CommandText == "" {
		return "", dialect.ErrUnsupported
	}
	return p.CommandText, nil
}

// ParseNeighbors parses neighbor output.
func (p NeighborPolicy) ParseNeighbors(output string) ([]dialect.Neighbor, error) {
	return p.Parser.ParseNeighbors(output)
}

// TopologyReader groups the read-only topology capabilities that share one
// parser: neighbor discovery, description parsing and interface identity.
type TopologyReader struct {
	NeighborPolicy
	DescriptionReader
	InterfaceIdentityPolicy
}

// NewTopologyReader declares the read-only topology capabilities for a
// platform whose neighbor command does not depend on device status.
func NewTopologyReader(neighborCommand string, parser topology.Parser) TopologyReader {
	return TopologyReader{
		NeighborPolicy:          NewNeighborPolicy(neighborCommand, parser),
		DescriptionReader:       DescriptionReader{Parser: parser},
		InterfaceIdentityPolicy: InterfaceIdentityPolicy{Parser: parser},
	}
}

// DescriptionReader parses a platform's running configuration.
type DescriptionReader struct{ Parser topology.Parser }

// ParseDescriptions parses interface descriptions from config.
func (p DescriptionReader) ParseDescriptions(config string) (map[string]string, error) {
	return p.Parser.ParseDescriptions(config)
}

// InterfaceIdentityPolicy maps discovered names to the keys used by config
// parsers and device configuration commands.
type InterfaceIdentityPolicy struct{ Parser topology.Parser }

// InterfaceKey returns the parser's canonical key for name.
func (p InterfaceIdentityPolicy) InterfaceKey(name string) string {
	if p.Parser.CanonicalInterface == nil {
		return ifname.Key(name)
	}
	return p.Parser.CanonicalInterface(name)
}

// ConfigName returns the name used in configuration commands.
func (InterfaceIdentityPolicy) ConfigName(name string) string {
	return ifname.ConfigName(name)
}

// DescriptionWriter implements command grammar shared by the
// currently writable families while retaining each family's mode transitions.
type DescriptionWriter struct{ Mode dialect.WriteMode }

// NewDescriptionWriter returns a writer using mode's transitions.
func NewDescriptionWriter(mode dialect.WriteMode) DescriptionWriter {
	return DescriptionWriter{Mode: mode}
}

// WriteMode returns the mode transitions.
func (p DescriptionWriter) WriteMode() dialect.WriteMode { return p.Mode }

// ChangeCommands returns the commands that set one interface description.
func (DescriptionWriter) ChangeCommands(interfaceName, description string) ([]string, error) {
	return []string{"interface " + interfaceName, "description " + description}, nil
}

// WriteRestriction gives a read-only platform a stable explanation
// without making it implement a write capability.
type WriteRestriction struct{ Code string }

// UnavailableCode returns why writing is unavailable.
func (p WriteRestriction) UnavailableCode() string { return p.Code }
