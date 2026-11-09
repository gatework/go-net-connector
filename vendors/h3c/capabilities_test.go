package h3c_test

import (
	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/topology"
	"github.com/gatework/go-net-connector/transfer"
	"github.com/gatework/go-net-connector/vendors/dialect"
	"github.com/gatework/go-net-connector/vendors/h3c"
)

// The declared capabilities are checked at compile time: a renamed or
// mistyped method fails the build instead of silently dropping a capability.
// Keep this list in step with the capability matrix in vendors/README.md.
var (
	_ dialect.Platform           = h3c.Dialect{}
	_ topology.NeighborDiscovery = h3c.Dialect{}
	_ topology.DescriptionReader = h3c.Dialect{}
	_ topology.InterfaceIdentity = h3c.Dialect{}
	_ topology.DescriptionWriter = h3c.Dialect{}
	_ transfer.TFTPPolicy        = h3c.Dialect{}
	_ connector.CommandPreparer  = h3c.Dialect{}
	_ connector.CommandObserver  = h3c.Dialect{}
	_ dialect.Platform           = h3c.WirelessDialect{}
	_ topology.NeighborDiscovery = h3c.WirelessDialect{}
	_ topology.DescriptionReader = h3c.WirelessDialect{}
	_ topology.InterfaceIdentity = h3c.WirelessDialect{}
	_ topology.DescriptionWriter = h3c.WirelessDialect{}
	_ transfer.TFTPPolicy        = h3c.WirelessDialect{}
	_ connector.CommandPreparer  = h3c.WirelessDialect{}
	_ connector.CommandObserver  = h3c.WirelessDialect{}
)
