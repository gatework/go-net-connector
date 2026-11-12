package archive

import (
	"github.com/gatework/go-net-connector/storage"
	"github.com/gatework/go-net-connector/transfer"
)

// CleanupState distinguishes an unrequested cleanup from an uncertain delete.
type CleanupState string

// Cleanup states.
const (
	CleanupNotRequested CleanupState = "not_requested"
	CleanupUnconfirmed  CleanupState = "unconfirmed"
	CleanupComplete     CleanupState = "complete"
)

// Receipt preserves independent server, local persistence and cleanup evidence.
// Server is present once the worker announces an archive; Verified additionally
// requires independent verification of the delivered body and trailer.
type Receipt struct {
	Server  *Proof          `json:"server,omitempty"`
	Local   storage.Receipt `json:"local"`
	Cleanup CleanupState    `json:"cleanup"`
}

// Verified reports whether the delivered archive matched the worker's proof.
func (r Receipt) Verified() bool { return r.Server != nil && r.Server.Verified }

// Complete reports whether the archive was verified, stored durably and the
// staging upload removed.
func (r Receipt) Complete() bool {
	return r.Verified() && r.Local.Durable() && r.Cleanup == CleanupComplete
}

// Clone returns a copy that does not share the proof.
func (r Receipt) Clone() Receipt {
	if r.Server != nil {
		proof := *r.Server
		r.Server = &proof
	}
	return r
}

// Result never folds a persistence or cleanup failure into device completion.
type Result struct {
	Transfer transfer.Receipt
	Receipt  Receipt
}
