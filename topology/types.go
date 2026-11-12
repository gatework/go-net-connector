// Package topology discovers neighbors and safely plans interface descriptions.
package topology

import (
	"errors"
	"fmt"

	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/vendors/dialect"
)

// Errors returned by topology planning and apply.
var (
	ErrInvalid      = errors.New("topology: invalid plan")
	ErrUnsupported  = errors.New("topology: unsupported operation")
	ErrParse        = dialect.ErrParse
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

func (e *Error) Error() string { return "topology: " + e.Code + " (" + e.Phase + ")" }

// GoString returns the same safe text as Error.
func (e *Error) GoString() string                    { return e.Error() }
func (e *Error) Unwrap() error                       { return e.cause }
func newError(code, phase string, cause error) error { return &Error{code, phase, cause} }

// Neighbor is one discovered link.
type Neighbor = dialect.Neighbor

// Change replaces one interface description.
type Change struct {
	Interface      string   `json:"interface"`
	OldDescription string   `json:"old_description"`
	NewDescription string   `json:"new_description"`
	Neighbor       Neighbor `json:"neighbor"`
}

// Evidence records the neighbor name, neighbor port and current description a
// change was planned from.
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

// Comparison is the audit result for one interface.
type Comparison struct {
	Interface string  `json:"interface"`
	Neighbor  string  `json:"neighbor"`
	Port      string  `json:"port"`
	Current   *string `json:"current"`
	Expected  *string `json:"expected"`
	Status    string  `json:"status"`
	Reason    string  `json:"reason,omitempty"`
}

// Issue names why an interface could not be verified.
type Issue struct {
	Interface string `json:"interface"`
	Code      string `json:"code"`
}

// Audit is the comparison of every linked interface, the plan when the
// platform can write descriptions, and the issues found.
type Audit struct {
	Plan           *Plan                  `json:"plan"`
	Comparisons    []Comparison           `json:"comparisons"`
	Issues         []Issue                `json:"issues"`
	ManualRequired bool                   `json:"manual_required"`
	Receipts       []connector.StepResult `json:"receipts,omitempty"`
}

// Complete reports whether every interface was verified.
func (a Audit) Complete() bool { return len(a.Issues) == 0 }

// Changes counts the interfaces whose description differs from the expected
// one.
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

// ApplyOptions confirms an apply and passes command options through.
type ApplyOptions struct {
	Confirmed bool
	Execute   connector.ExecuteOptions
}

// Snapshot is the discovered neighbors and descriptions with their receipts.
type Snapshot struct {
	Neighbors    []Neighbor             `json:"neighbors"`
	Descriptions map[string]string      `json:"descriptions"`
	Receipts     []connector.StepResult `json:"receipts"`
}

// Result reports an apply: its receipts and whether the change was verified
// and saved.
type Result struct {
	Receipts  []connector.StepResult `json:"receipts"`
	Verified  bool                   `json:"verified"`
	Persisted bool                   `json:"persisted"`
}

// Steps returns a copy of the receipts.
func (r Result) Steps() []connector.StepResult {
	return append([]connector.StepResult(nil), r.Receipts...)
}
func (r Result) String() string {
	return fmt.Sprintf("topology.Result{steps:%d,verified:%t,persisted:%t}", len(r.Receipts), r.Verified, r.Persisted)
}

// GoString returns the same text as String.
func (r Result) GoString() string { return r.String() }
