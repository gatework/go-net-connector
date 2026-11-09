package juniper_test

import (
	"github.com/gatework/go-net-connector/topology"
	"github.com/gatework/go-net-connector/transfer"
	"github.com/gatework/go-net-connector/vendors/dialect"
	"github.com/gatework/go-net-connector/vendors/juniper"
)

// The declared capabilities are checked at compile time: a renamed or
// mistyped method fails the build instead of silently dropping a capability.
// Keep this list in step with the capability matrix in vendors/README.md.
var (
	_ dialect.Platform            = juniper.Dialect{}
	_ topology.NeighborDiscovery  = juniper.Dialect{}
	_ topology.DescriptionReader  = juniper.Dialect{}
	_ topology.InterfaceIdentity  = juniper.Dialect{}
	_ topology.WriteRestriction   = juniper.Dialect{}
	_ transfer.RemoteBackupPolicy = juniper.Dialect{}
)
