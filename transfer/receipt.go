// Package transfer owns device upload and server verification receipts.
package transfer

import "time"

// Verification distinguishes device reports from independent server proof.
type Verification string

const (
	VerificationDeviceReported Verification = "device_reported"
)

type Receipt struct {
	Protocol, Username                                  string
	CompletedAt                                         time.Time
	RequestedPath                                       *string
	Path, Server, ConfigurationKind, SourceFile, Format string
	Verification                                        Verification
}

func (r Receipt) Reported() bool { return r.Verification == VerificationDeviceReported }
