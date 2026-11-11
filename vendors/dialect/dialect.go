// Package dialect contains the leaf contracts shared by vendor drivers and
// the workflows that consume them. It intentionally imports no workflow
// package such as topology or transfer.
package dialect

import (
	"errors"
	"fmt"
	"time"

	"github.com/gatework/go-net-connector/connector"
)

var (
	// ErrUnsupported reports a platform behavior that is not available.
	ErrUnsupported = errors.New("vendor dialect: unsupported behavior")
	// ErrParse reports output that the platform cannot recognize.
	ErrParse = errors.New("vendor dialect: unrecognized output")
)

// DiagnosticError carries stable machine-readable failure metadata across a
// vendor-policy boundary without importing a workflow package.
type DiagnosticError struct {
	Code, Phase string
	Cause       error
}

func (e *DiagnosticError) Error() string {
	if e == nil {
		return "vendor dialect: diagnostic unavailable"
	}
	return fmt.Sprintf("vendor dialect: %s (%s)", e.Code, e.Phase)
}
func (e *DiagnosticError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// DiagnosticCode returns the stable failure code without formatting its cause.
func (e *DiagnosticError) DiagnosticCode() string {
	if e == nil {
		return ""
	}
	return e.Code
}

// DiagnosticPhase returns the operation phase without formatting its cause.
func (e *DiagnosticError) DiagnosticPhase() string {
	if e == nil {
		return ""
	}
	return e.Phase
}

// DiagnosticMetadata lets consumers preserve structured errors while keeping
// the shared dialect package independent of business workflows.
//
//nolint:iface // shared contract: topology and workflow classify errors through it
type DiagnosticMetadata interface {
	error
	DiagnosticCode() string
	DiagnosticPhase() string
}

// NewDiagnosticError attaches safe failure metadata while retaining the cause for unwrapping.
func NewDiagnosticError(code, phase string, cause error) error {
	return &DiagnosticError{Code: code, Phase: phase, Cause: cause}
}

// Platform is one authoritative CLI platform: a device driver with a
// canonical key and an inventory matcher. Optional business capabilities are
// interfaces defined by the packages that consume them.
type Platform interface {
	connector.Driver
	Name() string
	// MatchInventory classifies a normalized inventory identity.
	MatchInventory(identity InventoryIdentity) bool
}

// Registry resolves immutable dialect objects by their canonical platform key.
//
//nolint:iface // shared contract consumed by fleet, topology, transfer and workflow
type Registry interface {
	Names() []string
	Lookup(name string) (Platform, error)
}

// InventoryIdentity contains the vendor, operating system and model reported
// by an inventory source. Built-in registries normalize these values before
// asking platform matchers to classify them.
type InventoryIdentity struct {
	Vendor, OS, Model string
}

// Neighbor is the workflow-neutral form of a discovered adjacency.
type Neighbor struct {
	LocalInterface    string `json:"local_interface"`
	NeighborName      string `json:"neighbor_name"`
	NeighborInterface string `json:"neighbor_interface"`
	ChassisID         string `json:"chassis_id"`
	Protocol          string `json:"protocol"`
}

// WriteMode describes the mode transitions around a platform's interface
// description commands.
type WriteMode struct{ Enter, Leave, InterfaceExit string }

// Named source interfaces accepted by platforms that distinguish their
// management and data planes instead of taking a device interface name.
const (
	SourceInterfaceManagement = "management"
	SourceInterfaceData       = "data"
)

// TFTPOptions carries target-specific upload choices across the package
// boundary without importing transfer.
type TFTPOptions struct {
	Server, Path, SourceFile, VRF, SourceInterface string
	Timeout                                        time.Duration
}

// TFTPReceipt describes the configuration format and requested-path evidence of an upload.
type TFTPReceipt struct {
	ConfigurationKind, Format, SourceFile string
	IncludeRequestedPath                  bool
}

// RemoteBackupOptions carries options for a platform's native remote export.
type RemoteBackupOptions struct {
	Server, Path, Username, Protocol string
}
