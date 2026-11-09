// Package paloalto implements the Palo Alto Networks PAN-OS device dialect.
package paloalto

import (
	"time"

	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/vendors/internal/driver"
)

// Dialect is the built-in Palo Alto Networks platform. Description writes stay
// unavailable because the platform lacks an isolated candidate.
type Dialect struct {
	driver.ProfiledPlatform
	driver.TopologyReader
	driver.WriteRestriction
	tftpPolicy
}

// New returns the built-in Palo Alto Networks dialect.
func New() Dialect {
	return Dialect{
		ProfiledPlatform: driver.NewProfiledPlatform(newProfile()),
		TopologyReader:   driver.NewTopologyReader("show lldp neighbors all", topologyParser()),
		WriteRestriction: driver.WriteRestriction{Code: "candidate_isolation_unavailable"},
		tftpPolicy:       newTFTPPolicy(),
	}
}

func newProfile() connector.Profile {
	p := driver.BaseProfile("palo_alto")
	p.ConfigCommands = []string{"set cli pager off", "set cli config-output-format set", "show config diff", "configure", "show", "exit", "show config diff"}
	p.LoginPrompt = driver.Pattern(`(?:\A|[\r\n])[^\s@]+@[^\s>#]+[>#]\s*\z`)
	p.CommandPrompt = p.LoginPrompt
	p.CommandErrors = driver.Regexps(`(?im)^[ \t]*(?:Invalid syntax|Unknown command|Server error|Command failed)(?:[.:]|\s)`)
	p.LoginInteractions = []connector.Interaction{driver.LoginInteraction(`(?i)(?:acknowledge|accept)[\s\S]*(?:yes/no|\[y/n\]|enter\s+yes)[^\r\n]*\z`, "yes\n")}
	return driver.FinishProfile(p, 120*time.Second, configFactory(), nil)
}
