package topology

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/gatework/go-net-connector/internal/terminal"
	"github.com/gatework/go-net-connector/textfsm"
)

func re(pattern string) *regexp.Regexp { return regexp.MustCompile(pattern) }

var descriptionTemplates = map[string]string{"h3c": "h3c/display_current_configuration.textfsm", "h3c_wireless": "h3c/display_current_configuration.textfsm", "huawei": "huawei/display_current_configuration.textfsm", "cisco_ios": "cisco/show_running_config.textfsm", "cisco_nxos": "cisco_nxos/show_running_config.textfsm", "hillstone": "hillstone/show_configuration_running.textfsm", "fortinet": "fortinet/show_full_configuration.textfsm", "juniper": "juniper/show_configuration_display_set.textfsm", "palo_alto": "palo_alto/show.textfsm", "radware": "radware/cfg_dump.textfsm"}
var neighborTemplates = map[string]string{"h3c": "h3c/display_lldp_neighbor_information_list.textfsm", "h3c_wireless": "h3c/display_lldp_neighbor_information_list.textfsm", "huawei": "huawei/display_lldp_neighbor_brief.textfsm", "cisco_ios": "cisco/show_cdp_neighbors_detail.textfsm", "cisco_nxos": "cisco_nxos/show_cdp_neighbors_detail.textfsm", "hillstone": "hillstone/show_lldp_neighbor_information.textfsm", "fortinet": "fortinet/diagnose_lldp_rx_neighbor.textfsm", "juniper": "juniper/show_lldp_neighbors_detail.textfsm", "palo_alto": "palo_alto/show_lldp_neighbors_all.textfsm"}

func field(row map[string]any, name string) string { s, _ := row[name].(string); return s }
func parseError() error                            { return failure("unrecognized_output", "parse", ErrParse) }

// ParseNeighbors parses frozen vendor templates and independently checks record
// counts; a recognized empty table is distinct from missing or partial output.
func ParseNeighbors(vendor, output string) ([]Neighbor, error) {
	template, ok := neighborTemplates[vendor]
	if !ok {
		return nil, failure("neighbor_discovery_unsupported", "discover", ErrUnsupported)
	}
	text, e := terminal.Render([]byte(output), true)
	if e != nil {
		return nil, parseError()
	}
	expected, empty := neighborCount(vendor, text)
	if expected < 0 {
		return nil, parseError()
	}
	rows, e := textfsm.ParseTemplate(template, text)
	if e != nil || len(rows) != expected || (len(rows) == 0 && !empty) {
		return nil, parseError()
	}
	neighbors := make([]Neighbor, 0, len(rows))
	protocol := "lldp"
	if vendor == "cisco_ios" || vendor == "cisco_nxos" {
		protocol = "cdp"
	}
	for _, r := range rows {
		neighbors = append(neighbors, Neighbor{strings.TrimSpace(field(r, "LOCAL_INTERFACE")), strings.TrimSpace(field(r, "NEIGHBOR_NAME")), strings.TrimSpace(field(r, "NEIGHBOR_INTERFACE")), strings.TrimSpace(field(r, "CHASSIS_ID")), protocol})
	}
	return neighbors, nil
}
func count(pattern, text string) int { return len(re(pattern).FindAllStringIndex(text, -1)) }

var h3cHeader = re(`(?i)^\s*(?:System Name\s+Local Interface\b|(?:Local Interface|LocalIf)\s+)`)
var huaweiHeader = re(`(?i)^[ \t]*Local Intf[ \t]+Neighbor Dev[ \t]+Neighbor Intf[ \t]+Exptime`)
var promptLine = re(`^(?:<[^>]+>|\[[^\]]+\])$`)

func tableLines(output string, header *regexp.Regexp) ([]string, bool) {
	seen := false
	lines := []string{}
	for _, line := range strings.Split(output, "\n") {
		if header.MatchString(line) {
			seen = true
			continue
		}
		if !seen {
			continue
		}
		line = strings.TrimSpace(line)
		if line == "" || re(`^[-=+ ]+$`).MatchString(line) || promptLine.MatchString(line) {
			continue
		}
		lines = append(lines, line)
	}
	return lines, seen
}
func neighborCount(vendor, text string) (int, bool) {
	switch vendor {
	case "h3c", "h3c_wireless":
		lines, seen := tableLines(text, h3cHeader)
		if !seen {
			return -1, false
		}
		column := 0
		if re(`(?im)^[ \t]*System Name[ \t]+Local Interface\b`).MatchString(text) {
			column = 1
		}
		pattern := re(`(?i)^(?:[A-Za-z][A-Za-z-]*\d+(?:/\d+)+|(?:Bridge-Aggregation|BAGG|Route-Aggregation|RAGG)\d+)(?:[:.]\d+)*$`)
		for _, line := range lines {
			fields := strings.Fields(line)
			if len(fields) <= column || !pattern.MatchString(fields[column]) {
				return -1, false
			}
		}
		return len(lines), len(lines) == 0
	case "huawei":
		lines, seen := tableLines(text, huaweiHeader)
		if !seen {
			return -1, false
		}
		return len(lines), len(lines) == 0
	case "cisco_ios":
		return count(`(?im)^\s*Device ID:\s*\S+`, text), re(`(?i)Total cdp entries displayed\s*:\s*0|no cdp neighbors`).MatchString(text)
	case "cisco_nxos":
		return count(`(?im)^\s*Device[ -]ID:`, text), re(`(?i)Total cdp entries displayed\s*:\s*0|no cdp neighbors`).MatchString(text)
	case "hillstone":
		lines := []string{}
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || line == "show lldp neighbor-information" || re(`(?i)^(?:System Name\s+Local Interface\b|Total lldp neighbor number:\s*\d+\.?$|[-=+ ]+$|\S+(?:#|\([MBF]\))$)`).MatchString(line) {
				continue
			}
			lines = append(lines, line)
		}
		for _, line := range lines {
			if !re(`(?i)^\S+\s+(?:(?:x?ethernet|eth|xe)\d+/\d+|(?:aggregate|agg)\d+)(?:\.\d+)?\s`).MatchString(line) {
				return -1, false
			}
		}
		if m := re(`(?i)Total lldp neighbor number:\s*(\d+)`).FindStringSubmatch(text); len(m) > 0 {
			n, _ := strconv.Atoi(m[1])
			if n != len(lines) {
				return -1, false
			}
		}
		return len(lines), len(lines) == 0 && re(`(?i)Total lldp neighbor number:\s*0\b|System Name\s+Local Interface`).MatchString(text)
	case "fortinet":
		return count(`(?m)^\s*\d+\s+port\b`, text), re(`(?im)^\s*No (?:LLDP )?neighbors\.?\s*$`).MatchString(text)
	case "juniper":
		return count(`(?im)^\s*Local Interface\s*:`, text), re(`(?im)^\s*(?:No LLDP neighbors\.?|Total entries displayed\s*:\s*0)\s*$`).MatchString(text) && !re(`(?im)^\s*(?:Local Interface|Chassis (?:ID|Id)|Port ID|System name)\s*:`).MatchString(text)
	case "palo_alto":
		blocks := re(`(?im)^\s*Local information:\s*$`).Split(text, -1)
		total, empty := 0, 0
		for _, block := range blocks {
			if !re(`(?im)^\s*Local interface:\s*\S+`).MatchString(block) {
				continue
			}
			total++
			parts := re(`(?im)^\s*Neighbor information:\s*$`).Split(block, 2)
			if len(parts) != 2 {
				continue
			}
			blank := true
			for _, line := range strings.Split(parts[1], "\n") {
				line = strings.TrimSpace(line)
				if line != "" && !re(`^\S+[>#]\s*$`).MatchString(line) {
					blank = false
				}
			}
			if blank {
				empty++
			}
		}
		return total - empty, re(`(?i)No LLDP neighbors`).MatchString(text) || (total > 0 && total == empty)
	}
	return -1, false
}

// ParseDescriptions retains physical/logical interface identity and rejects
// duplicate conflicting values, incomplete blocks, and truncated quoted values.
func ParseDescriptions(vendor, config string) (map[string]string, error) {
	template, ok := descriptionTemplates[vendor]
	if !ok {
		return nil, failure("description_unsupported", "discover", ErrUnsupported)
	}
	text, e := terminal.Render([]byte(config), true)
	if e != nil {
		return nil, parseError()
	}
	rows, e := textfsm.ParseTemplate(template, text)
	if e != nil {
		return nil, parseError()
	}
	switch vendor {
	case "h3c", "h3c_wireless", "cisco_ios", "cisco_nxos", "hillstone", "huawei":
		expected := []string{}
		for _, m := range re(`(?m)^interface[ \t]+([^\r\n]*)`).FindAllStringSubmatch(text, -1) {
			expected = append(expected, strings.TrimSpace(m[1]))
		}
		actual := []string{}
		for _, r := range rows {
			actual = append(actual, strings.TrimSpace(field(r, "INTERFACE")))
		}
		if !slices.Equal(expected, actual) {
			return nil, parseError()
		}
	case "fortinet":
		if count(`(?m)^config system interface[ \t]*$`, text) != 1 || re(`(?m)^[ \t]+config system interface[ \t]*$`).MatchString(text) {
			return nil, parseError()
		}
		m := re(`(?ms)^config system interface[ \t]*\n(.*?)^end[ \t]*$`).FindStringSubmatch(text)
		if len(m) != 2 {
			return nil, parseError()
		}
		expected := []string{}
		for _, match := range re(`(?m)^    edit[ \t]+([^\r\n]+)`).FindAllStringSubmatch(m[1], -1) {
			n, e := decodeQuoted(match[1], true)
			if e != nil {
				return nil, e
			}
			expected = append(expected, n)
		}
		actual := []string{}
		for _, r := range rows {
			n, e := decodeQuoted(field(r, "INTERFACE"), true)
			if e != nil {
				return nil, e
			}
			actual = append(actual, n)
		}
		if !slices.Equal(expected, actual) || count(`(?m)^    next[ \t]*$`, m[1]) != len(expected) {
			return nil, parseError()
		}
	}
	out := map[string]string{}
	for _, r := range rows {
		name := field(r, "INTERFACE")
		if vendor == "fortinet" {
			name, e = decodeQuoted(name, true)
			if e != nil {
				return nil, e
			}
		}
		name = key(vendor, name)
		if vendor == "juniper" && field(r, "UNIT") != "" {
			name += "." + field(r, "UNIT")
		}
		description := strings.TrimSpace(field(r, "DESCRIPTION"))
		decoded, err := decodeQuoted(description, vendor == "fortinet" || vendor == "juniper")
		if err != nil {
			switch vendor {
			case "fortinet", "juniper", "palo_alto", "radware":
				return nil, err
			default:
				decoded = description
			}
		}
		if old, ok := out[name]; ok {
			if old != "" && decoded != "" && old != decoded {
				return nil, failure("ambiguous_description", "parse", ErrParse)
			}
			if decoded == "" {
				continue
			}
		}
		out[name] = decoded
	}
	return out, nil
}

// decodeQuoted mirrors Shellwords' quote and escape handling without invoking a shell.
func decodeQuoted(value string, split bool) (string, error) {
	value = strings.TrimSpace(value)
	if !split && !strings.HasPrefix(value, `"`) {
		return value, nil
	}
	words := []string{}
	var word strings.Builder
	var quote rune
	started, escape := false, false
	for _, r := range value {
		if escape {
			if quote == '"' && r != '"' && r != '\\' && r != '$' && r != '`' && r != '\n' {
				word.WriteRune('\\')
			}
			if r != '\n' {
				word.WriteRune(r)
			}
			escape = false
			started = true
			continue
		}
		if r == '\\' && quote != '\'' {
			escape = true
			started = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				word.WriteRune(r)
			}
			continue
		}
		if r == '"' || r == '\'' {
			quote = r
			started = true
			continue
		}
		if unicode.IsSpace(r) {
			if started {
				words = append(words, word.String())
				word.Reset()
				started = false
			}
			continue
		}
		word.WriteRune(r)
		started = true
	}
	if quote != 0 || escape {
		return "", parseError()
	}
	if started {
		words = append(words, word.String())
	}
	return strings.Join(words, " "), nil
}
