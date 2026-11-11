// Package transfer owns device upload and server verification receipts.
package transfer

import "time"

// Verification distinguishes device reports from independent server proof.
type Verification string

// Transfer verification levels.
const (
	VerificationDeviceReported Verification = "device_reported"
)

// Receipt records device-reported transfer completion without credentials.
type Receipt struct {
	Protocol, Username                                  string
	CompletedAt                                         time.Time
	RequestedPath                                       *string
	Path, Server, ConfigurationKind, SourceFile, Format string
	Verification                                        Verification
}

// Reported reports whether the device confirmed transfer completion.
func (r Receipt) Reported() bool { return r.Verification == VerificationDeviceReported }
