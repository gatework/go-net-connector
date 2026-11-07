// Package huawei implements the Huawei VRP network device dialect.
package huawei

import (
	"time"

	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/vendors/internal/driver"
)

var saveConfirmationPattern = driver.Regex(`(?i)^(?:Info:[ \t]*)?(?:Save the configuration successfully\.|The configuration has been saved successfully\.|Configuration is saved to device successfully\.)$`)

type dialect struct {
	driver.ProfiledPlatform
	driver.NeighborPolicy
	driver.DescriptionReader
	driver.InterfaceIdentityPolicy
	tftpPolicy
}

// New returns the built-in Huawei VRP dialect.
func New() dialect {
	parser := topologyParser()
	return dialect{
		ProfiledPlatform:        driver.NewProfiledPlatform(newProfile()),
		NeighborPolicy:          driver.NewNeighborPolicy("display lldp neighbor brief", parser),
		DescriptionReader:       driver.DescriptionReader{Parser: parser},
		InterfaceIdentityPolicy: driver.InterfaceIdentityPolicy{Parser: parser},
		tftpPolicy:              newTFTPPolicy(),
	}
}

func newProfile() connector.Profile {
	p := driver.BaseProfile("huawei")
	p.ConfigCommands = []string{"dis cur"}
	p.SaveCommands = []string{"save force"}
	p.UsernamePrompt = driver.Pattern(`(?i)(?:login|Username):\s*\z`)
	p.LoginPrompt = driver.Pattern(`(?:\A|[\r\n])\s*\S+[>\]]\s*\z`)
	p.CommandPrompt = driver.Pattern(`(?:\A|[\r\n])[^\r\n]+[>\]]\s*\z`)
	p.CommandErrors = driver.Regexps(`(?im)^[ \t]*%[ \t]+(?:(?:Unrecognized|Ambiguous|Incomplete)\s+command|Too\s+many\s+parameters)\s+found\s+at\s+'\^'\s+position\.`, `(?im)^[ \t]*error:`, `(?im)^[ \t]*Permission\s+denied\.`, `(?m)^\s*\^`)
	p.Interactions = []connector.Interaction{driver.Interaction(`(?i)Are\s+you\s+sure\?\s*\[Y/N\]`, "y\n"), driver.Interaction(`(?i)press\s+the\s+enter\s+key\)`, "\n"), driver.Interaction(`(?i)Continue\?\s*\[Y/N\]`, "y\n"), driver.Interaction(`(?i)Please\s+input\s+the\s+file\s+name\(\*\.cfg\)\[[^\]]+\]\s*\z`, "\n"), driver.Interaction(`(?i)overwrite\?\s*\[Y/N\]`, "y\n")}
	p.PrivilegeCommand = "su"
	p.PrivilegePrompt = driver.Pattern(`(?is)privilege\s+(?:level\s+is|is\s+).+>\s*\z`)
	return driver.FinishProfile(p, 10*time.Second, driver.BaseConfigFactory(), saveConfirmationPattern)
}
