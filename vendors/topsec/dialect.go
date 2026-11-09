// Package topsec implements the Topsec network device dialect.
package topsec

import (
	"time"

	"github.com/gatework/go-net-connector/vendors/internal/driver"
)

// Dialect is the built-in Topsec platform.
type Dialect struct {
	driver.Base
	driver.ReadOnlyCommands
}

// New returns the built-in Topsec dialect.
func New() Dialect {
	return Dialect{Base: newBase(), ReadOnlyCommands: readOnly}
}

func newBase() driver.Base {
	p := driver.BaseTerminal("topsec")
	p.ConfigCommands = []string{"show-running nostop"}
	p.LoginPrompt = driver.Pattern(`(?:\A|[\r\n])[^\s#>]*#\s*\z`)
	p.CommandPrompt = driver.Pattern(`(?:\A|[\r\n])[^\s#>]*#\s*\z`)
	p.CommandErrors = driver.Regexps(`(?im)^[ \t]*(?:%[ \t]*)?(?:Unknown|Invalid|Incomplete|Ambiguous)\s+command\b`, `(?im)^[ \t]*(?:Error:|Permission denied\b)`)
	return driver.NewBase(p, 120*time.Second, driver.BaseConfigFactory(), nil)
}

// readOnly lists the commands this family accepts for read-only collection.
var readOnly = driver.ReadOnlyCommands{Verbs: []string{"show", "show-running"}}
