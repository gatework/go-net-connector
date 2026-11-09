package radware_test

import (
	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/topology"
	"github.com/gatework/go-net-connector/transfer"
	"github.com/gatework/go-net-connector/vendors/dialect"
	"github.com/gatework/go-net-connector/vendors/radware"
)

// The declared capabilities are checked at compile time: a renamed or
// mistyped method fails the build instead of silently dropping a capability.
// Keep this list in step with the capability matrix in vendors/README.md.
var (
	_ dialect.Platform           = radware.Dialect{}
	_ topology.DescriptionReader = radware.Dialect{}
	_ topology.InterfaceIdentity = radware.Dialect{}
	_ transfer.TFTPPolicy        = radware.Dialect{}
	_ connector.LoginInspector   = radware.Dialect{}
)
