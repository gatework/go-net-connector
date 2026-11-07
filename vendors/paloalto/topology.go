package paloalto

import (
	"strings"

	"github.com/gatework/go-net-connector/internal/ifname"
	"github.com/gatework/go-net-connector/vendors/internal/topology"
)

func topologyParser() topology.Parser {
	return topology.Parser{
		NeighborTemplate:    "palo_alto/show_lldp_neighbors_all.textfsm",
		DescriptionTemplate: "palo_alto/show.textfsm",
		Protocol:            "lldp",
		CountNeighbors:      neighborCount,
		CanonicalInterface:  ifname.Key,
		StrictDescription:   true,
	}
}

func neighborCount(text string) (int, bool) {
	blocks := topology.Pattern(`(?im)^\s*Local information:\s*$`).Split(text, -1)
	total, empty := 0, 0
	for _, block := range blocks {
		if !topology.Pattern(`(?im)^\s*Local interface:\s*\S+`).MatchString(block) {
			continue
		}
		total++
		parts := topology.Pattern(`(?im)^\s*Neighbor information:\s*$`).Split(block, 2)
		if len(parts) != 2 {
			continue
		}
		blank := true
		for _, line := range strings.Split(parts[1], "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !topology.Pattern(`^\S+[>#]\s*$`).MatchString(line) {
				blank = false
			}
		}
		if blank {
			empty++
		}
	}
	return total - empty, topology.Pattern(`(?i)No LLDP neighbors`).MatchString(text) || total > 0 && total == empty
}
