package fleet

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"time"
)

type Report struct {
	batch                Batch
	policy, logDirectory string
	diagnostic           Diagnostic
}

func NewReport(b Batch, policy string) (Report, error) {
	if policy != "strict" && policy != "selected" && policy != "verified" || policy == "verified" && b.mode != "tftp" {
		return Report{}, ErrPlan
	}
	b.outcomes = b.Outcomes()
	b.callbackErrors = b.CallbackErrors()
	return Report{batch: b, policy: policy}, nil
}
func (r Report) WithLogDirectory(path string) Report { r.logDirectory = path; return r }
func (r Report) WithReportError(err error, location string) Report {
	r.batch.reportLocation = location
	r.diagnostic = diagnose(err, Artifact{})
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
			if r.policy == "verified" && (o.Artifact.Transfer == nil || !o.Artifact.Transfer.Verified()) {
				return false
			}
		} else if o.Status != "filtered" && o.Status != "sample_limit" {
			return false
		}
	}
	return success
}
func optional(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func timestamp(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.Format(time.RFC3339)
}
func (r Report) Statistics() map[string]any {
	b := r.batch
	counts := b.Counts()
	success := counts["backed_up"] + counts["reported_uploaded"]
	partial := counts["saved_with_error"] + counts["reported_with_error"]
	failed := counts["failed"] + counts["interrupted"]
	attempted := success + partial + failed
	var verification any
	if b.mode == "tftp" {
		v, u := 0, 0
		for _, o := range b.outcomes {
			if o.Artifact.Transfer != nil {
				if o.Artifact.Transfer.Verified() {
					v++
				} else {
					u++
				}
			}
		}
		verification = map[string]int{"verified": v, "unverified": u}
	}
	callbacks := make([]CallbackError, len(b.callbackErrors))
	copy(callbacks, b.callbackErrors)
	for i := range callbacks {
		if !knownTypes[callbacks[i].ErrorType] {
			callbacks[i].ErrorType = "StandardError"
		}
	}
	var duration any
	if !b.startedAt.IsZero() && !b.finishedAt.IsZero() {
		duration = float64(b.finishedAt.Sub(b.startedAt)) / float64(time.Millisecond)
	}
	var diagnostic any
	if b.reportError != "" {
		d := r.diagnostic.safe()
		m := d.summary()
		m["error_code"], m["error_type"] = optional(d.ErrorCode), optional(d.ErrorType)
		diagnostic = m
	}
	return map[string]any{"schema_version": 2, "policy": r.policy, "policy_success": r.Success(), "duration_ms": duration, "mode": b.mode, "status": b.Status(), "started_at": timestamp(b.startedAt), "finished_at": timestamp(b.finishedAt), "total": len(b.outcomes), "succeeded": success, "partial": partial, "failed": failed, "skipped": len(b.outcomes) - attempted, "counts": counts, "coverage": map[string]any{"complete": len(b.outcomes) > 0 && attempted == len(b.outcomes), "attempted": attempted, "skipped": len(b.outcomes) - attempted}, "callback_errors": callbacks, "report_location": optional(b.reportLocation), "report_error": optional(b.reportError), "verification": verification, "report_diagnostic": diagnostic}
}
func (r Report) Summary() map[string]any {
	s := r.Statistics()
	rows := make([]map[string]any, 0, len(r.batch.outcomes))
	for _, o := range r.batch.outcomes {
		d := o.Diagnostic
		if d.ErrorType == "" {
			d.ErrorCode, d.ErrorType = o.ErrorCode, o.ErrorType
		}
		d = d.safe()
		host := o.Device.Host
		if host == "" {
			host = o.Device.SourceIP
		}
		var duration any
		if !o.StartedAt.IsZero() {
			duration = float64(o.Duration) / float64(time.Millisecond)
		}
		row := map[string]any{"host": host, "name": optional(o.Device.Name), "vendor": optional(o.Device.Vendor), "status": o.Status, "path": nil, "bytes": nil, "sha256": nil, "change": nil, "previous_sha256": nil, "started_at": timestamp(o.StartedAt), "finished_at": timestamp(o.FinishedAt), "duration_ms": duration, "error_code": optional(d.ErrorCode), "error_type": optional(d.ErrorType), "diagnostic": d.summary()}
		if a := o.Artifact.Backup; a != nil {
			row["path"], row["bytes"], row["sha256"], row["change"], row["previous_sha256"] = a.Path, a.Bytes, a.SHA256, a.Change, optional(a.PreviousSHA256)
		}
		if r.batch.mode == "tftp" {
			for _, key := range []string{"remote_path", "server", "configuration_kind", "source_file", "format", "verification", "server_sha256", "local_file", "server_archive_file", "bytes", "sha256"} {
				row[key] = nil
			}
			row["server_file_verified"] = false
			if a := o.Artifact.Transfer; a != nil {
				row["path"] = a.Path
				row["remote_path"] = a.Path
				row["server"] = a.Server
				row["configuration_kind"] = optional(a.ConfigurationKind)
				row["source_file"] = optional(a.SourceFile)
				row["format"] = optional(a.Format)
				row["verification"] = optional(string(a.Verification))
				row["server_file_verified"] = a.Verified()
				row["server_sha256"], row["sha256"] = optional(a.ServerSHA256), optional(a.ServerSHA256)
				row["local_file"], row["server_archive_file"] = optional(a.LocalPath), optional(a.ArchivePath)
				if a.Verified() {
					row["bytes"] = a.ServerBytes
				}
			}
		}
		if r.logDirectory != "" && o.Device.Host != "" && !o.StartedAt.IsZero() {
			row["session_log"] = filepath.Join(r.logDirectory, strings.ReplaceAll(o.Device.Host, ":", "_")+".log")
		}
		rows = append(rows, row)
	}
	s["devices"] = rows
	return s
}
func (r Report) MarshalJSON() ([]byte, error) { return json.Marshal(r.Summary()) }
