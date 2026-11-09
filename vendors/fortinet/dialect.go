// Package fortinet implements the Fortinet network device dialect.
package fortinet

import (
	"time"

	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/vendors/internal/driver"
)

// Dialect is the built-in Fortinet platform.
type Dialect struct {
	driver.Base
	driver.ReadOnlyCommands
	driver.TopologyReader
	tftpPolicy
}

// New returns the built-in Fortinet dialect. Its neighbor command depends
// on the device status, so the reader selects it at run time.
func New() Dialect {
	reader := driver.NewTopologyReader("", topologyParser())
	reader.StatusCommand = "get system status"
	reader.SelectNeighborCommand = neighborCommand
	return Dialect{
		Base:             newBase(),
		ReadOnlyCommands: readOnly,
		TopologyReader:   reader,
		tftpPolicy:       newTFTPPolicy(),
	}
}

func newBase() driver.Base {
	p := driver.BaseTerminal("fortinet")
	p.ConfigCommands = []string{"show full-configuration"}
	p.SaveCommands = []string{"execute cfg save"}
	p.LoginPrompt = driver.Pattern(`(?:\A|[\r\n])[^\s#>$]+(?:[ \t]+\([^\r\n()]+\))?[ \t]*[#\$][ \t]*\z`)
	p.CommandPrompt = p.LoginPrompt
	p.Pager = driver.Pattern(`(?:\A|[\r\n])[ \t]*--More--[ \t\x00\x08]*(?:\r|\n|\z)`)
	p.CommandErrors = driver.Regexps(`(?im)^[ \t]*(?:command parse error|Command fail\.|Unknown action\b|Unknown command\b)`, `(?im)^[ \t]*(?:Permission denied|Access denied|Sorry, you have no permission)\b`)
	p.LoginInteractions = []connector.Interaction{driver.LoginInteraction(`(?i)Press any key to continue[^\r\n]*\z`, " ")}
	p.LoginInteractions[0].Capture = false
	return driver.NewBase(p, 120*time.Second, driver.BaseConfigFactory(), nil)
}

// readOnly lists the commands this family accepts for read-only collection.
var readOnly = driver.ReadOnlyCommands{Verbs: []string{"get", "show"}}
