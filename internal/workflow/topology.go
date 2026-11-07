package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gatework/go-net-connector/config"
	"github.com/gatework/go-net-connector/fleet"
	"github.com/gatework/go-net-connector/internal/display"
	"github.com/gatework/go-net-connector/inventory"
	"github.com/gatework/go-net-connector/parallel"
	"github.com/gatework/go-net-connector/storage"
	"github.com/gatework/go-net-connector/topology"
)

var topologyPrefix = regexp.MustCompile(`^[\pL\pN_.-]+$`)
var topologyWhitespace = regexp.MustCompile(`\s+`)

type topologyOutcome struct {
	device fleet.Record
	plan   *topology.Plan
	row    map[string]any
	audit  topology.Audit
	result topology.Result
}

func ValidateTopologyPolicy(p config.Snapshot, o TopologyOptions) error {
	mode, _ := p.Value("topology.mode").(string)
	if mode == "" {
		mode = "plan"
	}
	if o.Mode == "apply" {
		mode = "apply"
	} else if o.Mode == "discover" {
		mode = "discover"
	} else if o.Mode == "plan" {
		mode = "plan"
	}
	if mode == "apply" && o.Mode != "apply" || mode != "plan" && mode != "discover" && mode != "apply" {
		return config.ErrInvalid
	}
	format, _ := p.Value("topology.description_format").(string)
	if strings.ContainsAny(format, "\r\n\x00") || strings.Contains(strings.ReplaceAll(strings.ReplaceAll(format, "%{neighbor}", ""), "%{port}", ""), "%") {
		return config.ErrInvalid
	}
	if values, ok := p.Value("topology.neighbor_name_prefixes").([]any); ok {
		for _, v := range values {
			prefix, ok := v.(string)
			if !ok || !topologyPrefix.MatchString(prefix) {
				return config.ErrInvalid
			}
		}
	}
	if cacheMode, ok := p.Value("topology.cache_mode").(string); ok && cacheMode != "prefer" && cacheMode != "only" && cacheMode != "refresh" {
		return config.ErrInvalid
	}
	return nil
}

func supportsTopologyMode(declaration any, mode string) bool {
	if _, ok := declaration.(topology.NeighborDiscovery); !ok {
		return false
	}
	if _, ok := declaration.(topology.DescriptionReader); !ok {
		return false
	}
	if _, ok := declaration.(topology.InterfaceIdentity); !ok {
		return false
	}
	if mode == "apply" {
		_, ok := declaration.(topology.DescriptionWriter)
		return ok
	}
	return true
}

func (r *Runner) Topology(ctx context.Context, runtime Runtime, settings TopologySettings, rows []inventory.Device, o TopologyOptions) (Result, error) {
	r.result = Result{}
	mode := settings.Mode
	if mode == "" {
		mode = "plan"
	}
	if o.Mode == "apply" {
		mode = "apply"
	} else if o.Mode == "discover" {
		mode = "discover"
	} else if o.Mode == "plan" {
		mode = "plan"
	}
	if mode == "apply" && o.Mode != "apply" || mode != "plan" && mode != "discover" && mode != "apply" {
		return r.fail(config.ErrInvalid)
	}
	// The inventory plan owns duplicate-address classification for every CLI.
	plan, e := fleet.NewPlan(rows, runtime.Rules, fleet.PlanOptions{Mode: "backup"})
	if e != nil {
		return r.fail(e)
	}
	devices := []fleet.Record{}
	inventoryRows := []map[string]any{}
	issues := map[string]int{}
	unexpected := false
	for _, entry := range plan.Entries() {
		record := entry.Device
		issue := record.Issue
		if entry.Status == "duplicate_host" {
			issue = entry.Status
		}
		inventoryRows = append(inventoryRows, map[string]any{"host": nullableString(record.Host), "vendor": nullableString(record.Vendor), "issue": nullableString(issue)})
		if issue == "" {
			devices = append(devices, record)
		} else {
			issues[issue]++
			if issue != "filtered" {
				unexpected = true
			}
		}
	}
	if o.InventoryOnly {
		preview := []map[string]any{}
		for _, d := range devices {
			preview = append(preview, map[string]any{"host": d.Host, "name": nullableString(d.Name), "vendor": d.Vendor})
		}
		r.result = Result{Success: len(devices) > 0, Topology: &TopologyResult{Inventory: plan.Entries(), Summary: map[string]any{"total": len(rows), "ready": len(devices), "issues": issues, "devices": preview}}}
		return r.result, nil
	}

	directory := settings.Directory
	workers := settings.Workers
	if workers < 1 {
		workers = runtime.Workers
	}
	limit := o.Limit
	if limit == 0 {
		limit = settings.Limit
	}
	if limit > 0 {
		selected := []fleet.Record{}
		capable := 0
		for _, d := range devices {
			declaration, lookupErr := runtime.Registry.Lookup(d.Vendor)
			if lookupErr == nil && supportsTopologyMode(declaration, mode) {
				capable++
				if capable > limit {
					continue
				}
			}
			selected = append(selected, d)
		}
		devices = selected
	}
	progress := r.reporter()
	_ = progress.Plan(len(rows), len(devices), workers, "topology")
	observer := r.observe()
	format := settings.DescriptionFormat
	if format == "" {
		format = "To_%{neighbor}_%{port}"
	}
	if strings.ContainsAny(format, "\r\n\x00") || strings.Contains(strings.ReplaceAll(strings.ReplaceAll(format, "%{neighbor}", ""), "%{port}", ""), "%") {
		return r.fail(config.ErrInvalid)
	}
	abbreviate := settings.AbbreviatePorts
	lower := settings.LowercasePorts
	prefixes := append([]string(nil), settings.NeighborNamePrefixes...)
	for _, prefix := range prefixes {
		if !topologyPrefix.MatchString(prefix) {
			return r.fail(config.ErrInvalid)
		}
	}
	directory, e = storage.CreateBatchDirectory(directory, time.Now())
	if e != nil {
		return r.fail(e)
	}
	whitespace := topologyWhitespace
	formatter := func(n topology.Neighbor) (string, error) {
		port := n.NeighborInterface
		if abbreviate {
			port = topology.ShortInterface(port, lower)
		} else if lower {
			port = strings.ToLower(port)
		}
		return strings.ReplaceAll(strings.ReplaceAll(format, "%{neighbor}", whitespace.ReplaceAllString(n.NeighborName, "_")), "%{port}", whitespace.ReplaceAllString(port, "_")), nil
	}
	opts := topology.FormatOptions{FullInterfaceNames: !abbreviate, Lowercase: lower, Formatter: formatter, DescriptionMatcher: func(current, expected string, n topology.Neighbor) bool {
		if topology.EquivalentDescription(current, expected) {
			return true
		}
		for _, prefix := range prefixes {
			variant := n
			variant.NeighborName = prefix + n.NeighborName
			text, _ := formatter(variant)
			if topology.EquivalentDescription(current, text) {
				return true
			}
		}
		return false
	}}
	eventsFile, e := storage.OpenAppend(filepath.Join(directory, "events.jsonl"))
	if e != nil {
		return r.fail(e)
	}
	defer eventsFile.Close()
	var eventMu sync.Mutex
	var eventError error
	var progressError error
	notify := func(out *topologyOutcome) {
		eventMu.Lock()
		defer eventMu.Unlock()
		copy := map[string]any{}
		for k, v := range out.row {
			if k != "commands" {
				copy[k] = v
			}
		}
		if e := json.NewEncoder(eventsFile).Encode(copy); e != nil {
			out.row["event_error_type"] = "IOError"
			if eventError == nil {
				eventError = e
			}
		}
		if r.Reporter != nil {
			code := ""
			if diag, ok := out.row["diagnostic"].(map[string]any); ok {
				code, _ = diag["error_code"].(string)
			}
			duration, _ := out.row["duration_ms"].(int64)
			if e := r.Reporter.Outcome(out.device.Host, fmt.Sprint(out.row["status"]), code, time.Duration(duration)*time.Millisecond); e != nil {
				out.row["progress_error_type"] = "IOError"
				if progressError == nil {
					progressError = e
				}
			}
		} else if r.Progress != nil {
			if _, e := fmt.Fprintf(r.Progress, "%s: %v\n", out.device.Host, display.Status(fmt.Sprint(out.row["status"]))); e != nil {
				out.row["progress_error_type"] = "IOError"
				if progressError == nil {
					progressError = e
				}
			}
		}
	}
	execute := func(ctx context.Context, d fleet.Record, previous *topologyOutcome) (out topologyOutcome, err error) {
		_ = progress.Start(d.Host, d.Vendor)
		start := time.Now()
		out = topologyOutcome{device: d, row: map[string]any{"host": d.Host, "vendor": d.Vendor, "status": "failed"}}
		if previous != nil {
			out = *previous
			out.row = map[string]any{}
			for k, v := range previous.row {
				out.row[k] = v
			}
		}
		defer func() {
			out.row["started_at"] = start.UTC().Format(time.RFC3339)
			out.row["finished_at"] = time.Now().UTC().Format(time.RFC3339)
			out.row["duration_ms"] = time.Since(start).Milliseconds()
			if err != nil {
				if out.result.Persisted {
					out.row["status"] = "persisted_with_error"
				} else if errors.Is(err, fleet.ErrMissingCredentials) {
					out.row["status"] = "missing_credentials"
				} else if ctx.Err() != nil {
					out.row["status"] = "interrupted"
				} else {
					if count, ok := out.row["steps"].(int); ok && count > 0 {
						out.row["status"] = "partial"
					} else {
						out.row["status"] = "failed"
					}
				}
				var te *topology.Error
				if errors.As(err, &te) {
					out.row["diagnostic"] = map[string]any{"error_code": te.Code, "phase": te.Phase, "error_type": "Net::Connector::Error"}
				} else {
					out.row["diagnostic"] = map[string]any{"error_type": "StandardError"}
				}
			}
			out.row["session_log"] = filepath.Join(runtime.logDir("topology", directory), strings.ReplaceAll(d.Host, ":", "_")+".log")
			notify(&out)
		}()
		declaration, lookupErr := runtime.Registry.Lookup(d.Vendor)
		if lookupErr != nil || !supportsTopologyMode(declaration, mode) {
			out.row["status"] = "unsupported_operation"
			return out, nil
		}
		device, closeDevice, e := openDevice(ctx, runtime, d, runtime.logDir("topology", directory), observer)
		if e != nil {
			return out, e
		}
		defer func() {
			err = joinDeviceCleanup(err, closeDevice())
		}()
		t, e := topology.New(device, runtime.Registry)
		if e != nil {
			return out, e
		}
		if previous != nil {
			result, e := t.Apply(ctx, *previous.plan, topology.ApplyOptions{Confirmed: true})
			out.result = result
			out.row["verified"] = result.Verified
			out.row["persisted"] = result.Persisted
			out.row["receipts"] = result.Receipts
			out.row["steps"] = len(result.Steps())
			out.row["status"] = "applied"
			if items, ok := out.row["issues"].([]topology.Issue); ok && len(items) > 0 {
				out.row["status"] = "partial_applied"
			}
			return out, e
		}
		audit, e := t.Audit(ctx, opts)
		out.audit = audit
		out.row["receipts"] = audit.Receipts
		out.row["steps"] = len(audit.Receipts)
		out.plan = audit.Plan
		if mode == "discover" {
			out.plan = nil
		}
		out.row["neighbors"] = audit.Comparisons
		out.row["issues"] = audit.Issues
		out.row["changes"] = audit.Changes()
		out.row["commands"] = []string{}
		if out.plan != nil {
			out.row["commands"] = out.plan.Commands
		}
		if e != nil {
			return out, e
		}
		status := "discovered"
		if !audit.Complete() {
			status = "unverified"
			if out.plan != nil && len(out.plan.Changes) > 0 && mode != "discover" {
				status = "partial_planned"
			}
		} else if mode != "discover" {
			if audit.ManualRequired {
				status = "manual_required"
			} else if out.plan != nil {
				status = "planned"
			}
		}
		out.row["status"] = status
		return out, nil
	}
	collected, collectErr := parallel.Map(ctx, workers, devices, func(ctx context.Context, d fleet.Record) (topologyOutcome, error) { return execute(ctx, d, nil) })
	outcomes := make([]topologyOutcome, len(devices))
	for i, item := range collected {
		if item.Started && item.Value.row != nil {
			outcomes[i] = item.Value
		} else {
			outcomes[i] = topologyOutcome{device: devices[i], row: map[string]any{"host": devices[i].Host, "vendor": devices[i].Vendor, "status": "not_started"}}
			if item.Started {
				outcomes[i].row["status"] = "failed"
				outcomes[i].row["diagnostic"] = map[string]any{"error_code": "internal_error", "phase": "callback"}
			}
		}
	}
	// Preserve every collected audit before any later persistence can fail.
	collectedRows := make([]map[string]any, 0, len(outcomes))
	for _, out := range outcomes {
		collectedRows = append(collectedRows, out.row)
	}

	r.result = Result{Topology: &TopologyResult{Directory: directory, Outcomes: makeTopologyOutcomes(outcomes), Summary: map[string]any{"mode": mode, "success": false, "directory": directory, "inventory": inventoryRows, "devices": collectedRows}}}
	// All review artifacts are durable before any application task is admitted.
	_ = progress.Phase("正在保存拓扑计划")
	if e = saveTopologyPlan(context.WithoutCancel(ctx), directory, outcomes, runtime.Registry); e != nil {
		r.result.Topology.Summary["report_error"] = "IOError"
		_ = persistTopologySummary(context.WithoutCancel(ctx), directory, r.result.Topology.Summary)
		return r.fail(e)
	}
	if mode == "apply" && collectErr == nil {
		pending := 0
		for _, out := range outcomes {
			if out.plan != nil && len(out.plan.Changes) > 0 && (out.row["status"] == "planned" || out.row["status"] == "partial_planned") {
				pending++
			}
		}
		_ = progress.Phase("重新核验并应用拓扑计划")
		_ = progress.BeginPhase(pending, workers)
		applied, applyErr := parallel.Map(ctx, workers, outcomes, func(ctx context.Context, out topologyOutcome) (topologyOutcome, error) {
			if out.plan == nil || out.row["status"] != "planned" && out.row["status"] != "partial_planned" {
				return out, nil
			}
			if len(out.plan.Changes) == 0 {
				out.row["status"] = "unchanged"
				notify(&out)
				return out, nil
			}
			return execute(ctx, out.device, &out)
		})
		for i, item := range applied {
			if item.Started && item.Value.row != nil {
				outcomes[i] = item.Value
			} else if item.Started {
				outcomes[i].row["status"] = "failed"
				outcomes[i].row["diagnostic"] = map[string]any{"error_code": "internal_error", "phase": "callback"}
			}
		}
		collectErr = applyErr
	}
	counts := map[string]int{}
	summaries := []map[string]any{}
	success := !unexpected && ctx.Err() == nil
	attempted := 0
	stats := map[string]int{"collected": 0, "failed": 0, "unchanged": 0, "change_devices": 0, "changes": 0, "review_devices": 0, "review_interfaces": 0}
	for _, out := range outcomes {
		status, _ := out.row["status"].(string)
		counts[status]++
		summaries = append(summaries, out.row)
		if status != "unsupported_operation" {
			attempted++
		}
		changes, _ := out.row["changes"].(int)
		items, _ := out.row["issues"].([]topology.Issue)
		if changes > 0 {
			stats["change_devices"]++
			stats["changes"] += changes
		}
		if len(items) > 0 {
			stats["review_devices"]++
			stats["review_interfaces"] += len(items)
		}
		if (status == "planned" || status == "unchanged") && changes == 0 {
			stats["unchanged"]++
		}
		bad := status == "persisted_with_error" || status == "failed" || status == "partial" || status == "missing_credentials" || status == "interrupted" || status == "not_started"
		if bad || out.row["event_error_type"] != nil || out.row["progress_error_type"] != nil {
			stats["failed"]++
		} else if status != "unsupported_operation" {
			stats["collected"]++
		}
		if status != "planned" && status != "discovered" && status != "applied" && status != "unchanged" && status != "unsupported_operation" || mode == "plan" && changes > 0 {
			success = false
		}
	}
	success = success && attempted > 0
	summary := map[string]any{"mode": mode, "success": success, "directory": directory, "counts": counts, "statistics": stats, "inventory": inventoryRows, "devices": summaries}
	if ctx.Err() != nil {
		summary["interrupted"] = true
	}
	if collectErr != nil {
		summary["orchestration_error_type"] = "StandardError"
		summary["success"] = false
	}
	if e = eventsFile.Sync(); eventError == nil {
		eventError = e
	}
	if eventError != nil || progressError != nil {
		summary["success"] = false
		summary["report_error"] = "IOError"
	}
	if progressError != nil {
		summary["progress_error"] = "IOError"
	}
	if mode == "apply" {
		for _, out := range outcomes {
			if out.row["status"] == "not_started" {
				continue
			}
			if e = writeTopologyDeviceReport(context.WithoutCancel(ctx), directory, out); e != nil {
				summary["success"] = false
				summary["report_error"] = "IOError"
				break
			}
		}
	}
	_ = persistTopologySummary(context.WithoutCancel(ctx), directory, summary)
	r.result = Result{Success: summary["success"] == true, Topology: &TopologyResult{Directory: directory, Summary: summary, Outcomes: makeTopologyOutcomes(outcomes)}}
	_ = progress.Finish()
	if ctx.Err() != nil {
		return r.result, context.Canceled
	}
	if summary["success"] == true {
		return r.complete(true), nil
	}
	return r.complete(false), nil
}
