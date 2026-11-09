package huawei

import (
	"strings"

	"github.com/gatework/go-net-connector/internal/ifname"
	"github.com/gatework/go-net-connector/vendors/internal/topology"
)

var headerPattern = topology.Pattern(`(?i)^[ \t]*Local Intf[ \t]+Neighbor Dev[ \t]+Neighbor Intf[ \t]+Exptime`)

func topologyParser() topology.Parser {
	return topology.Parser{
		NeighborTemplate:     "huawei/display_lldp_neighbor_brief.textfsm",
		DescriptionTemplate:  "huawei/display_current_configuration.textfsm",
		Protocol:             topology.ProtocolLLDP,
		CountNeighbors:       neighborCount,
		ValidateDescriptions: topology.ValidateInterfaceRows,
		CanonicalInterface: func(name string) string {
			return ifname.Key(strings.NewReplacer(" ", "", "\t", "").Replace(name))
		},
	}
}

func neighborCount(text string) (int, bool) {
	lines, seen := topology.TableLines(text, headerPattern)
	return len(lines), seen && len(lines) == 0
}
