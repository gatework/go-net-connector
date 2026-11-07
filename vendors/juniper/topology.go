package juniper

import (
	"github.com/gatework/go-net-connector/internal/ifname"
	"github.com/gatework/go-net-connector/vendors/internal/topology"
)

func topologyParser() topology.Parser {
	return topology.Parser{
		NeighborTemplate:    "juniper/show_lldp_neighbors_detail.textfsm",
		DescriptionTemplate: "juniper/show_configuration_display_set.textfsm",
		Protocol:            "lldp",
		CountNeighbors:      neighborCount,
		CanonicalInterface:  ifname.Key,
		AppendInterfaceUnit: true,
		DecodeDescription:   true,
		StrictDescription:   true,
	}
}

func neighborCount(text string) (int, bool) {
	empty := topology.Pattern(`(?im)^\s*(?:No LLDP neighbors\.?|Total entries displayed\s*:\s*0)\s*$`).MatchString(text)
	validRecord := topology.Pattern(`(?im)^\s*(?:Local Interface|Chassis (?:ID|Id)|Port ID|System name)\s*:`).MatchString(text)
	return topology.Count(`(?im)^\s*Local Interface\s*:`, text), empty && !validRecord
}
