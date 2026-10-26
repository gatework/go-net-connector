// Package vendors declares the supported device CLI dialects.
package vendors

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gatework/go-net-connector/connector"
)

// Names returns a new, sorted list of registered vendor keys.
func Names() []string {
	n := []string{"h3c", "h3c_wireless", "cisco_ios", "cisco_nxos", "huawei", "hillstone", "radware", "palo_alto", "topsec", "fortinet", "juniper", "centec"}
	sort.Strings(n)
	return n
}

// Open builds a vendor device and authenticates its connection.
func Open(ctx context.Context, key string, cfg connector.ConnectionConfig, opts ...connector.Option) (*connector.Device, error) {
	p, e := Profile(key)
	if e != nil {
		return nil, e
	}
	d, e := connector.New(p, cfg, opts...)
	if e != nil {
		return nil, e
	}
	if e = d.Connect(ctx); e != nil {
		return nil, e
	}
	return d, nil
}
func re(s string) *regexp.Regexp { return regexp.MustCompile(s) }
func rules(patterns ...string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, len(patterns))
	for i, s := range patterns {
		out[i] = re(s)
	}
	return out
}
func reply(pattern, response string) connector.Interaction {
	return connector.Interaction{Pattern: re(pattern), Response: response, Capture: true}
}
func loginReply(pattern, response string) connector.Interaction {
	r := reply(pattern, response)
	r.Limit = 1
	return r
}

// Profile constructs an independent declaration from the frozen Ruby dialect.
// Running-configuration strategies carry state only for one collection.
func Profile(name string) (connector.Profile, error) {
	p := connector.Profile{Name: name, UsernamePrompt: re(`(?i)(?:login|username|name):\s*\z`), PasswordPrompt: re(`(?i)password:\s*\z`), PrivilegePrompt: re(`#\s*\z`), Pager: re(`(?i)(?:\A|[\r\n])[ \t]*(?:--More--|---- More ----)[ \t\x00\x08]*(?:\r|\n|\z)`), PagerResponse: " ", AuthenticationErrors: rules(`(?i)(?:authentication failed|permission denied|access denied|login incorrect)`)}
	var seconds int
	switch name {
	case "h3c", "h3c_wireless", "huawei":
		p.ConfigCommands = []string{"dis cur"}
		p.SaveCommands = []string{"save force"}
		p.UsernamePrompt = re(`(?i)(?:login|Username):\s*\z`)
		p.LoginPrompt = re(`(?:\A|[\r\n])(?:[^\S\n]|\x00)*\S+[>\]](?:[^\S\n]|\x00)*\z`)
		p.CommandPrompt = re(`(?:\A|[\r\n])(?:[^\S\n]|\x00)*[^\r\n]+[>\]](?:[^\S\n]|\x00)*\z`)
		p.CommandErrors = rules(`(?im)^[ \t]*%[ \t]+(?:(?:Unrecognized|Ambiguous|Incomplete)\s+command|Too\s+many\s+parameters)\s+found\s+at\s+'\^'\s+position\.`, `(?im)^[ \t]*error:`, `(?im)^[ \t]*Permission\s+denied\.`, `(?m)^\s*\^`)
		p.Interactions = []connector.Interaction{reply(`(?i)Are\s+you\s+sure\?\s*\[Y/N\]`, "y\n"), reply(`(?i)press\s+the\s+enter\s+key\)`, "\n"), reply(`(?i)Continue\?\s*\[Y/N\]`, "y\n"), reply(`(?i)Please\s+input\s+the\s+file\s+name\(\*\.cfg\)\[[^\]]+\]\s*\z`, "\n"), reply(`(?i)overwrite\?\s*\[Y/N\]`, "y\n")}
		seconds = 60
		if name == "huawei" {
			seconds = 10
			p.LoginPrompt = re(`(?:\A|[\r\n])\s*\S+[>\]]\s*\z`)
			p.CommandPrompt = re(`(?:\A|[\r\n])[^\r\n]+[>\]]\s*\z`)
			p.PrivilegeCommand = "su"
			p.PrivilegePrompt = re(`(?is)privilege\s+(?:level\s+is|is\s+).+>\s*\z`)
		} else {
			p.Prepare = prepareH3C
			p.AfterCommand = afterH3C
		}
	case "cisco_ios", "cisco_nxos", "centec":
		p.ConfigCommands = []string{"terminal length 0", "show running-config"}
		p.SaveCommands = []string{"copy running-config startup-config"}
		seconds = 60
		p.LoginPrompt = re(`(?:\A|[\r\n])\S+[>#]\s*\z`)
		p.CommandPrompt = re(`(?:\A|[\r\n])\S+[>#]\s*\z`)
		if name == "centec" {
			p.CommandErrors = rules(`(?im)^[ \t]*%[ \t]+(?:Syntax\s+error|Invalid|Incomplete|Ambiguous|Unknown|Error)\b`)
			break
		}
		p.Interactions = []connector.Interaction{reply(`(?i)Destination\s+filename\s+\[[^\]]+\]\?\s*\z`, "\n"), reply(`(?i)Do\s+you\s+want\s+to\s+overwrite\?\s*\[yes/no\]\s*\z`, "yes\n")}
		if name == "cisco_ios" {
			p.Pager = re(`(?i)(?:\A|[\r\n])[^\S\r\n]*--More--[^\r\n]*(?:\r|\n|\z)`)
			p.CommandErrors = rules(`(?im)^[ \t]*%[ \t]+(?:Invalid|Incomplete|Ambiguous)\s+(?:input|command)\b`, `(?m)^[ \t]*\^`)
			p.Interactions = append(p.Interactions, reply(`(?i)Proceed\s+with\s+copy\?\s*\[confirm\]\s*\z`, "\n"))
		} else {
			p.LoginPrompt = re(`\S+[>#]\s*\z`)
			p.CommandPrompt = re(`\S+[>#]\s*\z`)
			p.SaveCommands = []string{"copy run start"}
			p.Pager = re(`(?i)(?:\A|[\r\n])[ \t]*(?:\x1b\[[0-9;]*[A-Za-z])*--More--(?:[ \t\x00\x08]|\x1b\[[0-9;]*[A-Za-z])*(?:\r|\n|\z)`)
			p.CommandErrors = rules(`(?im)^[ \t]*%?[ \t]*(?:(?:Invalid|Ambiguous|Incomplete)\s+command(?:\s+\([^)]+\))?|Invalid\s+parameter\s+detected)\s+at\s+'\^'\s+marker\.`, `(?m)^[ \t]*\^`, `(?im)^[ \t]*%[ \t]+(?:Invalid|Ambiguous|Incomplete)\s+command\b`, `(?im)^[ \t]*%?[ \t]*Invalid\s+parameter\s+detected\b`, `(?im)^[ \t]*syntax\s+error\b`)
		}
	case "hillstone":
		p.ConfigCommands = []string{"terminal length 0", "show configuration running"}
		p.SaveCommands = []string{"save all"}
		seconds = 60
		p.LoginPrompt = re(`(?:\A|[\r\n])\S+(?:[#>$]|\([MBF]\))\s*\z`)
		p.CommandPrompt = re(`(?:\A|[\r\n])\S+(?:#|\([MBF]\))\s*\z`)
		p.Pager = re(`(?i)(?:\A|[\r\n])[ \t]*--More--[ \t\x00\x08]*(?:\r|\n|\z)`)
		p.CommandErrors = rules(`(?im)^[ \t]*\^-+`, `(?m)^\s*\^`, `(?im)^[ \t]*(?:Unknown|Invalid|Error)\s+command`, `(?im)^[ \t]*Error:\s*[^\r\n]+`)
	case "radware":
		p.ConfigCommands = []string{"/cfg/dump"}
		p.SaveCommands = []string{"/cfg/save"}
		seconds = 30
		p.LoginPrompt = re(`(?:\A|[\r\n])(?:>>[^\r\n]*#|[^>\r\n][^\r\n]*[>#]|>[^>\r\n][^\r\n]*[>#])\s*\z`)
		p.CommandPrompt = re(`(?:\A|[\r\n])[^\r\n]+#\s*\z`)
		p.CommandErrors = rules(`(?im)^[ \t]*(?:Unknown|Invalid|Error)\s+command\b`)
		p.Interactions = []connector.Interaction{reply(`(?i)Confirm\s+Sync\s+to\s+Peer\s+\[y/n\]:\s*\z`, "y\n"), reply(`(?i)Synchronize\s+configuration\s+changes\?\s+\[y/n\]:\s*\z`, "y\n"), reply(`(?i)Confirm\s+saving\s+to\s+FLASH\s+\[y/n\]:\s*\z`, "y\n"), reply(`(?i)Confirm\s+operation\s+without\s+applying\s+changes\s+\(leave\s+them\s+pending\)\s+\[y/n\]:\s*\z`, "y\n"), reply(`(?i)Confirm\s+operation\s+without\s+saving\s+changes\s+\(leave\s+them\s+pending\)\s+\[y/n\]:?\s*\z`, "y\n"), reply(`(?i)Confirm\s+seeing\s+above\s+note\s+\[y\]:\s*\z`, "y\n"), reply(`(?i)Please\s+enter\s+y\s+to\s+perform\s+the\s+action,\s+n\s+to\s+skip\s+it:\s*\z`, "y\n"), reply(`(?i)Display\s+private\s+keys\?\s+\[y/n\]:\s*\z`, "n\n")}
		p.AfterLogin = func(s connector.StepResult) map[string]string {
			m := re(`(?im)^\s*HA\s+State:\s*(\S+)`).FindStringSubmatch(s.Raw())
			if len(m) > 1 {
				return map[string]string{"ha_state": m[1]}
			}
			return nil
		}
	case "palo_alto":
		p.ConfigCommands = []string{"set cli pager off", "set cli config-output-format set", "show config diff", "configure", "show", "exit", "show config diff"}
		seconds = 120
		p.LoginPrompt = re(`(?:\A|[\r\n])[^\s@]+@[^\s>#]+[>#]\s*\z`)
		p.CommandPrompt = re(`(?:\A|[\r\n])[^\s@]+@[^\s>#]+[>#]\s*\z`)
		p.CommandErrors = rules(`(?im)^[ \t]*(?:Invalid syntax|Unknown command|Server error|Command failed)(?:[.:]|\s)`)
		p.LoginInteractions = []connector.Interaction{loginReply(`(?i)(?:acknowledge|accept)[\s\S]*(?:yes/no|\[y/n\]|enter\s+yes)[^\r\n]*\z`, "yes\n")}
	case "topsec":
		p.ConfigCommands = []string{"show-running nostop"}
		seconds = 120
		p.LoginPrompt = re(`(?:\A|[\r\n])[^\s#>]*#\s*\z`)
		p.CommandPrompt = re(`(?:\A|[\r\n])[^\s#>]*#\s*\z`)
		p.CommandErrors = rules(`(?im)^[ \t]*(?:%[ \t]*)?(?:Unknown|Invalid|Incomplete|Ambiguous)\s+command\b`, `(?im)^[ \t]*(?:Error:|Permission denied\b)`)
	case "fortinet":
		p.ConfigCommands = []string{"show full-configuration"}
		p.SaveCommands = []string{"execute cfg save"}
		seconds = 120
		p.LoginPrompt = re(`(?:\A|[\r\n])[^\s#>$]+(?:[ \t]+\([^\r\n()]+\))?[ \t]*[#\$][ \t]*\z`)
		p.CommandPrompt = re(p.LoginPrompt.String())
		p.Pager = re(`(?:\A|[\r\n])[ \t]*--More--[ \t\x00\x08]*(?:\r|\n|\z)`)
		p.CommandErrors = rules(`(?im)^[ \t]*(?:command parse error|Command fail\.|Unknown action\b|Unknown command\b)`, `(?im)^[ \t]*(?:Permission denied|Access denied|Sorry, you have no permission)\b`)
		p.LoginInteractions = []connector.Interaction{loginReply(`(?i)Press any key to continue[^\r\n]*\z`, " ")}
		p.LoginInteractions[0].Capture = false
	case "juniper":
		p.ConfigCommands = []string{"show configuration | display set | no-more"}
		seconds = 120
		p.LoginPrompt = re(`(?:\A|[\r\n])[^\s@]+@[^\s>#]+>[ \t]*\z`)
		p.CommandPrompt = re(p.LoginPrompt.String())
		p.Pager = re(`(?i)(?:\A|[\r\n])[ \t]*---\(more(?: \d+%)?\)---[ \t]*\z`)
		p.CommandErrors = rules(`(?im)^[ \t]*(?:error:|syntax error[,.]|unknown command\b|permission denied\b|(?:ftp|scp|ssh):\s)`)
	default:
		return connector.Profile{}, fmt.Errorf("vendors: unknown vendor: %w", connector.ErrUnsupported)
	}
	p.CommandTimeout = time.Duration(seconds) * time.Second
	p.ConfigStrategy = func() connector.ConfigStrategy { return &configStrategy{vendor: name} }
	p.Capabilities = connector.Capabilities{Save: len(p.SaveCommands) > 0, TFTP: name != "topsec" && name != "juniper", RemoteBackup: name == "juniper", Descriptions: name != "topsec" && name != "centec", Neighbors: name != "radware" && name != "topsec" && name != "centec", TopologyWrite: name == "h3c" || name == "h3c_wireless" || name == "cisco_ios" || name == "cisco_nxos" || name == "hillstone"}
	return p, nil
}

var appendRule = re(`(?i)\A[ \t]*rule[ \t]+XXX[ \t]+append\b`)
var createRule = re(`(?i)\A[ \t]*rule[ \t]+pass\b`)
var assignedRule = re(`(?i)rule\s+(\d+)\s+pass[^\n]+\n\s*#`)

func prepareH3C(_ context.Context, c connector.Command, x connector.Executor) (connector.Command, bool, error) {
	if appendRule.MatchString(c.Text) {
		id := x.Get("rule_id")
		if id == "" {
			return c, false, fmt.Errorf("vendors: missing rule id: %w", connector.ErrIncomplete)
		}
		i := strings.Index(strings.ToUpper(c.Text), "XXX")
		c.Text = c.Text[:i] + id + c.Text[i+3:]
	}
	return c, false, nil
}
func afterH3C(ctx context.Context, c connector.Command, _ connector.StepResult, x connector.Executor) error {
	if !createRule.MatchString(c.Text) {
		return nil
	}
	s, e := x.Execute(ctx, connector.Command{Text: "dis this", OutputSensitive: true})
	if e != nil {
		return e
	}
	m := assignedRule.FindStringSubmatch(s.Raw())
	if len(m) < 2 {
		return fmt.Errorf("vendors: missing assigned rule id: %w", connector.ErrIncomplete)
	}
	x.Set("rule_id", m[1])
	return nil
}
