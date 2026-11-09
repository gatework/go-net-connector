package cisco

import (
	"github.com/gatework/go-net-connector/internal/ifname"
	"github.com/gatework/go-net-connector/vendors/internal/topology"
)

func iosTopologyParser() topology.Parser {
	return topology.Parser{
		NeighborTemplate:     "cisco/show_cdp_neighbors_detail.textfsm",
		DescriptionTemplate:  "cisco/show_running_config.textfsm",
		Protocol:             topology.ProtocolCDP,
		CountNeighbors:       iosNeighborCount,
		ValidateDescriptions: topology.ValidateInterfaceRows,
		CanonicalInterface:   ifname.Key,
	}
}

func nxosTopologyParser() topology.Parser {
	return topology.Parser{
		NeighborTemplate:     "cisco_nxos/show_cdp_neighbors_detail.textfsm",
		DescriptionTemplate:  "cisco_nxos/show_running_config.textfsm",
		Protocol:             topology.ProtocolCDP,
		CountNeighbors:       nxosNeighborCount,
		ValidateDescriptions: topology.ValidateInterfaceRows,
		CanonicalInterface:   ifname.Key,
	}
}

func iosNeighborCount(text string) (int, bool) {
	return topology.Count(`(?im)^\s*Device ID:\s*\S+`, text), topology.Pattern(`(?i)Total cdp entries displayed\s*:\s*0|no cdp neighbors`).MatchString(text)
}

func nxosNeighborCount(text string) (int, bool) {
	return topology.Count(`(?im)^\s*Device[ -]ID:`, text), topology.Pattern(`(?i)Total cdp entries displayed\s*:\s*0|no cdp neighbors`).MatchString(text)
}
