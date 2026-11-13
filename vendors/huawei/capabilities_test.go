package huawei_test

import (
	"github.com/gatework/go-net-connector/platform"
	"github.com/gatework/go-net-connector/topology"
	"github.com/gatework/go-net-connector/transfer"
	"github.com/gatework/go-net-connector/vendors/huawei"
)

// The declared capabilities are checked at compile time: a renamed or
// mistyped method fails the build instead of silently dropping a capability.
// Keep this list in step with the capability matrix in vendors/README.md.
var (
	_ platform.Platform          = huawei.Platform{}
	_ topology.NeighborDiscovery = huawei.Platform{}
	_ topology.DescriptionReader = huawei.Platform{}
	_ topology.InterfaceIdentity = huawei.Platform{}
	_ transfer.TFTPPolicy        = huawei.Platform{}
)
