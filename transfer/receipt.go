// Package transfer owns device upload and server verification receipts.
package transfer

import "time"

// Verification distinguishes device reports from independent server proof.
type Verification string

const (
	VerificationDeviceReported Verification = "device_reported"
	VerificationServerVerified Verification = "server_verified"
)

type Receipt struct {
	Protocol, Username                                  string
	CompletedAt                                         time.Time
	RequestedPath                                       *string
	Path, Server, ConfigurationKind, SourceFile, Format string
	Verification                                        Verification
	ServerSHA256, LocalPath, ArchivePath                string
	ServerBytes                                         int64
}

func (r Receipt) Verified() bool { return r.Verification == "server_verified" }
func (r Receipt) Reported() bool { return r.Verification == "device_reported" || r.Verified() }
