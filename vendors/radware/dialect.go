// Package radware implements the Radware ADC network device dialect.
package radware

import (
	"context"
	"time"

	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/vendors/internal/driver"
)

var haStatePattern = driver.Regex(`(?im)^\s*HA\s+State:\s*(\S+)`)

// Dialect is the built-in Radware ADC platform. It reads interface
// descriptions but does not discover neighbors.
type Dialect struct {
	driver.ProfiledPlatform
	driver.DescriptionReader
	driver.InterfaceIdentityPolicy
	tftpPolicy
}

// New returns the built-in Radware ADC dialect.
func New() Dialect {
	parser := topologyParser()
	return Dialect{
		ProfiledPlatform:        driver.NewProfiledPlatform(newProfile()),
		DescriptionReader:       driver.DescriptionReader{Parser: parser},
		InterfaceIdentityPolicy: driver.InterfaceIdentityPolicy{Parser: parser},
		tftpPolicy:              newTFTPPolicy(),
	}
}

func newProfile() connector.Profile {
	p := driver.BaseProfile("radware")
	p.LoginInteractions = []connector.Interaction{driver.LoginInteraction(`(?i)Confirm\s+seeing\s+above\s+note\s+\[y\]:\s*\z`, "y\n")}
	p.ConfigCommands = []string{"/cfg/dump"}
	p.SaveCommands = []string{"/cfg/save"}
	p.LoginPrompt = driver.Pattern(`(?:\A|[\r\n])(?:>>[^\r\n]*#|[^>\r\n][^\r\n]*[>#]|>[^>\r\n][^\r\n]*[>#])\s*\z`)
	p.CommandPrompt = driver.Pattern(`(?:\A|[\r\n])[^\r\n]+#\s*\z`)
	p.CommandErrors = driver.Regexps(`(?im)^[ \t]*(?:Unknown|Invalid|Error)\s+command\b`)
	p.Interactions = []connector.Interaction{driver.Interaction(`(?i)Confirm\s+Sync\s+to\s+Peer\s+\[y/n\]:\s*\z`, "y\n"), driver.Interaction(`(?i)Synchronize\s+configuration\s+changes\?\s+\[y/n\]:\s*\z`, "y\n"), driver.Interaction(`(?i)Confirm\s+saving\s+to\s+FLASH\s+\[y/n\]:\s*\z`, "y\n"), driver.Interaction(`(?i)Confirm\s+operation\s+without\s+applying\s+changes\s+\(leave\s+them\s+pending\)\s+\[y/n\]:\s*\z`, "y\n"), driver.Interaction(`(?i)Confirm\s+operation\s+without\s+saving\s+changes\s+\(leave\s+them\s+pending\)\s+\[y/n\]:?\s*\z`, "y\n"), driver.Interaction(`(?i)Confirm\s+seeing\s+above\s+note\s+\[y\]:\s*\z`, "y\n"), driver.Interaction(`(?i)Please\s+enter\s+y\s+to\s+perform\s+the\s+action,\s+n\s+to\s+skip\s+it:\s*\z`, "y\n"), driver.Interaction(`(?i)Display\s+private\s+keys\?\s+\[y/n\]:\s*\z`, "n\n")}
	p.AfterLogin = func(_ context.Context, step connector.StepResult) (connector.LoginState, error) {
		match := haStatePattern.FindStringSubmatch(step.Response().Raw())
		if len(match) > 1 {
			return connector.LoginState{HAState: match[1]}, nil
		}
		return connector.LoginState{}, nil
	}
	return driver.FinishProfile(p, 30*time.Second, configFactory(), nil)
}
