package cli

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gatework/go-net-connector/config"
	"github.com/gatework/go-net-connector/fleet"
	"github.com/gatework/go-net-connector/internal/bootstrap"
	"github.com/gatework/go-net-connector/inventory"
	"github.com/gatework/go-net-connector/parallel"
	"github.com/gatework/go-net-connector/storage"
	"github.com/gatework/go-net-connector/topology"
	"github.com/gatework/go-net-connector/vendors"
)

type topologyOutcome struct {
	device fleet.Record
	plan   *topology.Plan
	row    map[string]any
}

func validateTopologyPolicy(p config.Snapshot, o options) error {
	mode, _ := p.Value("topology.mode").(string)
	if mode == "" {
		mode = "plan"
	}
	if o.apply {
		mode = "apply"
	} else if o.discover {
		mode = "discover"
	} else if o.flags.Changed("plan") {
		mode = "plan"
	}
	if mode == "apply" && !o.apply || mode != "plan" && mode != "discover" && mode != "apply" {
		return config.ErrInvalid
	}
	format, _ := p.Value("topology.description_format").(string)
	if strings.ContainsAny(format, "\r\n\x00") || strings.Contains(strings.ReplaceAll(strings.ReplaceAll(format, "%{neighbor}", ""), "%{port}", ""), "%") {
		return config.ErrInvalid
	}
	if values, ok := p.Value("topology.neighbor_name_prefixes").([]any); ok {
		for _, v := range values {
			prefix, ok := v.(string)
			if !ok || !regexp.MustCompile(`^[\pL\pN_.-]+$`).MatchString(prefix) {
				return config.ErrInvalid
			}
		}
	}
	if cacheMode, ok := p.Value("topology.cache_mode").(string); ok && cacheMode != "prefer" && cacheMode != "only" && cacheMode != "refresh" {
		return config.ErrInvalid
	}
	return nil
}

func (r Runtime) runTopology(ctx context.Context, services *bootstrap.Services, rows []inventory.Device, o options, reader *bufio.Reader) int {
	p := services.Policy()
	mode, _ := p.Value("topology.mode").(string)
	if mode == "" {
		mode = "plan"
	}
	if o.apply {
		mode = "apply"
	} else if o.discover {
		mode = "discover"
	} else if o.flags.Changed("plan") {
		mode = "plan"
	}
	if mode == "apply" && !o.apply || mode != "plan" && mode != "discover" && mode != "apply" {
		return r.fail("net-topology", config.ErrInvalid)
	}
	// The inventory plan owns duplicate-address classification for every CLI.
	plan, e := fleet.NewPlan(rows, services.Rules(), fleet.PlanOptions{Mode: "backup"})
	if e != nil {
		return r.fail("net-topology", e)
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
		inventoryRows = append(inventoryRows, map[string]any{"host": nullable(record.Host), "vendor": nullable(record.Vendor), "issue": nullable(issue)})
		if issue == "" {
			devices = append(devices, record)
		} else {
			issues[issue]++
			if issue != "filtered" {
				unexpected = true
			}
		}
	}
	if o.plan {
		preview := []map[string]any{}
		for _, d := range devices {
			preview = append(preview, map[string]any{"host": d.Host, "name": nullable(d.Name), "vendor": d.Vendor})
		}
		if r.displayTopology(map[string]any{"total": len(rows), "ready": len(devices), "issues": issues, "devices": preview}, o) != nil {
			if ctx.Err() != nil {
				return 130
			}
			return 2
		}
		if len(devices) == 0 {
			return 1
		}
		return 0
	}
	if mode == "apply" && o.stdin {
		line, e := readLine(reader, 64)
		if e != nil || strings.TrimSpace(line) != "RUN" {
			return 1
		}
	}
	directory, _ := p.Value("topology.directory").(string)
	workers := services.Workers()
	if n, ok := p.Value("topology.parallel").(int); ok {
		workers = n
	}
	limit := o.limit
	if limit == 0 {
		limit, _ = p.Value("topology.limit").(int)
	}
	if limit > 0 {
		selected := []fleet.Record{}
		capable := 0
		for _, d := range devices {
			profile, _ := vendors.Profile(d.Vendor)
			if profile.Capabilities.Neighbors {
				capable++
				if capable > limit {
					continue
				}
			}
			selected = append(selected, d)
		}
		devices = selected
	}
	format := "To_%{neighbor}_%{port}"
	if v, ok := p.Value("topology.description_format").(string); ok {
		format = v
	}
	if strings.ContainsAny(format, "\r\n\x00") || strings.Contains(strings.ReplaceAll(strings.ReplaceAll(format, "%{neighbor}", ""), "%{port}", ""), "%") {
		return r.fail("net-topology", config.ErrInvalid)
	}
	abbreviate := true
	if v, ok := p.Value("topology.abbreviate_ports").(bool); ok {
		abbreviate = v
	}
	lower, _ := p.Value("topology.lowercase_ports").(bool)
	prefixes := []string{}
	if values, ok := p.Value("topology.neighbor_name_prefixes").([]any); ok {
		for _, v := range values {
			prefix, ok := v.(string)
			if !ok || !regexp.MustCompile(`^[\pL\pN_.-]+$`).MatchString(prefix) {
				return r.fail("net-topology", config.ErrInvalid)
			}
			prefixes = append(prefixes, prefix)
		}
	}
	directory, e = storage.CreateBatchDirectory(directory, time.Now())
	if e != nil {
		return r.fail("net-topology", e)
	}
	whitespace := regexp.MustCompile(`\s+`)
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
		return r.fail("net-topology", e)
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
		if !o.quiet {
			if _, e := fmt.Fprintf(r.Error, "%s: %v\n", out.device.Host, out.row["status"]); e != nil {
				out.row["progress_error_type"] = "IOError"
				if progressError == nil {
					progressError = e
				}
			}
		}
	}
	execute := func(ctx context.Context, d fleet.Record, previous *topologyOutcome) (out topologyOutcome, err error) {
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
				if errors.Is(err, bootstrap.ErrCredentials) {
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
			out.row["session_log"] = filepath.Join(services.LogDirectory("net-topology", "topology", directory), strings.ReplaceAll(d.Host, ":", "_")+".log")
			notify(&out)
		}()
		profile, _ := vendors.Profile(d.Vendor)
		if !profile.Capabilities.Neighbors {
			out.row["status"] = "unsupported_operation"
			return out, nil
		}
		device, closeDevice, e := openDevice(ctx, services, d, services.LogDirectory("net-topology", "topology", directory))
		if e != nil {
			return out, e
		}
		defer func() {
			err = joinDeviceCleanup(err, closeDevice())
		}()
		t, e := topology.New(device)
		if e != nil {
			return out, e
		}
		if previous != nil {
			result, e := t.Apply(ctx, *previous.plan, topology.ApplyOptions{Confirmed: true})
			out.row["steps"] = len(result.Steps())
			out.row["status"] = "applied"
			if items, ok := out.row["issues"].([]topology.Issue); ok && len(items) > 0 {
				out.row["status"] = "partial_applied"
			}
			return out, e
		}
		audit, e := t.Audit(ctx, opts)
		out.row["steps"] = len(audit.Receipts)
		if e != nil {
			return out, e
		}
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
		if item.Started {
			outcomes[i] = item.Value
		} else {
			outcomes[i] = topologyOutcome{device: devices[i], row: map[string]any{"host": devices[i].Host, "vendor": devices[i].Vendor, "status": "not_started"}}
		}
	}
	// All review artifacts are durable before any application task is admitted.
	if e = saveTopologyPlan(context.WithoutCancel(ctx), directory, outcomes); e != nil {
		return r.fail("net-topology", e)
	}
	if mode == "apply" && collectErr == nil {
		applied, applyErr := parallel.Map(ctx, workers, outcomes, func(ctx context.Context, out topologyOutcome) (topologyOutcome, error) {
			if out.plan == nil || out.row["status"] != "planned" && out.row["status"] != "partial_planned" {
				return out, nil
			}
			return execute(ctx, out.device, &out)
		})
		for i, item := range applied {
			if item.Started {
				outcomes[i] = item.Value
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
		if status == "planned" && changes == 0 {
			stats["unchanged"]++
		}
		bad := status == "failed" || status == "partial" || status == "missing_credentials" || status == "interrupted" || status == "not_started"
		if bad || out.row["event_error_type"] != nil || out.row["progress_error_type"] != nil {
			stats["failed"]++
		} else if status != "unsupported_operation" {
			stats["collected"]++
		}
		if status != "planned" && status != "discovered" && status != "applied" && status != "unsupported_operation" || mode == "plan" && changes > 0 {
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
			if e = writeTopologyDeviceReport(context.WithoutCancel(ctx), directory, out); e != nil {
				summary["success"] = false
				summary["report_error"] = "IOError"
				break
			}
		}
	}
	_ = persistTopologySummary(context.WithoutCancel(ctx), directory, summary)
	outputErr := r.displayTopology(summary, o)
	if outputErr != nil {
		summary["success"] = false
		if summary["report_error"] == nil {
			summary["report_error"] = "IOError"
		}
		summary["output_error"] = "IOError"
		_ = persistTopologySummary(context.WithoutCancel(ctx), directory, summary)
	}
	if ctx.Err() != nil {
		return 130
	}
	if outputErr != nil {
		return 2
	}
	if summary["success"] == true {
		return 0
	}
	return 1
}
func saveTopologyPlan(ctx context.Context, directory string, outcomes []topologyOutcome) error {
	plans := []map[string]any{}
	audits := []map[string]any{}
	for _, out := range outcomes {
		name, e := out.device.BackupFilename("hostname_ip")
		if e != nil {
			return e
		}
		stem := strings.TrimSuffix(name, ".txt")
		out.row["name"] = nullable(out.device.Name)
		reportPath := filepath.Join(directory, "devices", stem+".txt")
		out.row["device_report"] = reportPath
		if out.plan != nil {
			p := out.plan
			if e = topology.ValidatePlan(*p, out.device.Host, out.device.Vendor); e != nil {
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
				scriptPath = path
				digest = fmt.Sprintf("%x", sha256.Sum256([]byte(body)))
				out.row["script_file"], out.row["script_sha256"] = scriptPath, digest
			}
			plans = append(plans, map[string]any{"host": p.Host, "name": nullable(out.device.Name), "vendor": p.Vendor, "evidence": p.Evidence, "scope": p.Scope, "commands": p.Commands, "script_file": scriptPath, "script_sha256": digest, "device_report": reportPath, "changes": p.Changes})
		}
		if e = storage.PrepareDirectory(filepath.Dir(reportPath)); e != nil {
			return e
		}
		if e = writeTopologyDeviceReport(ctx, directory, out); e != nil {
			return e
		}
		audits = append(audits, out.row)
	}
	b, e := json.MarshalIndent(map[string]any{"devices": plans, "audits": audits}, "", "  ")
	if e != nil {
		return e
	}
	path := filepath.Join(directory, "plan.json")
	if _, e = storage.Write(ctx, path, b); e != nil {
		return e
	}
	_, e = storage.Write(ctx, path+".sha256", []byte(fmt.Sprintf("%x\n", sha256.Sum256(b))))
	return e
}
func saveTopologySummary(ctx context.Context, dir string, summary map[string]any) error {
	if e := saveTopologyJSON(ctx, dir, summary); e != nil {
		return e
	}
	_, e := storage.Write(ctx, filepath.Join(dir, "summary.txt"), []byte(topologySummaryText(summary)))
	return e
}
func persistTopologySummary(ctx context.Context, dir string, summary map[string]any) error {
	err := saveTopologySummary(ctx, dir, summary)
	if err != nil {
		summary["success"] = false
		if summary["report_error"] == nil {
			summary["report_error"] = "IOError"
		}
		// JSON may already be committed when the text report fails.
		_ = saveTopologyJSON(ctx, dir, summary)
	}
	return err
}
func saveTopologyJSON(ctx context.Context, dir string, summary map[string]any) error {
	summary["report_location"] = filepath.Join(dir, "summary.json")
	b, e := json.MarshalIndent(summary, "", "  ")
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
	var body strings.Builder
	fmt.Fprintf(&body, "Device: %s (%s)\nVendor: %s\nStatus: %v; candidate changes: %v\nCompleted steps: %v\n", safeTopologyText(out.device.Name), out.device.Host, out.device.Vendor, out.row["status"], out.row["changes"], out.row["steps"])
	if script, ok := out.row["script_file"].(string); ok {
		fmt.Fprintf(&body, "Command file: %s\nCommand SHA-256: %v\n", script, out.row["script_sha256"])
	} else {
		body.WriteString("Command file: not generated (no verified configuration changes)\n")
	}
	if diagnostic, ok := out.row["diagnostic"].(map[string]any); ok {
		fmt.Fprintf(&body, "Diagnostics: %v\n", diagnostic["error_code"])
	}
	body.WriteString("Configuration commands come from this plan; check status and device receipts for execution results.\n")
	if rows, ok := out.row["neighbors"].([]topology.Comparison); ok {
		for _, row := range rows {
			fmt.Fprintf(&body, "%s %s current=%s target=%s\n", safeTopologyText(row.Interface), safeTopologyText(row.Status), topologyQuoted(row.Current), topologyQuoted(row.Expected))
		}
	}
	if issues, ok := out.row["issues"].([]topology.Issue); ok {
		for _, issue := range issues {
			fmt.Fprintf(&body, "Unverified: %s %s\n", safeTopologyText(issue.Interface), topologyReason(issue.Code))
		}
	}
	_, e = storage.Write(ctx, destination, []byte(body.String()))
	return e
}

func safeTopologyText(value string) string {
	return strings.Map(func(c rune) rune {
		if c < ' ' || c == 127 {
			return ' '
		}
		return c
	}, value)
}
func topologyQuoted(value *string) string {
	if value == nil {
		return "nil"
	}
	return fmt.Sprintf("%q", *value)
}
func topologyReason(code string) string {
	switch code {
	case "unknown_neighbor_name":
		return "neighbor name missing"
	case "unknown_neighbor_interface":
		return "neighbor port missing"
	case "ambiguous_neighbor":
		return "multiple neighbors on one interface"
	case "interface_missing":
		return "local interface not found in configuration"
	case "unsafe_description":
		return "target description violates safety constraints"
	default:
		return safeTopologyText(code)
	}
}

func topologySummaryText(summary map[string]any) string {
	var b strings.Builder
	b.WriteString(topologyResultLines(summary))
	if rows, ok := summary["devices"].([]map[string]any); ok {
		for _, row := range rows {
			name, _ := row["name"].(string)
			script, _ := row["script_file"].(string)
			if script == "" {
				script = "not generated"
			}
			fmt.Fprintf(&b, "%v %s %v changes=%v steps=%v\n  Device report: %v\n  Command file: %s\n", row["host"], safeTopologyText(name), row["status"], row["changes"], row["steps"], row["device_report"], script)
			if hash, ok := row["script_sha256"].(string); ok {
				fmt.Fprintf(&b, "  Command SHA-256: %s\n", hash)
			}
			if diagnostic, ok := row["diagnostic"].(map[string]any); ok {
				fmt.Fprintf(&b, "  Diagnostics: %v\n", diagnostic["error_code"])
			}
			if comparisons, ok := row["neighbors"].([]topology.Comparison); ok {
				for _, item := range comparisons {
					fmt.Fprintf(&b, "  %s %s current=%s expected=%s\n", safeTopologyText(item.Interface), safeTopologyText(item.Status), topologyQuoted(item.Current), topologyQuoted(item.Expected))
				}
			}
			if issues, ok := row["issues"].([]topology.Issue); ok {
				for _, item := range issues {
					fmt.Fprintf(&b, "  %s %s\n", safeTopologyText(item.Interface), topologyReason(item.Code))
				}
			}
			if commands, ok := row["commands"].([]string); ok {
				for _, command := range commands {
					fmt.Fprintf(&b, "  command=%q\n", command)
				}
			}
		}
	}
	return b.String()
}

func topologyResultLines(summary map[string]any) string {
	var b strings.Builder
	stats, _ := summary["statistics"].(map[string]int)
	result := "pending items remain"
	switch {
	case summary["orchestration_error_type"] != nil || summary["interrupted"] == true || summary["progress_error"] != nil:
		result = "incomplete"
	case summary["success"] == true:
		result = "complete"
	case stats["failed"] > 0:
		result = "execution contains failures"
	default:
		rows, _ := summary["devices"].([]map[string]any)
		if len(rows) == 0 {
			result = "no eligible devices"
		} else {
			switch summary["mode"] {
			case "plan":
				result = "plan generated; corrections or verification still required"
			case "discover":
				result = "discovery finished; unverified interfaces remain"
			case "apply":
				result = "apply finished; pending items remain"
			}
		}
	}
	fmt.Fprintf(&b, "Result: %s; mode: %v\nDevices: collected %d; unchanged %d; with candidate changes %d; failed %d devices\nInterfaces: candidate changes %d; unverified %d (across %d devices)\n", result, summary["mode"], stats["collected"], stats["unchanged"], stats["change_devices"], stats["failed"], stats["changes"], stats["review_interfaces"], stats["review_devices"])
	reasons := map[string]int{}
	if rows, ok := summary["devices"].([]map[string]any); ok {
		for _, row := range rows {
			if issues, ok := row["issues"].([]topology.Issue); ok {
				for _, issue := range issues {
					reasons[issue.Code]++
				}
			}
		}
	}
	keys := make([]string, 0, len(reasons))
	for key := range reasons {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s %d interfaces", topologyReason(key), reasons[key]))
	}
	fmt.Fprintf(&b, "Unverified reasons: %s\n", strings.Join(parts, "; "))
	return b.String()
}

func (r Runtime) displayTopology(data map[string]any, o options) error {
	if o.json {
		return writeReportOutput(r.Output, data)
	}
	var b strings.Builder
	if total, ok := data["total"].(int); ok {
		ready, _ := data["ready"].(int)
		fmt.Fprintf(&b, "Inventory %d devices; selected %d; skipped %d devices\n", total, ready, total-ready)
		if rows, ok := data["devices"].([]map[string]any); ok {
			for _, row := range rows {
				fmt.Fprintf(&b, "%v\t%v\t%v\n", row["host"], row["vendor"], row["name"])
			}
		}
	}
	if directory, ok := data["directory"].(string); ok {
		b.WriteString(topologyResultLines(data))
		scripts := 0
		if rows, ok := data["devices"].([]map[string]any); ok {
			for _, row := range rows {
				if script, ok := row["script_file"].(string); ok && script != "" {
					scripts++
				}
			}
		}
		if scripts > 0 {
			fmt.Fprintf(&b, "Command file: %s(%d devices with changes)\n", filepath.Join(directory, "scripts"), scripts)
		}
		fmt.Fprintf(&b, "Device report: %s\nPlan: %s\nReport: %s\nText report: %s\n", filepath.Join(directory, "devices"), filepath.Join(directory, "plan.json"), filepath.Join(directory, "summary.json"), filepath.Join(directory, "summary.txt"))
	}
	text := strings.Map(func(c rune) rune {
		if c < ' ' && c != '\n' && c != '\t' || c == 127 {
			return ' '
		}
		return c
	}, b.String())
	_, e := fmt.Fprint(r.Output, text)
	return e
}
