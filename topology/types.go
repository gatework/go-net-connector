// Package topology discovers neighbors and safely plans interface descriptions.
package topology

import (
	"errors"
	"fmt"
	"github.com/gatework/go-net-connector/connector"
)

var (
	ErrInvalid      = errors.New("topology: invalid plan")
	ErrUnsupported  = errors.New("topology: unsupported operation")
	ErrParse        = errors.New("topology: unrecognized output")
	ErrStale        = errors.New("topology: stale plan")
	ErrConfirmation = errors.New("topology: confirmation required")
	ErrVerification = errors.New("topology: description unconfirmed")
	ErrPersistence  = errors.New("topology: persistence unconfirmed")
)

// Error formats metadata only; response bodies are never included in diagnostics.
type Error struct {
	Code, Phase string
	cause       error
}

func (e *Error) Error() string                      { return "topology: " + e.Code + " (" + e.Phase + ")" }
func (e *Error) GoString() string                   { return e.Error() }
func (e *Error) Unwrap() error                      { return e.cause }
func failure(code, phase string, cause error) error { return &Error{code, phase, cause} }

type Neighbor struct {
	LocalInterface    string `json:"local_interface"`
	NeighborName      string `json:"neighbor_name"`
	NeighborInterface string `json:"neighbor_interface"`
	ChassisID         string `json:"chassis_id"`
	Protocol          string `json:"protocol"`
}
type Change struct {
	Interface      string   `json:"interface"`
	OldDescription string   `json:"old_description"`
	NewDescription string   `json:"new_description"`
	Neighbor       Neighbor `json:"neighbor"`
}
type Evidence [3]string

// Plan contains exact, ordered change, verification, and persistence commands.
// Apply snapshots and validates all fields before performing any device I/O.
type Plan struct {
	Host     string                 `json:"host"`
	Vendor   string                 `json:"vendor"`
	Evidence map[string]Evidence    `json:"evidence"`
	Changes  []Change               `json:"changes"`
	Commands []string               `json:"commands"`
	Scope    string                 `json:"scope"`
	Receipts []connector.StepResult `json:"receipts,omitempty"`
}
type Comparison struct {
	Interface string  `json:"interface"`
	Neighbor  string  `json:"neighbor"`
	Port      string  `json:"port"`
	Current   *string `json:"current"`
	Expected  *string `json:"expected"`
	Status    string  `json:"status"`
	Reason    string  `json:"reason,omitempty"`
}
type Issue struct {
	Interface string `json:"interface"`
	Code      string `json:"code"`
}
type Audit struct {
	Plan           *Plan                  `json:"plan"`
	Comparisons    []Comparison           `json:"comparisons"`
	Issues         []Issue                `json:"issues"`
	ManualRequired bool                   `json:"manual_required"`
	Receipts       []connector.StepResult `json:"receipts,omitempty"`
}

func (a Audit) Complete() bool { return len(a.Issues) == 0 }
func (a Audit) Changes() int {
	n := 0
	for _, c := range a.Comparisons {
		if c.Status != "matches" && c.Status != "unverified" {
			n++
		}
	}
	return n
}

// FormatOptions defaults to abbreviated port names. Callbacks run synchronously
// inside the device operation lease and must honor their caller's cancellation.
type FormatOptions struct {
	FullInterfaceNames bool
	Lowercase          bool
	Formatter          func(Neighbor) (string, error)
	DescriptionMatcher func(string, string, Neighbor) bool
}
type ApplyOptions struct {
	Confirmed bool
	Execute   connector.ExecuteOptions
}
type Snapshot struct {
	Neighbors    []Neighbor             `json:"neighbors"`
	Descriptions map[string]string      `json:"descriptions"`
	Receipts     []connector.StepResult `json:"receipts"`
}
type Result struct {
	Receipts  []connector.StepResult `json:"receipts"`
	Verified  bool                   `json:"verified"`
	Persisted bool                   `json:"persisted"`
}

func (r Result) Steps() []connector.StepResult {
	return append([]connector.StepResult(nil), r.Receipts...)
}
func (r Result) String() string {
	return fmt.Sprintf("topology.Result{steps:%d,verified:%t,persisted:%t}", len(r.Receipts), r.Verified, r.Persisted)
}
func (r Result) GoString() string { return r.String() }
