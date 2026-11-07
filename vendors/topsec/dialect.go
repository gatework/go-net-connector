// Package topsec implements the Topsec network device dialect.
package topsec

import (
	"time"

	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/vendors/internal/driver"
)

type dialect struct{ driver.ProfiledPlatform }

// New returns the built-in Topsec dialect.
func New() dialect {
	return dialect{ProfiledPlatform: driver.NewProfiledPlatform(newProfile())}
}

func newProfile() connector.Profile {
	p := driver.BaseProfile("topsec")
	p.ConfigCommands = []string{"show-running nostop"}
	p.LoginPrompt = driver.Pattern(`(?:\A|[\r\n])[^\s#>]*#\s*\z`)
	p.CommandPrompt = driver.Pattern(`(?:\A|[\r\n])[^\s#>]*#\s*\z`)
	p.CommandErrors = driver.Regexps(`(?im)^[ \t]*(?:%[ \t]*)?(?:Unknown|Invalid|Incomplete|Ambiguous)\s+command\b`, `(?im)^[ \t]*(?:Error:|Permission denied\b)`)
	return driver.FinishProfile(p, 120*time.Second, driver.BaseConfigFactory(), nil)
}
