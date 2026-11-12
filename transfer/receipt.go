// Package transfer owns device upload and server verification receipts.
package transfer

import "time"

// Verification distinguishes device reports from independent server proof.
type Verification string

// VerificationDeviceReported means only the device reported completion.
const (
	VerificationDeviceReported Verification = "device_reported"
)

// Receipt records how far a replacement got: its state and the phase it
// reached.
type Receipt struct {
	Protocol, Username                                  string
	CompletedAt                                         time.Time
	RequestedPath                                       *string
	Path, Server, ConfigurationKind, SourceFile, Format string
	Verification                                        Verification
}

// Reported reports whether the device reported completion.
func (r Receipt) Reported() bool { return r.Verification == VerificationDeviceReported }
