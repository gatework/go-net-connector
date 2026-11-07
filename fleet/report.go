package fleet

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"time"

	"github.com/gatework/go-net-connector/archive"
	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/internal/batchreport"
)

type Report struct {
	batch                Batch
	policy, logDirectory string
	diagnostic           Diagnostic
}

func NewReport(b Batch, policy string) (Report, error) {
	if policy != "strict" && policy != "selected" && policy != "verified" || policy == "verified" && b.mode != modeTFTP {
		return Report{}, ErrInvalidPlan
	}
	b.outcomes = b.Outcomes()
	b.callbackErrors = b.CallbackErrors()
	return Report{batch: b, policy: policy}, nil
}

func (r Report) WithLogDirectory(path string) Report { r.logDirectory = path; return r }

func (r Report) WithReportError(err error, location string) Report {
	r.batch.reportLocation = location
	r.diagnostic = diagnoseError(err, Artifact{})
	r.diagnostic.Phase = "report"
	r.batch.reportError = r.diagnostic.ErrorType
	return r
}

func (r Report) Success() bool {
	b := r.batch
	if r.policy == "strict" {
		return b.Success()
	}
	if b.interrupted || len(b.callbackErrors) > 0 || b.reportError != "" {
		return false
	}
	success := false
	for _, o := range b.outcomes {
		if o.Success() {
			success = true
			if r.policy == "verified" && (o.Artifact.Archive == nil || !o.Artifact.Archive.Complete()) {
				return false
			}
		} else if outcomeStatus(o.Status) != outcomeStatusFiltered && outcomeStatus(o.Status) != outcomeStatusSampleLimit {
			return false
		}
	}
	return success
}

type reportTotals struct {
	succeeded, textWarnings, partial, failed, attempted, skipped int
	counts                                                       map[string]int
	verification                                                 *batchreport.Verification
	callbacks                                                    []CallbackError
	durationMS                                                   *float64
	reportDiagnostic                                             *batchreport.ReportDiagnostic
}

func (r Report) totals() reportTotals {
	b := r.batch
	counts := b.Counts()
	totals := reportTotals{
		counts:    counts,
		succeeded: counts[string(outcomeStatusBackedUp)] + counts[string(outcomeStatusReportedUploaded)],
		partial:   counts[string(outcomeStatusSavedWithError)] + counts[string(outcomeStatusReportedWithError)],
		failed:    counts[string(outcomeStatusFailed)] + counts[string(outcomeStatusInterrupted)],
		callbacks: make([]CallbackError, len(b.callbackErrors)),
	}
	for _, outcome := range b.outcomes {
		if receipt := outcome.Artifact.Backup; receipt != nil && receipt.TextWarning != nil && receipt.TextWarning.Count > 0 {
			totals.textWarnings++
		}
	}
	totals.attempted = totals.succeeded + totals.partial + totals.failed
	totals.skipped = len(b.outcomes) - totals.attempted
	if b.mode == modeTFTP {
		verification := &batchreport.Verification{}
		for _, outcome := range b.outcomes {
			if outcome.Artifact.Transfer == nil {
				continue
			}
			if outcome.Artifact.Archive != nil && outcome.Artifact.Archive.Verified() {
				verification.Verified++
			} else {
				verification.Unverified++
			}
		}
		totals.verification = verification
	}
	for i, callback := range b.callbackErrors {
		errorType := callback.ErrorType
		if !knownTypes[errorType] {
			errorType = "StandardError"
		}
		totals.callbacks[i] = CallbackError{Host: callback.Host, ErrorType: errorType}
	}
	if !b.startedAt.IsZero() && !b.finishedAt.IsZero() {
		duration := float64(b.finishedAt.Sub(b.startedAt)) / float64(time.Millisecond)
		totals.durationMS = &duration
	}
	if b.reportError != "" {
		diagnostic := r.diagnostic.sanitized()
		totals.reportDiagnostic = &batchreport.ReportDiagnostic{
			ErrorCode: optionalString(diagnostic.ErrorCode), ErrorType: optionalString(diagnostic.ErrorType),
			Phase: optionalString(diagnostic.Phase), Underlying: optionalString(diagnostic.UnderlyingType),
			ArtifactState: optionalString(diagnostic.ArtifactState), ArtifactPhase: optionalString(diagnostic.ArtifactPhase),
			Verification: optionalString(diagnostic.Verification),
		}
	}
	return totals
}

// Statistics returns the stable generic view consumed by CLI renderers.
func (r Report) Statistics() map[string]any {
	totals := r.totals()
	var verification any
	if totals.verification != nil {
		verification = map[string]int{"verified": totals.verification.Verified, "unverified": totals.verification.Unverified}
	}
	var reportDiagnostic any
	if diagnostic := totals.reportDiagnostic; diagnostic != nil {
		reportDiagnostic = map[string]any{
			"error_code": nullableString(optionalStringValue(diagnostic.ErrorCode)), "error_type": nullableString(optionalStringValue(diagnostic.ErrorType)),
			"phase": nullableString(optionalStringValue(diagnostic.Phase)), "underlying_type": nullableString(optionalStringValue(diagnostic.Underlying)),
			"artifact_state": nullableString(optionalStringValue(diagnostic.ArtifactState)), "artifact_phase": nullableString(optionalStringValue(diagnostic.ArtifactPhase)),
			"verification": nullableString(optionalStringValue(diagnostic.Verification)),
		}
	}
	var duration any
	if totals.durationMS != nil {
		duration = *totals.durationMS
	}
	return map[string]any{
		"schema_version":       batchreport.SchemaVersion,
		"policy":               r.policy,
		"policy_success":       r.Success(),
		"duration_ms":          duration,
		"mode":                 string(r.batch.mode),
		"status":               r.batch.Status(),
		"started_at":           nullableString(timestampString(r.batch.startedAt)),
		"finished_at":          nullableString(timestampString(r.batch.finishedAt)),
		"total":                len(r.batch.outcomes),
		"succeeded":            totals.succeeded,
		"text_warning_devices": totals.textWarnings,
		"partial":              totals.partial,
		"failed":               totals.failed,
		"skipped":              totals.skipped,
		"counts":               totals.counts,
		"coverage": map[string]any{
			"complete":  len(r.batch.outcomes) > 0 && totals.attempted == len(r.batch.outcomes),
			"attempted": totals.attempted,
			"skipped":   totals.skipped,
		},
		"callback_errors":   totals.callbacks,
		"report_location":   nullableString(r.batch.reportLocation),
		"report_error":      nullableString(r.batch.reportError),
		"verification":      verification,
		"report_diagnostic": reportDiagnostic,
	}
}

func (r Report) document() batchreport.Summary {
	totals := r.totals()
	callbacks := make([]batchreport.CallbackError, len(totals.callbacks))
	for i, callback := range totals.callbacks {
		callbacks[i] = batchreport.CallbackError{Host: callback.Host, ErrorType: callback.ErrorType}
	}
	return batchreport.Summary{
		SchemaVersion: batchreport.SchemaVersion,
		Policy:        r.policy, PolicySuccess: r.Success(), DurationMS: totals.durationMS,
		Mode: string(r.batch.mode), Status: r.batch.Status(),
		StartedAt: timestampPointer(r.batch.startedAt), FinishedAt: timestampPointer(r.batch.finishedAt),
		Total: len(r.batch.outcomes), Succeeded: totals.succeeded, TextWarningDevices: totals.textWarnings,
		Partial: totals.partial, Failed: totals.failed, Skipped: totals.skipped, Counts: totals.counts,
		Coverage: batchreport.Coverage{
			Complete:  len(r.batch.outcomes) > 0 && totals.attempted == len(r.batch.outcomes),
			Attempted: totals.attempted, Skipped: totals.skipped,
		},
		CallbackErrors: callbacks,
		ReportLocation: optionalString(r.batch.reportLocation), ReportError: optionalString(r.batch.reportError),
		Verification: totals.verification, ReportDiagnostic: totals.reportDiagnostic,
		Devices: r.devices(),
	}
}

func (r Report) devices() []batchreport.Device {
	devices := make([]batchreport.Device, 0, len(r.batch.outcomes))
	for _, outcome := range r.batch.outcomes {
		diagnostic := outcome.Diagnostic
		if diagnostic.ErrorType == "" {
			diagnostic.ErrorCode, diagnostic.ErrorType = outcome.ErrorCode, outcome.ErrorType
		}
		diagnostic = diagnostic.sanitized()
		host := outcome.Device.Host
		if host == "" {
			host = outcome.Device.SourceIP
		}
		device := batchreport.Device{
			Mode: string(r.batch.mode),
			Host: host, Name: optionalString(outcome.Device.Name), Vendor: optionalString(outcome.Device.Vendor),
			Status:    outcome.Status,
			StartedAt: timestampPointer(outcome.StartedAt), FinishedAt: timestampPointer(outcome.FinishedAt),
			ErrorCode: optionalString(diagnostic.ErrorCode), ErrorType: optionalString(diagnostic.ErrorType),
			Diagnostic: batchreport.Diagnostic{
				Phase: optionalString(diagnostic.Phase), Underlying: optionalString(diagnostic.UnderlyingType),
				ArtifactState: optionalString(diagnostic.ArtifactState), ArtifactPhase: optionalString(diagnostic.ArtifactPhase),
				Verification: optionalString(diagnostic.Verification),
			},
		}
		if !outcome.StartedAt.IsZero() {
			duration := float64(outcome.Duration) / float64(time.Millisecond)
			device.DurationMS = &duration
		}
		if receipt := outcome.Artifact.Backup; receipt != nil {
			device.HasBackup = true
			device.Path = stringPointer(receipt.Path)
			device.Bytes = int64Pointer(int64(receipt.Bytes))
			device.SHA256 = stringPointer(receipt.SHA256)
			device.Change = stringPointer(receipt.Change)
			device.PreviousSHA256 = optionalString(receipt.PreviousSHA256)
			device.Source = reportBackupSource(receipt.Source)
			device.Encoding = optionalString(receipt.Encoding)
			device.TextWarning = reportTextWarning(receipt.TextWarning)
		}
		if r.batch.mode == modeTFTP {
			device.RemotePath = device.Path
			device.ServerFileVerified = false
			if receipt := outcome.Artifact.Transfer; receipt != nil {
				device.Path = stringPointer(receipt.Path)
				device.RemotePath = stringPointer(receipt.Path)
				device.Server = stringPointer(receipt.Server)
				device.ConfigurationKind = optionalString(receipt.ConfigurationKind)
				device.SourceFile = optionalString(receipt.SourceFile)
				device.Format = stringPointer(receipt.Format)
				device.Verification = optionalString(string(receipt.Verification))
			}
			if receipt := outcome.Artifact.Archive; receipt != nil {
				device.Archive = reportArchiveReceipt(*receipt)
				device.ServerFileVerified = receipt.Verified()
				if receipt.Verified() {
					device.Verification = stringPointer("server_verified")
					device.ServerSHA256 = stringPointer(receipt.Server.SHA256)
					device.SHA256 = device.ServerSHA256
					device.Bytes = int64Pointer(receipt.Server.Bytes)
				}
				if receipt.Server != nil {
					device.ServerArchiveFile = optionalString(receipt.Server.ArchivePath)
				}
				if receipt.Local.Committed() {
					device.LocalFile = optionalString(receipt.Local.Path)
				}
			}
		}
		if r.logDirectory != "" && outcome.Device.Host != "" && !outcome.StartedAt.IsZero() {
			path := filepath.Join(r.logDirectory, strings.ReplaceAll(outcome.Device.Host, ":", "_")+".log")
			device.SessionLog = &path
		}
		devices = append(devices, device)
	}
	return devices
}

func reportArchiveReceipt(receipt archive.Receipt) *batchreport.ArchiveReceipt {
	r := &batchreport.ArchiveReceipt{
		Local:   batchreport.FileReceipt{Path: receipt.Local.Path, State: string(receipt.Local.State), Phase: receipt.Local.Phase},
		Cleanup: string(receipt.Cleanup),
	}
	if proof := receipt.Server; proof != nil {
		r.Server = &batchreport.ArchiveProof{Bytes: proof.Bytes, SHA256: proof.SHA256, ArchivePath: proof.ArchivePath, Verified: proof.Verified}
	}
	return r
}

func reportBackupSource(source *connector.BackupSource) *batchreport.BackupSource {
	if source == nil {
		return nil
	}
	report := &batchreport.BackupSource{
		Path: source.Path, Bytes: source.Bytes, SHA256: source.SHA256,
		File:            batchreport.FileReceipt{Path: source.File.Path, State: string(source.File.State), Phase: source.File.Phase},
		NoWriteVerified: source.NoWriteVerified,
	}
	if source.Segments != nil {
		report.Segments = make([]batchreport.SourceSegment, len(source.Segments))
		for i, segment := range source.Segments {
			report.Segments[i] = batchreport.SourceSegment{Step: segment.Step, Offset: segment.Offset, Bytes: segment.Bytes}
		}
	}
	return report
}

func reportTextWarning(warning *connector.TextWarning) *batchreport.TextWarning {
	if warning == nil {
		return nil
	}
	report := &batchreport.TextWarning{Count: warning.Count}
	if warning.Positions != nil {
		report.Positions = make([]batchreport.TextPosition, len(warning.Positions))
		for i, position := range warning.Positions {
			report.Positions[i] = batchreport.TextPosition{Offset: position.Offset, Line: position.Line, Column: position.Column}
		}
	}
	return report
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func stringPointer(value string) *string { return &value }

func int64Pointer(value int64) *int64 { return &value }

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func timestampPointer(value time.Time) *string {
	formatted := timestampString(value)
	if formatted == "" {
		return nil
	}
	return &formatted
}

func timestampString(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339)
}

func optionalStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func (r Report) Summary() map[string]any {
	values := r.document().Map()
	if r.batch.mode != modeBackup {
		return values
	}
	rows, ok := values["devices"].([]map[string]any)
	if !ok {
		return values
	}
	for i, outcome := range r.batch.outcomes {
		if i >= len(rows) || outcome.Artifact.Backup == nil {
			continue
		}
		metadata := outcome.Artifact.Backup.Clone()
		rows[i]["source"] = metadata.Source
		rows[i]["text_warning"] = metadata.TextWarning
	}
	return values
}

func (r Report) MarshalJSON() ([]byte, error) { return json.Marshal(r.document()) }
