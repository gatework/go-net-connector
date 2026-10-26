package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/gatework/go-net-connector/netdisco"
)

func (r Runtime) preview(plan netdisco.Plan, view map[string]any, o options, program string) int {
	if program == "net-backup" {
		return r.print(view)
	}
	if o.json {
		if code := r.print(map[string]any{"plan": view}); code != 0 {
			return code
		}
	} else {
		for _, task := range plan.Tasks() {
			name := strings.Map(func(c rune) rune {
				if c < ' ' || c == 127 {
					return ' '
				}
				return c
			}, task.Device.Name)
			if _, e := fmt.Fprintf(r.Output, "%s\t%s\t%s\n", task.Device.Host, name, task.Device.Vendor); e != nil {
				return 2
			}
		}
	}
	if len(plan.Tasks()) == 0 {
		return 1
	}
	return 0
}
func (r Runtime) finishReport(report netdisco.Report, directory string, o options, program string, interrupted bool) error {
	if program == "net-backup" {
		return writeReportOutput(r.Output, report)
	}
	if !o.json {
		return nil
	}
	summary := report.Statistics()
	exit := 1
	if report.Success() {
		exit = 0
	}
	if interrupted {
		exit = 130
	}
	return writeReportOutput(r.Output, map[string]any{"directory": directory, "counts": summary["counts"], "tasks_succeeded": report.Success(), "success": report.Success(), "policy": o.policy, "policy_success": report.Success(), "exit_status": exit, "report_location": summary["report_location"], "report_error": summary["report_error"]})
}
func writeReportOutput(output io.Writer, value any) error {
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
func artifactView(a netdisco.Artifact) any {
	if b := a.Backup; b != nil {
		return map[string]any{"path": b.Path, "bytes": b.Bytes, "sha256": b.SHA256, "collected_at": b.CollectedAt, "change": b.Change, "previous_sha256": nullable(b.PreviousSHA256)}
	}
	if t := a.Transfer; t != nil {
		return map[string]any{"server": t.Server, "path": nullable(t.Path), "requested_path": t.RequestedPath, "completed_at": t.CompletedAt, "configuration_kind": t.ConfigurationKind, "source_file": nullable(t.SourceFile), "format": t.Format, "verification": t.Verification, "server_sha256": nullable(t.ServerSHA256), "server_bytes": t.ServerBytes, "local_file": nullable(t.LocalPath), "server_archive_file": nullable(t.ArchivePath)}
	}
	return nil
}
