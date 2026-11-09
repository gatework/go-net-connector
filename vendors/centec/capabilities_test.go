package centec_test

import (
	"github.com/gatework/go-net-connector/transfer"
	"github.com/gatework/go-net-connector/vendors/centec"
	"github.com/gatework/go-net-connector/vendors/dialect"
)

// The declared capabilities are checked at compile time: a renamed or
// mistyped method fails the build instead of silently dropping a capability.
// Keep this list in step with the capability matrix in vendors/README.md.
var (
	_ dialect.Platform    = centec.Dialect{}
	_ transfer.TFTPPolicy = centec.Dialect{}
)
