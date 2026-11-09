package h3c

import (
	"strings"

	"github.com/gatework/go-net-connector/internal/ifname"
	"github.com/gatework/go-net-connector/vendors/internal/topology"
)

var headerPattern = topology.Pattern(`(?i)^\s*(?:System Name\s+Local Interface\b|(?:Local Interface|LocalIf)\s+)`)

func topologyParser() topology.Parser {
	return topology.Parser{
		NeighborTemplate:     "h3c/display_lldp_neighbor_information_list.textfsm",
		DescriptionTemplate:  "h3c/display_current_configuration.textfsm",
		Protocol:             topology.ProtocolLLDP,
		CountNeighbors:       neighborCount,
		ValidateDescriptions: topology.ValidateInterfaceRows,
		CanonicalInterface:   ifname.Key,
	}
}

func neighborCount(text string) (int, bool) {
	lines, seen := topology.TableLines(text, headerPattern)
	if !seen {
		return -1, false
	}
	column := 0
	if topology.Pattern(`(?im)^[ \t]*System Name[ \t]+Local Interface\b`).MatchString(text) {
		column = 1
	}
	pattern := topology.Pattern(`(?i)^(?:[A-Za-z][A-Za-z-]*\d+(?:/\d+)+|(?:Bridge-Aggregation|BAGG|Route-Aggregation|RAGG)\d+)(?:[:.]\d+)*$`)
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) <= column || !pattern.MatchString(fields[column]) {
			return -1, false
		}
	}
	return len(lines), len(lines) == 0
}
