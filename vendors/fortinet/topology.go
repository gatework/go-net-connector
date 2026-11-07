package fortinet

import (
	"slices"
	"strconv"

	"github.com/gatework/go-net-connector/internal/terminal"
	dialectapi "github.com/gatework/go-net-connector/vendors/dialect"
	"github.com/gatework/go-net-connector/vendors/internal/topology"
)

func topologyParser() topology.Parser {
	return topology.Parser{
		NeighborTemplate:     "fortinet/diagnose_lldp_rx_neighbor.textfsm",
		DescriptionTemplate:  "fortinet/show_full_configuration.textfsm",
		Protocol:             "lldp",
		CountNeighbors:       neighborCount,
		ValidateDescriptions: validateInterfaceRows,
		CanonicalInterface:   identityInterface,
		DecodeInterface:      true,
		DecodeDescription:    true,
		StrictDescription:    true,
	}
}

func identityInterface(name string) string { return name }

func neighborCount(text string) (int, bool) {
	return topology.Count(`(?m)^\s*\d+\s+port\b`, text), topology.Pattern(`(?im)^\s*No (?:LLDP )?neighbors\.?\s*$`).MatchString(text)
}

func validateInterfaceRows(text string, rows []map[string]any) error {
	if topology.Count(`(?m)^config system interface[ \t]*$`, text) != 1 || topology.Pattern(`(?m)^[ \t]+config system interface[ \t]*$`).MatchString(text) {
		return dialectapi.ErrParse
	}
	match := topology.Pattern(`(?ms)^config system interface[ \t]*\n(.*?)^end[ \t]*$`).FindStringSubmatch(text)
	if len(match) != 2 {
		return dialectapi.ErrParse
	}
	expected := []string{}
	for _, item := range topology.Pattern(`(?m)^    edit[ \t]+([^\r\n]+)`).FindAllStringSubmatch(match[1], -1) {
		name, err := topology.DecodeQuoted(item[1], true)
		if err != nil {
			return err
		}
		expected = append(expected, name)
	}
	actual := []string{}
	for _, row := range rows {
		name, err := topology.DecodeQuoted(topology.Field(row, "INTERFACE"), true)
		if err != nil {
			return err
		}
		actual = append(actual, name)
	}
	if !slices.Equal(expected, actual) || topology.Count(`(?m)^    next[ \t]*$`, match[1]) != len(expected) {
		return dialectapi.ErrParse
	}
	return nil
}

// FortinetNeighborCommand chooses the firmware-specific LLDP command and
// refuses to issue it when prompt and active VDOM evidence disagree.
func neighborCommand(status, prompt string) (string, error) {
	rendered, err := terminal.Render([]byte(status), true)
	if err != nil {
		return "", dialectapi.NewDiagnosticError("unrecognized_output", "discover", dialectapi.ErrParse)
	}
	version := topology.Pattern(`(?im)^Version:[^\r\n]*\bv(\d+)\.(\d+)\.(\d+)\b`).FindStringSubmatch(rendered)
	if len(version) != 4 {
		return "", dialectapi.NewDiagnosticError("unrecognized_output", "discover", dialectapi.ErrParse)
	}
	release := [3]int{}
	for i := range release {
		n, err := strconv.Atoi(version[i+1])
		if err != nil {
			return "", dialectapi.NewDiagnosticError("unrecognized_output", "discover", dialectapi.ErrParse)
		}
		release[i] = n
	}
	current := topology.Pattern(`(?im)^Current virtual domain:[ \t]*(\S+)[ \t]*$`).FindStringSubmatch(rendered)
	view := topology.Pattern(`\(([^()\r\n]+)\)[ \t]*[#\$][ \t]*$`).FindStringSubmatch(prompt)
	if len(current) > 1 && len(view) > 1 && current[1] != view[1] {
		return "", dialectapi.NewDiagnosticError("vdom_context_mismatch", "discover", dialectapi.ErrParse)
	}
	if release[0] > 7 || release[0] == 7 && (release[1] > 6 || release[1] == 6 && release[2] >= 3) {
		return "diagnose lldp rx neighbor", nil
	}
	return "diagnose lldprx neighbor", nil
}
