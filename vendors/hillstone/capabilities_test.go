package hillstone_test

import (
	"github.com/gatework/go-net-connector/topology"
	"github.com/gatework/go-net-connector/transfer"
	"github.com/gatework/go-net-connector/vendors/dialect"
	"github.com/gatework/go-net-connector/vendors/hillstone"
)

// The declared capabilities are checked at compile time: a renamed or
// mistyped method fails the build instead of silently dropping a capability.
// Keep this list in step with the capability matrix in vendors/README.md.
var (
	_ dialect.Platform           = hillstone.Dialect{}
	_ topology.NeighborDiscovery = hillstone.Dialect{}
	_ topology.DescriptionReader = hillstone.Dialect{}
	_ topology.InterfaceIdentity = hillstone.Dialect{}
	_ topology.DescriptionWriter = hillstone.Dialect{}
	_ transfer.TFTPPolicy        = hillstone.Dialect{}
)
