package cisco_test

import (
	"github.com/gatework/go-net-connector/topology"
	"github.com/gatework/go-net-connector/transfer"
	"github.com/gatework/go-net-connector/vendors/cisco"
	"github.com/gatework/go-net-connector/vendors/dialect"
)

// The declared capabilities are checked at compile time: a renamed or
// mistyped method fails the build instead of silently dropping a capability.
// Keep this list in step with the capability matrix in vendors/README.md.
var (
	_ dialect.Platform           = cisco.IOSDialect{}
	_ topology.NeighborDiscovery = cisco.IOSDialect{}
	_ topology.DescriptionReader = cisco.IOSDialect{}
	_ topology.InterfaceIdentity = cisco.IOSDialect{}
	_ topology.DescriptionWriter = cisco.IOSDialect{}
	_ transfer.TFTPPolicy        = cisco.IOSDialect{}
	_ dialect.Platform           = cisco.NXOSDialect{}
	_ topology.NeighborDiscovery = cisco.NXOSDialect{}
	_ topology.DescriptionReader = cisco.NXOSDialect{}
	_ topology.InterfaceIdentity = cisco.NXOSDialect{}
	_ topology.DescriptionWriter = cisco.NXOSDialect{}
	_ transfer.TFTPPolicy        = cisco.NXOSDialect{}
)
