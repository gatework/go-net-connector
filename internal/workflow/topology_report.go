package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/gatework/go-net-connector/internal/topologyreport"
	"github.com/gatework/go-net-connector/storage"
	"github.com/gatework/go-net-connector/topology"
	"github.com/gatework/go-net-connector/vendors/dialect"
)

func saveTopologyPlan(ctx context.Context, directory string, outcomes []topologyOutcome, registry dialect.Registry) error {
	plans := []map[string]any{}
	audits := []map[string]any{}
	for i := range outcomes {
		out := &outcomes[i]
		name, e := out.device.BackupFilename("hostname_ip")
		if e != nil {
			return e
		}
		stem := strings.TrimSuffix(name, ".txt")
		out.named = true
		reportPath := filepath.Join(directory, "devices", stem+".txt")
		if out.state == topologyNotStarted {
			out.deviceReport = ""
		} else {
			out.deviceReport = reportPath
		}
		if out.plan != nil {
			p := out.plan
			if e = topology.ValidatePlan(registry, *p, out.device.Host, out.device.Vendor); e != nil {
				return e
			}
			var scriptPath, digest any
			if len(p.Commands) > 0 {
				path := filepath.Join(directory, "scripts", stem+".cfg")
				body := strings.Join(p.Commands, "\n") + "\n"
				if e = storage.PrepareDirectory(filepath.Dir(path)); e != nil {
					return e
				}
				if _, e = storage.Write(ctx, path, []byte(body)); e != nil {
					return e
				}
				out.scriptFile, out.scriptSHA256 = path, fmt.Sprintf("%x", sha256.Sum256([]byte(body)))
				scriptPath, digest = out.scriptFile, out.scriptSHA256
			}
			plans = append(plans, map[string]any{"host": p.Host, "name": nullableString(out.device.Name), "vendor": p.Vendor, "evidence": p.Evidence, "scope": p.Scope, "commands": p.Commands, "script_file": scriptPath, "script_sha256": digest, "device_report": reportPath, "changes": p.Changes})
		}
		if out.state != topologyNotStarted {
			if e = storage.PrepareDirectory(filepath.Dir(reportPath)); e != nil {
				return e
			}
			if e = writeTopologyDeviceReport(ctx, directory, *out); e != nil {
				return e
			}
		}
		audits = append(audits, out.row(true))
	}
	b, e := json.MarshalIndent(map[string]any{"devices": plans, "audits": audits}, "", "  ")
	if e != nil {
		return e
	}
	path := filepath.Join(directory, "plan.json")
	if _, e = storage.Write(ctx, path, b); e != nil {
		return e
	}
	_, e = storage.Write(ctx, path+".sha256", fmt.Appendf(nil, "%x\n", sha256.Sum256(b)))
	return e
}
func saveTopologySummary(ctx context.Context, dir string, summary *TopologyReport) error {
	if e := saveTopologyJSON(ctx, dir, summary); e != nil {
		return e
	}
	_, e := storage.Write(ctx, filepath.Join(dir, "summary.txt"), []byte(topologyreport.SummaryText(summary.Presentation())))
	return e
}
func persistTopologySummary(ctx context.Context, dir string, summary *TopologyReport) error {
	err := saveTopologySummary(ctx, dir, summary)
	if err != nil {
		summary.Success = false
		if summary.ReportError == "" {
			summary.ReportError = "IOError"
		}
		// JSON may already be committed when the text report fails.
		_ = saveTopologyJSON(ctx, dir, summary)
	}
	return err
}
func saveTopologyJSON(ctx context.Context, dir string, summary *TopologyReport) error {
	summary.ReportLocation = filepath.Join(dir, "summary.json")
	b, e := json.MarshalIndent(summary.view(), "", "  ")
	if e != nil {
		return e
	}
	_, e = storage.Write(ctx, filepath.Join(dir, "summary.json"), b)
	return e
}
func writeTopologyDeviceReport(ctx context.Context, directory string, out topologyOutcome) error {
	name, e := out.device.BackupFilename("hostname_ip")
	if e != nil {
		return e
	}
	destination := filepath.Join(directory, "devices", name)
	if e = storage.PrepareDirectory(filepath.Dir(destination)); e != nil {
		return e
	}
	body := topologyreport.DeviceText(out.presentation())
	_, e = storage.Write(ctx, destination, []byte(body))
	return e
}

func makeTopologyOutcomes(outcomes []topologyOutcome) []TopologyOutcome {
	items := make([]TopologyOutcome, 0, len(outcomes))
	for _, out := range outcomes {
		audit, result := out.audit, out.result
		audit.Plan = cloneTopologyPlan(audit.Plan)
		audit.Comparisons = cloneTopologyComparisons(audit.Comparisons)
		audit.Issues, audit.Receipts = slices.Clone(audit.Issues), slices.Clone(audit.Receipts)
		result.Receipts = slices.Clone(result.Receipts)
		items = append(items, TopologyOutcome{Device: out.device, Audit: audit, Result: result, Plan: cloneTopologyPlan(out.plan)})
	}
	return items
}
