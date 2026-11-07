// Package batchreport owns the persisted batch summary contract used by Fleet
// and by safe artifact export.
package batchreport

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/netip"
)

const SchemaVersion = 2
const MaxArtifactBytes int64 = 64 << 20

var ErrInvalidEvidence = errors.New("batchreport: invalid artifact evidence")

// Summary is the schema-v2 batch report written by Fleet and read by export.
type Summary struct {
	SchemaVersion      int               `json:"schema_version"`
	Policy             string            `json:"policy"`
	PolicySuccess      bool              `json:"policy_success"`
	DurationMS         *float64          `json:"duration_ms"`
	Mode               string            `json:"mode"`
	Status             string            `json:"status"`
	StartedAt          *string           `json:"started_at"`
	FinishedAt         *string           `json:"finished_at"`
	Total              int               `json:"total"`
	Succeeded          int               `json:"succeeded"`
	TextWarningDevices int               `json:"text_warning_devices"`
	Partial            int               `json:"partial"`
	Failed             int               `json:"failed"`
	Skipped            int               `json:"skipped"`
	Counts             map[string]int    `json:"counts"`
	Coverage           Coverage          `json:"coverage"`
	CallbackErrors     []CallbackError   `json:"callback_errors"`
	ReportLocation     *string           `json:"report_location"`
	ReportError        *string           `json:"report_error"`
	Verification       *Verification     `json:"verification"`
	ReportDiagnostic   *ReportDiagnostic `json:"report_diagnostic"`
	Devices            []Device          `json:"devices"`
}

// Map returns the established generic view used by CLI renderers and callers.
func (s Summary) Map() map[string]any {
	var verification any
	if s.Verification != nil {
		verification = map[string]int{"verified": s.Verification.Verified, "unverified": s.Verification.Unverified}
	}
	var reportDiagnostic any
	if s.ReportDiagnostic != nil {
		reportDiagnostic = s.ReportDiagnostic.values()
	}
	devices := make([]map[string]any, len(s.Devices))
	for i, device := range s.Devices {
		devices[i] = device.values()
	}
	return map[string]any{
		"schema_version": s.SchemaVersion, "policy": s.Policy, "policy_success": s.PolicySuccess,
		"duration_ms": floatValue(s.DurationMS), "mode": s.Mode, "status": s.Status,
		"started_at": stringValue(s.StartedAt), "finished_at": stringValue(s.FinishedAt),
		"total": s.Total, "succeeded": s.Succeeded, "text_warning_devices": s.TextWarningDevices,
		"partial": s.Partial, "failed": s.Failed, "skipped": s.Skipped, "counts": s.Counts,
		"coverage":        map[string]any{"complete": s.Coverage.Complete, "attempted": s.Coverage.Attempted, "skipped": s.Coverage.Skipped},
		"callback_errors": s.CallbackErrors, "report_location": stringValue(s.ReportLocation),
		"report_error": stringValue(s.ReportError), "verification": verification,
		"report_diagnostic": reportDiagnostic, "devices": devices,
	}
}

type Coverage struct {
	Complete  bool `json:"complete"`
	Attempted int  `json:"attempted"`
	Skipped   int  `json:"skipped"`
}

type CallbackError struct {
	Host      string `json:"host"`
	ErrorType string `json:"error_type"`
}

type Verification struct {
	Verified   int `json:"verified"`
	Unverified int `json:"unverified"`
}

type Diagnostic struct {
	Phase         *string `json:"phase"`
	Underlying    *string `json:"underlying_type"`
	ArtifactState *string `json:"artifact_state"`
	ArtifactPhase *string `json:"artifact_phase"`
	Verification  *string `json:"verification"`
}

type ReportDiagnostic struct {
	ErrorCode     *string `json:"error_code"`
	ErrorType     *string `json:"error_type"`
	Phase         *string `json:"phase"`
	Underlying    *string `json:"underlying_type"`
	ArtifactState *string `json:"artifact_state"`
	ArtifactPhase *string `json:"artifact_phase"`
	Verification  *string `json:"verification"`
}

// BackupSource and TextWarning are report-owned representations of the
// evidence nested in a configuration backup row. Keeping these shapes here
// makes the persisted contract explicit without coupling it to connector or
// storage implementation types.
type FileReceipt struct {
	Path  string `json:"path"`
	State string `json:"state"`
	Phase string `json:"phase"`
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

// Device is a report row. Mode and HasBackup control mode-specific fields but
// are not serialized, keeping the established schema-v2 shape intact.
type Device struct {
	Mode               string        `json:"-"`
	HasBackup          bool          `json:"-"`
	Host               string        `json:"host"`
	Name               *string       `json:"name"`
	Vendor             *string       `json:"vendor"`
	Status             string        `json:"status"`
	Path               *string       `json:"path"`
	Bytes              *int64        `json:"bytes"`
	SHA256             *string       `json:"sha256"`
	Change             *string       `json:"change"`
	PreviousSHA256     *string       `json:"previous_sha256"`
	StartedAt          *string       `json:"started_at"`
	FinishedAt         *string       `json:"finished_at"`
	DurationMS         *float64      `json:"duration_ms"`
	ErrorCode          *string       `json:"error_code"`
	ErrorType          *string       `json:"error_type"`
	Diagnostic         Diagnostic    `json:"diagnostic"`
	Source             *BackupSource `json:"source"`
	Encoding           *string       `json:"encoding"`
	TextWarning        *TextWarning  `json:"text_warning"`
	RemotePath         *string       `json:"remote_path"`
	Server             *string       `json:"server"`
	ConfigurationKind  *string       `json:"configuration_kind"`
	SourceFile         *string       `json:"source_file"`
	Format             *string       `json:"format"`
	Verification       *string       `json:"verification"`
	ServerSHA256       *string       `json:"server_sha256"`
	LocalFile          *string       `json:"local_file"`
	ServerArchiveFile  *string       `json:"server_archive_file"`
	ServerFileVerified bool          `json:"server_file_verified"`
	SessionLog         *string       `json:"session_log"`
}

func (d Device) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.values())
}

func (d Device) values() map[string]any {
	fields := map[string]any{
		"host": d.Host, "name": d.Name, "vendor": d.Vendor, "status": d.Status,
		"path": stringValue(d.Path), "bytes": int64Value(d.Bytes), "sha256": stringValue(d.SHA256),
		"change": stringValue(d.Change), "previous_sha256": stringValue(d.PreviousSHA256),
		"started_at": stringValue(d.StartedAt), "finished_at": stringValue(d.FinishedAt),
		"duration_ms": floatValue(d.DurationMS), "error_code": stringValue(d.ErrorCode),
		"error_type": stringValue(d.ErrorType), "diagnostic": d.Diagnostic.values(),
	}
	if d.Mode == "tftp" {
		fields["remote_path"] = stringValue(d.RemotePath)
		fields["server"] = stringValue(d.Server)
		fields["configuration_kind"] = stringValue(d.ConfigurationKind)
		fields["source_file"] = stringValue(d.SourceFile)
		fields["format"] = stringValue(d.Format)
		fields["verification"] = stringValue(d.Verification)
		fields["server_sha256"] = stringValue(d.ServerSHA256)
		fields["local_file"] = stringValue(d.LocalFile)
		fields["server_archive_file"] = stringValue(d.ServerArchiveFile)
		fields["server_file_verified"] = d.ServerFileVerified
	} else if d.HasBackup {
		fields["source"] = d.Source
		fields["encoding"] = stringValue(d.Encoding)
		fields["text_warning"] = d.TextWarning
	}
	if d.SessionLog != nil {
		fields["session_log"] = *d.SessionLog
	}
	return fields
}

func (d Diagnostic) values() map[string]any {
	return map[string]any{
		"phase": stringValue(d.Phase), "underlying_type": stringValue(d.Underlying),
		"artifact_state": stringValue(d.ArtifactState), "artifact_phase": stringValue(d.ArtifactPhase),
		"verification": stringValue(d.Verification),
	}
}

func (d ReportDiagnostic) values() map[string]any {
	values := Diagnostic{Phase: d.Phase, Underlying: d.Underlying, ArtifactState: d.ArtifactState, ArtifactPhase: d.ArtifactPhase, Verification: d.Verification}.values()
	values["error_code"], values["error_type"] = stringValue(d.ErrorCode), stringValue(d.ErrorType)
	return values
}

func stringValue(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func int64Value(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func floatValue(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

type Artifact struct {
	Host   string
	Path   string
	Bytes  int64
	SHA256 string
	Status string
}

func NormalizeHost(host string) (string, error) {
	address, err := netip.ParseAddr(host)
	if err != nil || address.Zone() != "" {
		return "", ErrInvalidEvidence
	}
	return address.String(), nil
}

// SelectArtifact validates the report contract and returns the unique saved
// artifact whose IP address matches host. IPv4-mapped IPv6 retains its family.
func SelectArtifact(data []byte, host string) (Artifact, error) {
	canonicalHost, err := NormalizeHost(host)
	if err != nil {
		return Artifact{}, ErrInvalidEvidence
	}
	address, _ := netip.ParseAddr(canonicalHost)
	var summary Summary
	if json.Unmarshal(data, &summary) != nil || summary.SchemaVersion != SchemaVersion || (summary.Mode != "backup" && summary.Mode != "tftp") {
		return Artifact{}, ErrInvalidEvidence
	}
	var selected *Device
	for i := range summary.Devices {
		candidate := &summary.Devices[i]
		a, parseErr := netip.ParseAddr(candidate.Host)
		if parseErr == nil && a.Zone() == "" && a == address {
			if selected != nil {
				return Artifact{}, ErrInvalidEvidence
			}
			selected = candidate
		}
	}
	if selected == nil || selected.Bytes == nil || *selected.Bytes < 0 || *selected.Bytes > MaxArtifactBytes || selected.SHA256 == nil {
		return Artifact{}, ErrInvalidEvidence
	}
	checksum, err := hex.DecodeString(*selected.SHA256)
	if err != nil || len(checksum) != 32 {
		return Artifact{}, ErrInvalidEvidence
	}
	path := selected.Path
	if summary.Mode == "tftp" {
		if selected.Verification == nil || *selected.Verification != "server_verified" {
			return Artifact{}, ErrInvalidEvidence
		}
		path = selected.LocalFile
	}
	if path == nil || *path == "" {
		return Artifact{}, ErrInvalidEvidence
	}
	return Artifact{
		Host:   address.String(),
		Path:   *path,
		Bytes:  *selected.Bytes,
		SHA256: *selected.SHA256,
		Status: selected.Status,
	}, nil
}
