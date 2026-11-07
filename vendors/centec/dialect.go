// Package centec implements the Centec network device dialect.
package centec

import (
	"time"

	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/vendors/internal/driver"
)

var saveConfirmationPattern = driver.Regex(`(?i)^(?:\[OK\]|Copy complete\.)$`)

type dialect struct {
	driver.ProfiledPlatform
	tftpPolicy
}

// New returns the built-in Centec dialect.
func New() dialect {
	return dialect{ProfiledPlatform: driver.NewProfiledPlatform(newProfile()), tftpPolicy: newTFTPPolicy()}
}

func newProfile() connector.Profile {
	p := driver.BaseProfile("centec")
	p.ConfigCommands = []string{"terminal length 0", "show running-config"}
	p.SaveCommands = []string{"copy running-config startup-config"}
	p.LoginPrompt = driver.Pattern(`(?:\A|[\r\n])\S+[>#]\s*\z`)
	p.CommandPrompt = p.LoginPrompt
	p.CommandErrors = driver.Regexps(`(?im)^[ \t]*%[ \t]+(?:Syntax\s+error|Invalid|Incomplete|Ambiguous|Unknown|Error)\b`)
	return driver.FinishProfile(p, 60*time.Second, configFactory(), saveConfirmationPattern)
}
