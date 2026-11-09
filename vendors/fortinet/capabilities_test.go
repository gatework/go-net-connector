package fortinet_test

import (
	"github.com/gatework/go-net-connector/topology"
	"github.com/gatework/go-net-connector/transfer"
	"github.com/gatework/go-net-connector/vendors/dialect"
	"github.com/gatework/go-net-connector/vendors/fortinet"
)

// The declared capabilities are checked at compile time: a renamed or
// mistyped method fails the build instead of silently dropping a capability.
// Keep this list in step with the capability matrix in vendors/README.md.
var (
	_ dialect.Platform           = fortinet.Dialect{}
	_ topology.NeighborDiscovery = fortinet.Dialect{}
	_ topology.DescriptionReader = fortinet.Dialect{}
	_ topology.InterfaceIdentity = fortinet.Dialect{}
	_ transfer.TFTPPolicy        = fortinet.Dialect{}
)
