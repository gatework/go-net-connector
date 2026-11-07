// Package h3c implements H3C and H3C Wireless network device dialects.
package h3c

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gatework/go-net-connector/connector"
	dialectapi "github.com/gatework/go-net-connector/vendors/dialect"
	"github.com/gatework/go-net-connector/vendors/internal/driver"
)

type dialect struct {
	driver.ProfiledPlatform
	driver.NeighborPolicy
	driver.DescriptionReader
	driver.InterfaceIdentityPolicy
	driver.DescriptionWriter
	tftpPolicy
}
type wirelessDialect struct {
	driver.ProfiledPlatform
	driver.NeighborPolicy
	driver.DescriptionReader
	driver.InterfaceIdentityPolicy
	driver.DescriptionWriter
	tftpPolicy
}

// New returns the built-in Comware switch dialect.
func New() dialect {
	parser := topologyParser()
	return dialect{
		ProfiledPlatform:        driver.NewProfiledPlatform(newProfile("h3c")),
		NeighborPolicy:          driver.NewNeighborPolicy("display lldp neighbor-information list", parser),
		DescriptionReader:       driver.DescriptionReader{Parser: parser},
		InterfaceIdentityPolicy: driver.InterfaceIdentityPolicy{Parser: parser},
		DescriptionWriter:       driver.NewDescriptionWriter(dialectapi.WriteMode{Enter: "system-view", Leave: "return", InterfaceExit: "quit"}),
		tftpPolicy:              newTFTPPolicy(),
	}
}

// Wireless returns the built-in H3C wireless controller dialect.
func Wireless() wirelessDialect {
	parser := topologyParser()
	return wirelessDialect{
		ProfiledPlatform:        driver.NewProfiledPlatform(newProfile("h3c_wireless")),
		NeighborPolicy:          driver.NewNeighborPolicy("display lldp neighbor-information list", parser),
		DescriptionReader:       driver.DescriptionReader{Parser: parser},
		InterfaceIdentityPolicy: driver.InterfaceIdentityPolicy{Parser: parser},
		DescriptionWriter:       driver.NewDescriptionWriter(dialectapi.WriteMode{Enter: "system-view", Leave: "return", InterfaceExit: "quit"}),
		tftpPolicy:              newTFTPPolicy(),
	}
}

var saveConfirmationPattern = driver.Regex(`(?i)^Saved the current configuration to mainboard device successfully\.$`)

func newProfile(name string) connector.Profile {
	p := driver.BaseProfile(name)
	p.ConfigCommands = []string{"dis cur"}
	p.SaveCommands = []string{"save force"}
	p.UsernamePrompt = driver.Pattern(`(?i)(?:login|Username):\s*\z`)
	p.LoginPrompt = driver.Pattern(`(?:\A|[\r\n])(?:[^\S\n]|\x00)*\S+[>\]](?:[^\S\n]|\x00)*\z`)
	p.CommandPrompt = driver.Pattern(`(?:\A|[\r\n])(?:[^\S\n]|\x00)*[^\r\n]+[>\]](?:[^\S\n]|\x00)*\z`)
	p.CommandErrors = driver.Regexps(`(?im)^[ \t]*%[ \t]+(?:(?:Unrecognized|Ambiguous|Incomplete)\s+command|Too\s+many\s+parameters)\s+found\s+at\s+'\^'\s+position\.`, `(?im)^[ \t]*error:`, `(?im)^[ \t]*Permission\s+denied\.`, `(?m)^\s*\^`)
	p.Interactions = []connector.Interaction{driver.Interaction(`(?i)Are\s+you\s+sure\?\s*\[Y/N\]`, "y\n"), driver.Interaction(`(?i)press\s+the\s+enter\s+key\)`, "\n"), driver.Interaction(`(?i)Continue\?\s*\[Y/N\]`, "y\n"), driver.Interaction(`(?i)Please\s+input\s+the\s+file\s+name\(\*\.cfg\)\[[^\]]+\]\s*\z`, "\n"), driver.Interaction(`(?i)overwrite\?\s*\[Y/N\]`, "y\n")}
	p.Prepare = prepareCommand
	p.AfterCommand = captureAssignedRuleID
	return driver.FinishProfile(p, 60*time.Second, driver.BaseConfigFactory(), saveConfirmationPattern)
}

var appendRule = driver.Regex(`(?i)\A[ \t]*rule[ \t]+XXX[ \t]+append\b`)
var createRule = driver.Regex(`(?i)\A[ \t]*rule[ \t]+pass\b`)
var assignedRule = driver.Regex(`(?i)rule\s+(\d+)\s+pass[^\n]+\n\s*#`)

func prepareCommand(_ context.Context, c connector.Command, x connector.Executor) (connector.Command, bool, error) {
	if appendRule.MatchString(c.Text) {
		id := x.Value("rule_id")
		if id == "" {
			return c, false, fmt.Errorf("vendors: missing rule id: %w", connector.ErrIncomplete)
		}
		i := strings.Index(strings.ToUpper(c.Text), "XXX")
		c.Text = c.Text[:i] + id + c.Text[i+3:]
	}
	return c, false, nil
}
func captureAssignedRuleID(ctx context.Context, c connector.Command, _ connector.StepResult, x connector.Executor) error {
	if !createRule.MatchString(c.Text) {
		return nil
	}
	s, e := x.Execute(ctx, connector.Command{Text: "dis this", OutputSensitive: true})
	if e != nil {
		return e
	}
	m := assignedRule.FindStringSubmatch(s.Response().Raw())
	if len(m) < 2 {
		return fmt.Errorf("vendors: missing assigned rule id: %w", connector.ErrIncomplete)
	}
	x.SetValue("rule_id", m[1])
	return nil
}
