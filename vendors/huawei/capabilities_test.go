package huawei_test

import (
	"github.com/gatework/go-net-connector/topology"
	"github.com/gatework/go-net-connector/transfer"
	"github.com/gatework/go-net-connector/vendors/dialect"
	"github.com/gatework/go-net-connector/vendors/huawei"
)

// The declared capabilities are checked at compile time: a renamed or
// mistyped method fails the build instead of silently dropping a capability.
// Keep this list in step with the capability matrix in vendors/README.md.
var (
	_ dialect.Platform           = huawei.Dialect{}
	_ topology.NeighborDiscovery = huawei.Dialect{}
	_ topology.DescriptionReader = huawei.Dialect{}
	_ topology.InterfaceIdentity = huawei.Dialect{}
	_ transfer.TFTPPolicy        = huawei.Dialect{}
)
