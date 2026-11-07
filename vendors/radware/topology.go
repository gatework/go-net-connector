package radware

import (
	"github.com/gatework/go-net-connector/internal/ifname"
	"github.com/gatework/go-net-connector/vendors/internal/topology"
)

func topologyParser() topology.Parser {
	return topology.Parser{
		DescriptionTemplate: "radware/cfg_dump.textfsm",
		CanonicalInterface:  ifname.Key,
		StrictDescription:   true,
	}
}
