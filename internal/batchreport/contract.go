// Package batchreport owns the persisted batch report contract shared by every
// operation (backup, TFTP, remote backup, deploy, exec, topology) and read back
// by artifact export and diff.
package batchreport

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/netip"
)

// SchemaVersion identifies the unified report format.
const SchemaVersion = 3

// MaxArtifactBytes bounds one exported artifact.
const MaxArtifactBytes int64 = 64 << 20

var ErrInvalidEvidence = errors.New("batchreport: invalid artifact evidence")

// Device status values. Plan skip reasons (filtered, duplicate_host, …) are
// also used verbatim as a device status for devices that never started.
const (
	StatusSucceeded   = "succeeded"
	StatusPartial     = "partial"
	StatusFailed      = "failed"
	StatusInterrupted = "interrupted"
	StatusNotStarted  = "not_started"
	StatusStopped     = "stopped"
)

// Batch status values.
const (
	BatchSucceeded   = "succeeded"
	BatchIncomplete  = "incomplete"
	BatchInterrupted = "interrupted"
	BatchNoDevices   = "no_devices"
)

// Report is report.json for one batch directory.
type Report struct {
	SchemaVersion int            `json:"schema_version"`
	Operation     string         `json:"operation"`
	Policy        string         `json:"policy"`
	Success       bool           `json:"success"`
	Status        string         `json:"status"`
	Directory     string         `json:"directory"`
	StartedAt     string         `json:"started_at"`
	FinishedAt    string         `json:"finished_at"`
	DurationMS    float64        `json:"duration_ms"`
	Workers       int            `json:"workers"`
	Total         int            `json:"total"`
	Counts        map[string]int `json:"counts"`
	Stopped       bool           `json:"stopped"`
	Interrupted   bool           `json:"interrupted"`
	SessionLogs   string         `json:"session_logs,omitempty"`
	ReportError   string         `json:"report_error,omitempty"`
	Devices       []Device       `json:"devices"`
}

// Diagnostic is a stable, safe classification: never error text or output.
type Diagnostic struct {
	Code  string `json:"code"`
	Phase string `json:"phase"`
}

// Stage records one step of the per-device pipeline.
type Stage struct {
	Name       string      `json:"name"`
	Status     string      `json:"status"` // succeeded | partial | failed | skipped
	DurationMS float64     `json:"duration_ms"`
	Diagnostic *Diagnostic `json:"diagnostic,omitempty"`
}

// Device is one inventory entry and, when it ran, its pipeline evidence.
type Device struct {
	Host         string      `json:"host"`
	Name         string      `json:"name,omitempty"`
	Platform     string      `json:"platform,omitempty"`
	SourceVendor string      `json:"source_vendor,omitempty"`
	Status       string      `json:"status"`
	Outcome      string      `json:"outcome,omitempty"`
	FailedStage  string      `json:"failed_stage,omitempty"`
	Diagnostic   *Diagnostic `json:"diagnostic,omitempty"`
	Verified     bool        `json:"verified"`
	StartedAt    string      `json:"started_at,omitempty"`
	DurationMS   float64     `json:"duration_ms,omitempty"`
	SessionLog   string      `json:"session_log,omitempty"`
	Target       *Target     `json:"target,omitempty"`
	Stages       []Stage     `json:"stages,omitempty"`
	Artifacts    Artifacts   `json:"artifacts"`
}

// Target is what the plan intends for a device, before any I/O: the backup
// file name, the TFTP destination or the script to deploy.
type Target struct {
	File             string `json:"file,omitempty"`
	Server           string `json:"server,omitempty"`
	VerificationRoot string `json:"verification_root,omitempty"`
	SourceFile       string `json:"source_file,omitempty"`
	SourceInterface  string `json:"source_interface,omitempty"`
	VRF              string `json:"vrf,omitempty"`
	Script           string `json:"script,omitempty"`
	Commands         int    `json:"commands,omitempty"`
}

// Artifacts holds the evidence each stage produced. Unset stages are omitted.
type Artifacts struct {
	Backup   *Backup         `json:"backup,omitempty"`
	Transfer *Transfer       `json:"transfer,omitempty"`
	Archive  *ArchiveReceipt `json:"archive,omitempty"`
	Remote   *Remote         `json:"remote,omitempty"`
	Script   *Script         `json:"script,omitempty"`
	Save     *Save           `json:"save,omitempty"`
	Outputs  []Output        `json:"outputs,omitempty"`
	// Topology carries the audit, plan and apply evidence of a topology run.
	Topology any `json:"topology,omitempty"`
}

type FileReceipt struct {
	Path  string `json:"path"`
	State string `json:"state"`
	Phase string `json:"phase"`
}

// Backup is a locally saved configuration.
type Backup struct {
	Path           string        `json:"path"`
	Bytes          int64         `json:"bytes"`
	SHA256         string        `json:"sha256"`
	Change         string        `json:"change"`
	PreviousSHA256 string        `json:"previous_sha256,omitempty"`
	Encoding       string        `json:"encoding,omitempty"`
	Source         *BackupSource `json:"source,omitempty"`
	TextWarning    *TextWarning  `json:"text_warning,omitempty"`
	File           FileReceipt   `json:"file"`
	// Saved: the configuration is on disk; Complete: device completion and
	// file durability are both confirmed.
	Saved    bool `json:"saved"`
	Complete bool `json:"complete"`
}

type SourceSegment struct {
	Step   int `json:"step"`
	Offset int `json:"offset"`
	Bytes  int `json:"bytes"`
}

type BackupSource struct {
	Path            string          `json:"path"`
	Bytes           int             `json:"bytes"`
	SHA256          string          `json:"sha256"`
	File            FileReceipt     `json:"file"`
	NoWriteVerified bool            `json:"no_write_verified"`
	Segments        []SourceSegment `json:"segments"`
}

type TextPosition struct {
	Offset int `json:"offset"`
	Line   int `json:"line"`
	Column int `json:"column"`
}

type TextWarning struct {
	Count     int            `json:"count"`
	Positions []TextPosition `json:"positions"`
}

// Transfer is a device-reported TFTP upload.
type Transfer struct {
	RemotePath        string `json:"remote_path"`
	Server            string `json:"server"`
	ConfigurationKind string `json:"configuration_kind,omitempty"`
	SourceFile        string `json:"source_file,omitempty"`
	Format            string `json:"format,omitempty"`
	Verification      string `json:"verification"`
}

type ArchiveProof struct {
	Bytes       int64  `json:"bytes"`
	SHA256      string `json:"sha256"`
	ArchivePath string `json:"archive_path"`
	Verified    bool   `json:"verified"`
}

// ArchiveReceipt is the server-side verification and local retrieval of an upload.
type ArchiveReceipt struct {
	Server  *ArchiveProof `json:"server,omitempty"`
	Local   FileReceipt   `json:"local"`
	Cleanup string        `json:"cleanup"`
}

// Remote is a device-native export to a remote server (SCP/FTP).
type Remote struct {
	Server      string `json:"server"`
	Destination string `json:"destination"`
	Protocol    string `json:"protocol"`
	Reported    bool   `json:"reported"`
}

type Attempt struct {
	Line     int  `json:"line"`
	Written  int  `json:"written"`
	Complete bool `json:"complete"`
	Failed   bool `json:"failed"`
}

// Script is a deployed command script. Accepted counts only commands whose
// responses passed the rejection rules; Attempts include the failed command.
type Script struct {
	Name       string    `json:"name"`
	Commands   int       `json:"commands"`
	Accepted   int       `json:"accepted"`
	FailedLine int       `json:"failed_line,omitempty"`
	Attempts   []Attempt `json:"attempts"`
}

// Save records a configuration save; Confirmed requires positive evidence.
type Save struct {
	Confirmed bool `json:"confirmed"`
}

// Output is one saved read-only command output.
type Output struct {
	Command string `json:"command"`
	Path    string `json:"path"`
	Bytes   int64  `json:"bytes"`
	SHA256  string `json:"sha256"`
}

// Artifact identifies one saved configuration selected from a report.
type Artifact struct {
	// Subdirectory is derived from the artifact kind, never from a
	// report-controlled path: TFTP retrievals live under "tftp".
	Subdirectory string
	Host         string
	Path         string
	Bytes        int64
	SHA256       string
	Status       string
}

func NormalizeHost(host string) (string, error) {
	address, err := netip.ParseAddr(host)
	if err != nil || address.Zone() != "" {
		return "", ErrInvalidEvidence
	}
	return address.String(), nil
}

// SelectArtifact returns the unique saved configuration for host: a local
// backup, or a TFTP upload whose server copy was verified and retrieved.
// IPv4-mapped IPv6 keeps its family.
func SelectArtifact(data []byte, host string) (Artifact, error) {
	canonicalHost, err := NormalizeHost(host)
	if err != nil {
		return Artifact{}, ErrInvalidEvidence
	}
	address, _ := netip.ParseAddr(canonicalHost)
	var report Report
	if json.Unmarshal(data, &report) != nil || report.SchemaVersion != SchemaVersion {
		return Artifact{}, ErrInvalidEvidence
	}
	var selected *Device
	for i := range report.Devices {
		candidate := &report.Devices[i]
		a, parseErr := netip.ParseAddr(candidate.Host)
		if parseErr == nil && a.Zone() == "" && a == address {
			if selected != nil {
				return Artifact{}, ErrInvalidEvidence
			}
			selected = candidate
		}
	}
	if selected == nil {
		return Artifact{}, ErrInvalidEvidence
	}
	artifact := Artifact{Host: address.String(), Status: selected.Status}
	switch a := selected.Artifacts; {
	case a.Backup != nil:
		artifact.Path, artifact.Bytes, artifact.SHA256 = a.Backup.Path, a.Backup.Bytes, a.Backup.SHA256
	case a.Archive != nil && a.Archive.Server != nil && a.Archive.Server.Verified && (a.Archive.Local.State == "committed" || a.Archive.Local.State == "durable"):
		artifact.Subdirectory = "tftp"
		artifact.Path, artifact.Bytes, artifact.SHA256 = a.Archive.Local.Path, a.Archive.Server.Bytes, a.Archive.Server.SHA256
	default:
		return Artifact{}, ErrInvalidEvidence
	}
	checksum, err := hex.DecodeString(artifact.SHA256)
	if artifact.Path == "" || artifact.Bytes < 0 || artifact.Bytes > MaxArtifactBytes || err != nil || len(checksum) != 32 {
		return Artifact{}, ErrInvalidEvidence
	}
	return artifact, nil
}
