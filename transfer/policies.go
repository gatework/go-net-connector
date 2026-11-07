package transfer

import (
	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/vendors/dialect"
)

// TFTPPolicy is the device-specific half of a TFTP operation. Shared target
// grammar, service verification and archive lifecycle stay in transfer.
type TFTPPolicy interface {
	DefaultVRF() string
	ValidateOptions(deviceHost string, options dialect.TFTPOptions) error
	Filename(host, label string) (string, error)
	SourceFileCommand() string
	ResolveSourceFile(output string) (string, error)
	Command(deviceHost string, options dialect.TFTPOptions, explicitPath bool) connector.Command
	Completed(lines []string, options dialect.TFTPOptions) (actualPath string, complete bool, err error)
	Receipt(options dialect.TFTPOptions, explicitSource, explicitPath bool) dialect.TFTPReceipt
	AllowsConcurrentDestinationReuse() bool
	RequiresSourceFile() bool
	RequiresVerificationRoot() bool
}

// RemoteBackupPolicy defines a platform's native remote configuration export.
type RemoteBackupPolicy interface {
	Destination(options dialect.RemoteBackupOptions) string
	Command(options dialect.RemoteBackupOptions, password string) connector.Command
	Completed(lines []string, destination string) bool
}
