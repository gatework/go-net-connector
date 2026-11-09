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

// Dialect is the built-in H3C Comware switch platform.
type Dialect struct {
	driver.Base
	driver.ReadOnlyCommands
	driver.TopologyReader
	driver.DescriptionWriter
	tftpPolicy
}

// WirelessDialect is the built-in H3C wireless controller platform. It shares
// Comware behavior and differs only in its profile name and inventory match.
type WirelessDialect struct{ Dialect }

// New returns the built-in Comware switch dialect.
func New() Dialect { return newDialect("h3c") }

// Wireless returns the built-in H3C wireless controller dialect.
func Wireless() WirelessDialect { return WirelessDialect{newDialect("h3c_wireless")} }

func newDialect(profileName string) Dialect {
	return Dialect{
		Base:              newBase(profileName),
		ReadOnlyCommands:  readOnly,
		TopologyReader:    driver.NewTopologyReader("display lldp neighbor-information list", topologyParser()),
		DescriptionWriter: driver.NewDescriptionWriter(dialectapi.WriteMode{Enter: "system-view", Leave: "return", InterfaceExit: "quit"}),
		tftpPolicy:        newTFTPPolicy(),
	}
}

var saveConfirmationPattern = driver.Regex(`(?i)^Saved the current configuration to mainboard device successfully\.$`)

func newBase(name string) driver.Base {
	p := driver.BaseTerminal(name)
	p.ConfigCommands = []string{"dis cur"}
	p.SaveCommands = []string{"save force"}
	p.UsernamePrompt = driver.Pattern(`(?i)(?:login|Username):\s*\z`)
	p.LoginPrompt = driver.Pattern(`(?:\A|[\r\n])(?:[^\S\n]|\x00)*\S+[>\]](?:[^\S\n]|\x00)*\z`)
	p.CommandPrompt = driver.Pattern(`(?:\A|[\r\n])(?:[^\S\n]|\x00)*[^\r\n]+[>\]](?:[^\S\n]|\x00)*\z`)
	p.CommandErrors = driver.PositionMarkerCommandErrors()
	p.Interactions = driver.YesNoConfirmations()
	return driver.NewBase(p, 60*time.Second, driver.BaseConfigFactory(), saveConfirmationPattern)
}

var appendRule = driver.Regex(`(?i)\A[ \t]*rule[ \t]+XXX[ \t]+append\b`)
var createRule = driver.Regex(`(?i)\A[ \t]*rule[ \t]+pass\b`)
var assignedRule = driver.Regex(`(?i)rule\s+(\d+)\s+pass[^\n]+\n\s*#`)

// PrepareCommand substitutes the rule id assigned by the preceding
// "rule pass" command into a later "rule XXX append" command.
func (Dialect) PrepareCommand(_ context.Context, c connector.Command, x connector.Executor) (connector.Command, bool, error) {
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

// AfterCommand records the rule id the device assigned to a new pass rule.
func (Dialect) AfterCommand(ctx context.Context, c connector.Command, _ connector.StepResult, x connector.Executor) error {
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

// readOnly lists the commands this family accepts for read-only collection.
var readOnly = driver.ReadOnlyCommands{Verbs: []string{"display", "dis", "ping", "tracert"}}
