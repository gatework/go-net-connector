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
const SchemaVersion = 4

// MaxArtifactBytes bounds one exported artifact.
const MaxArtifactBytes int64 = 64 << 20

// ErrInvalidEvidence reports missing, ambiguous or invalid artifact evidence.
var ErrInvalidEvidence = errors.New("batchreport: invalid artifact evidence")

// Status is the state of one inventory entry in a batch. Entries that never
// started carry the reason they were skipped.
type Status string

// Device statuses of entries that were admitted to the batch.
const (
	StatusSucceeded   Status = "succeeded"
	StatusPartial     Status = "partial"
	StatusFailed      Status = "failed"
	StatusInterrupted Status = "interrupted"
	StatusNotStarted  Status = "not_started"
	StatusStopped     Status = "stopped"
)

// Skip statuses of entries that were never admitted.
const (
	StatusReady                Status = "ready"
	StatusFiltered             Status = "filtered"
	StatusSampleLimit          Status = "sample_limit"
	StatusInvalidAddress       Status = "invalid_address"
	StatusDuplicateHost        Status = "duplicate_host"
	StatusUnsupportedVendor    Status = "unsupported_vendor"
	StatusUnsupportedOperation Status = "unsupported_operation"
	StatusFilenameCollision    Status = "remote_filename_collision"
	StatusMissingScript        Status = "missing_script"
	StatusSaveUnsupported      Status = "save_unsupported"
	StatusNotReadOnly          Status = "not_read_only"
	StatusConfirmUnsupported   Status = "commit_confirm_unsupported"
)

// Outcome names what a finished device achieved, such as "backed_up".
type Outcome string

// Device outcomes.
const (
	OutcomeBackedUp           Outcome = "backed_up"
	OutcomeUploaded           Outcome = "uploaded"
	OutcomeExported           Outcome = "exported"
	OutcomeExecuted           Outcome = "executed"
	OutcomeCollected          Outcome = "collected"
	OutcomeMismatch           Outcome = "inventory_mismatch"
	OutcomeDeployed           Outcome = "deployed"
	OutcomeRejected           Outcome = "rejected"
	OutcomeCandidateDirty     Outcome = "candidate_dirty"
	OutcomeDiscovered         Outcome = "discovered"
	OutcomePlanned            Outcome = "planned"
	OutcomePartialPlanned     Outcome = "partial_planned"
	OutcomeUnverified         Outcome = "unverified"
	OutcomeManualRequired     Outcome = "manual_required"
	OutcomeUnchanged          Outcome = "unchanged"
	OutcomeApplied            Outcome = "applied"
	OutcomePartialApplied     Outcome = "partial_applied"
	OutcomePersistedWithError Outcome = "persisted_with_error"
)

// Stage status values.
const (
	StageSucceeded = "succeeded"
	StageFailed    = "failed"
	StageSkipped   = "skipped"
)

// BatchStatus is the state of a whole batch.
type BatchStatus string

// Batch status values.
const (
	BatchSucceeded   BatchStatus = "succeeded"
	BatchIncomplete  BatchStatus = "incomplete"
	BatchInterrupted BatchStatus = "interrupted"
	BatchNoDevices   BatchStatus = "no_devices"
	BatchPlanned     BatchStatus = "planned"
)

// Report is report.json for one batch directory.
type Report struct {
	SchemaVersion int            `json:"schema_version"`
	Operation     string         `json:"operation"`
	Policy        string         `json:"policy"`
	Success       bool           `json:"success"`
	Status        BatchStatus    `json:"status"`
	Directory     string         `json:"directory"`
	StartedAt     string         `json:"started_at"`
	FinishedAt    string         `json:"finished_at"`
	DurationMS    float64        `json:"duration_ms"`
	Workers       int            `json:"workers"`
	Total         int            `json:"total"`
	Counts        map[Status]int `json:"counts"`
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
	Status       Status      `json:"status"`
	Outcome      Outcome     `json:"outcome,omitempty"`
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
	Commit   *Commit         `json:"commit,omitempty"`
	Outputs  []Output        `json:"outputs,omitempty"`
	Change   *Change         `json:"change,omitempty"`
	Facts    *Facts          `json:"facts,omitempty"`
	// Topology carries the audit, plan and apply evidence of a topology run.
	Topology any `json:"topology,omitempty"`
}

// FileReceipt records how far a local file publication completed.
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

// SourceSegment locates one accepted command response in the saved source.
type SourceSegment struct {
	Step   int `json:"step"`
	Offset int `json:"offset"`
	Bytes  int `json:"bytes"`
}

// BackupSource identifies the raw response and its accepted segments.
type BackupSource struct {
	Path            string          `json:"path"`
	Bytes           int             `json:"bytes"`
	SHA256          string          `json:"sha256"`
	File            FileReceipt     `json:"file"`
	NoWriteVerified bool            `json:"no_write_verified"`
	Segments        []SourceSegment `json:"segments"`
}

// TextPosition locates a decoding warning in the source text.
type TextPosition struct {
	Offset int `json:"offset"`
	Line   int `json:"line"`
	Column int `json:"column"`
}

// TextWarning counts decoding warnings and their bounded source positions.
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

// ArchiveProof records the verified identity of an archived upload.
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

// Attempt preserves the write progress of one script command.
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

// Snapshot identifies one configuration collected around a change.
type Snapshot struct {
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

// Change is the configuration before and after a deployment. Compared is set
// once both snapshots were diffed; Changed, Added and Removed describe that
// diff, and DiffFile is written only when the configuration changed.
type Change struct {
	Before   *Snapshot `json:"before,omitempty"`
	After    *Snapshot `json:"after,omitempty"`
	Compared bool      `json:"compared"`
	Changed  bool      `json:"changed"`
	Added    int       `json:"added"`
	Removed  int       `json:"removed"`
	DiffFile string    `json:"diff_file,omitempty"`
}

// Commit records a candidate-configuration commit; Confirmed requires the
// platform's positive evidence. ConfirmMinutes is set for a confirmed commit.
type Commit struct {
	Confirmed      bool `json:"confirmed"`
	ConfirmMinutes int  `json:"confirm_minutes,omitempty"`
}

// Facts is what a device reported about itself. Mismatches names the
// inventory fields (model, serial, os_version) that disagree with it.
type Facts struct {
	Hostname   string   `json:"hostname,omitempty"`
	Model      string   `json:"model,omitempty"`
	Serial     string   `json:"serial,omitempty"`
	OSVersion  string   `json:"os_version"`
	Uptime     string   `json:"uptime,omitempty"`
	Mismatches []string `json:"mismatches"`
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
	Status       Status
}

// NormalizeHost returns a canonical, unscoped IP address for artifact lookup.
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
