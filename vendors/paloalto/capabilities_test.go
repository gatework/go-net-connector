package paloalto_test

import (
	"github.com/gatework/go-net-connector/topology"
	"github.com/gatework/go-net-connector/transfer"
	"github.com/gatework/go-net-connector/vendors/dialect"
	"github.com/gatework/go-net-connector/vendors/paloalto"
)

// The declared capabilities are checked at compile time: a renamed or
// mistyped method fails the build instead of silently dropping a capability.
// Keep this list in step with the capability matrix in vendors/README.md.
var (
	_ dialect.Platform           = paloalto.Dialect{}
	_ topology.NeighborDiscovery = paloalto.Dialect{}
	_ topology.DescriptionReader = paloalto.Dialect{}
	_ topology.InterfaceIdentity = paloalto.Dialect{}
	_ topology.WriteRestriction  = paloalto.Dialect{}
	_ transfer.TFTPPolicy        = paloalto.Dialect{}
)
