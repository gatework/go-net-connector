// Package huawei implements the Huawei VRP network device dialect.
package huawei

import (
	"time"

	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/vendors/internal/driver"
)

var saveConfirmationPattern = driver.Regex(`(?i)^(?:Info:[ \t]*)?(?:Save the configuration successfully\.|The configuration has been saved successfully\.|Configuration is saved to device successfully\.)$`)

// Dialect is the built-in Huawei VRP platform.
type Dialect struct {
	driver.ProfiledPlatform
	driver.TopologyReader
	tftpPolicy
}

// New returns the built-in Huawei VRP dialect.
func New() Dialect {
	return Dialect{
		ProfiledPlatform: driver.NewProfiledPlatform(newProfile()),
		TopologyReader:   driver.NewTopologyReader("display lldp neighbor brief", topologyParser()),
		tftpPolicy:       newTFTPPolicy(),
	}
}

func newProfile() connector.Profile {
	p := driver.BaseProfile("huawei")
	p.ConfigCommands = []string{"dis cur"}
	p.SaveCommands = []string{"save force"}
	p.UsernamePrompt = driver.Pattern(`(?i)(?:login|Username):\s*\z`)
	p.LoginPrompt = driver.Pattern(`(?:\A|[\r\n])\s*\S+[>\]]\s*\z`)
	p.CommandPrompt = driver.Pattern(`(?:\A|[\r\n])[^\r\n]+[>\]]\s*\z`)
	p.CommandErrors = driver.PositionMarkerCommandErrors()
	p.Interactions = driver.YesNoConfirmations()
	p.PrivilegeCommand = "su"
	p.PrivilegePrompt = driver.Pattern(`(?is)privilege\s+(?:level\s+is|is\s+).+>\s*\z`)
	return driver.FinishProfile(p, 10*time.Second, driver.BaseConfigFactory(), saveConfirmationPattern)
}
