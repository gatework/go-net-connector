// Package juniper implements the Juniper Junos network device dialect.
package juniper

import (
	"time"

	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/vendors/internal/driver"
)

// Dialect is the built-in Juniper Junos platform. Description writes stay
// unavailable because the platform lacks an isolated candidate.
type Dialect struct {
	driver.ProfiledPlatform
	driver.TopologyReader
	driver.WriteRestriction
	remoteBackupPolicy
}

// New returns the built-in Juniper Junos dialect.
func New() Dialect {
	return Dialect{
		ProfiledPlatform:   driver.NewProfiledPlatform(newProfile()),
		TopologyReader:     driver.NewTopologyReader("show lldp neighbors detail | no-more", topologyParser()),
		WriteRestriction:   driver.WriteRestriction{Code: "candidate_isolation_unavailable"},
		remoteBackupPolicy: remoteBackupPolicy{},
	}
}

func newProfile() connector.Profile {
	p := driver.BaseProfile("juniper")
	p.ConfigCommands = []string{"show configuration | display set | no-more"}
	p.LoginPrompt = driver.Pattern(`(?:\A|[\r\n])[^\s@]+@[^\s>#]+>[ \t]*\z`)
	p.CommandPrompt = p.LoginPrompt
	p.Pager = driver.Pattern(`(?i)(?:\A|[\r\n])[ \t]*---\(more(?: \d+%)?\)---[ \t]*\z`)
	p.CommandErrors = driver.Regexps(`(?im)^[ \t]*(?:error:|syntax error[,.]|unknown command\b|permission denied\b|(?:ftp|scp|ssh):\s)`)
	return driver.FinishProfile(p, 120*time.Second, driver.BaseConfigFactory(), nil)
}
