// Package hillstone implements the Hillstone network device dialect.
package hillstone

import (
	"time"

	"github.com/gatework/go-net-connector/connector"
	dialectapi "github.com/gatework/go-net-connector/vendors/dialect"
	"github.com/gatework/go-net-connector/vendors/internal/driver"
)

var saveConfirmationPattern = driver.Regex(`(?i)^Saving configuration is finished\.?$`)

type dialect struct {
	driver.ProfiledPlatform
	driver.NeighborPolicy
	driver.DescriptionReader
	driver.InterfaceIdentityPolicy
	driver.DescriptionWriter
	tftpPolicy
}

// New returns the built-in Hillstone dialect.
func New() dialect {
	parser := topologyParser()
	return dialect{
		ProfiledPlatform:        driver.NewProfiledPlatform(newProfile()),
		NeighborPolicy:          driver.NewNeighborPolicy("show lldp neighbor-information", parser),
		DescriptionReader:       driver.DescriptionReader{Parser: parser},
		InterfaceIdentityPolicy: driver.InterfaceIdentityPolicy{Parser: parser},
		DescriptionWriter:       driver.NewDescriptionWriter(dialectapi.WriteMode{Enter: "configure", Leave: "exit", InterfaceExit: "exit"}),
		tftpPolicy:              newTFTPPolicy(),
	}
}

func newProfile() connector.Profile {
	p := driver.BaseProfile("hillstone")
	p.ConfigCommands = []string{"terminal length 0", "show configuration running"}
	p.SaveCommands = []string{"save all"}
	p.LoginPrompt = driver.Pattern(`(?:\A|[\r\n])\S+(?:[#>$]|\([MBF]\))\s*\z`)
	p.CommandPrompt = driver.Pattern(`(?:\A|[\r\n])\S+(?:#|\([MBF]\))\s*\z`)
	p.Pager = driver.Pattern(`(?i)(?:\A|[\r\n])[ \t]*--More--[ \t\x00\x08]*(?:\r|\n|\z)`)
	p.CommandErrors = driver.Regexps(`(?im)^[ \t]*\^-+`, `(?m)^\s*\^`, `(?im)^[ \t]*(?:Unknown|Invalid|Error)\s+command`, `(?im)^[ \t]*Error:\s*[^\r\n]+`)
	return driver.FinishProfile(p, 60*time.Second, configFactory(), saveConfirmationPattern)
}
