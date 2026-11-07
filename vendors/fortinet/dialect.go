// Package fortinet implements the Fortinet network device dialect.
package fortinet

import (
	"time"

	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/vendors/internal/driver"
)

type dialect struct {
	driver.ProfiledPlatform
	driver.NeighborPolicy
	driver.DescriptionReader
	driver.InterfaceIdentityPolicy
	tftpPolicy
}

// New returns the built-in Fortinet dialect.
func New() dialect {
	parser := topologyParser()
	return dialect{
		ProfiledPlatform: driver.NewProfiledPlatform(newProfile()),
		NeighborPolicy: driver.NeighborPolicy{
			StatusCommand: "get system status", Parser: parser,
			SelectNeighborCommand: neighborCommand,
		},
		DescriptionReader:       driver.DescriptionReader{Parser: parser},
		InterfaceIdentityPolicy: driver.InterfaceIdentityPolicy{Parser: parser},
		tftpPolicy:              newTFTPPolicy(),
	}
}

func newProfile() connector.Profile {
	p := driver.BaseProfile("fortinet")
	p.ConfigCommands = []string{"show full-configuration"}
	p.SaveCommands = []string{"execute cfg save"}
	p.LoginPrompt = driver.Pattern(`(?:\A|[\r\n])[^\s#>$]+(?:[ \t]+\([^\r\n()]+\))?[ \t]*[#\$][ \t]*\z`)
	p.CommandPrompt = p.LoginPrompt
	p.Pager = driver.Pattern(`(?:\A|[\r\n])[ \t]*--More--[ \t\x00\x08]*(?:\r|\n|\z)`)
	p.CommandErrors = driver.Regexps(`(?im)^[ \t]*(?:command parse error|Command fail\.|Unknown action\b|Unknown command\b)`, `(?im)^[ \t]*(?:Permission denied|Access denied|Sorry, you have no permission)\b`)
	p.LoginInteractions = []connector.Interaction{driver.LoginInteraction(`(?i)Press any key to continue[^\r\n]*\z`, " ")}
	p.LoginInteractions[0].Capture = false
	return driver.FinishProfile(p, 120*time.Second, driver.BaseConfigFactory(), nil)
}
