package workflow

import (
	"maps"
	"slices"
	"time"

	"github.com/gatework/go-net-connector/fleet"
	"github.com/gatework/go-net-connector/internal/topologyreport"
	"github.com/gatework/go-net-connector/topology"
)

type topologyState string

const (
	topologyNotStarted         topologyState = "not_started"
	topologyFailed             topologyState = "failed"
	topologyPersistedWithError topologyState = "persisted_with_error"
	topologyMissingCredentials topologyState = "missing_credentials"
	topologyInterrupted        topologyState = "interrupted"
	topologyPartial            topologyState = "partial"
	topologyUnsupported        topologyState = "unsupported_operation"
	topologyApplied            topologyState = "applied"
	topologyPartialApplied     topologyState = "partial_applied"
	topologyDiscovered         topologyState = "discovered"
	topologyUnverified         topologyState = "unverified"
	topologyPartialPlanned     topologyState = "partial_planned"
	topologyManualRequired     topologyState = "manual_required"
	topologyPlanned            topologyState = "planned"
	topologyUnchanged          topologyState = "unchanged"
)

func (s topologyState) permitsApply() bool {
	return s == topologyPlanned || s == topologyPartialPlanned
}

func (s topologyState) failed() bool {
	switch s {
	case topologyPersistedWithError, topologyFailed, topologyPartial, topologyMissingCredentials, topologyInterrupted, topologyNotStarted:
		return true
	default:
		return false
	}
}

func (s topologyState) successful() bool {
	switch s {
	case topologyPlanned, topologyDiscovered, topologyApplied, topologyUnchanged, topologyUnsupported:
		return true
	default:
		return false
	}
}

type topologyDiagnostic struct{ code, phase, errorType string }

// topologyOutcome owns execution evidence. Reports are projections and never
// feed state back into apply eligibility, completion or error classification.
type topologyOutcome struct {
	device                    fleet.Record
	state                     topologyState
	plan                      *topology.Plan
	audit                     topology.Audit
	result                    topology.Result
	audited, applied, named   bool
	startedAt, finishedAt     time.Time
	duration                  time.Duration
	diagnostic                topologyDiagnostic
	sessionLog, deviceReport  string
	scriptFile, scriptSHA256  string
	eventError, progressError bool
}

func (o topologyOutcome) steps() int {
	if o.applied {
		return len(o.result.Receipts)
	}
	return len(o.audit.Receipts)
}

func (o topologyOutcome) commands() []string {
	if o.plan != nil {
		return slices.Clone(o.plan.Commands)
	}
	return []string{}
}

func (o topologyOutcome) row(includeCommands bool) map[string]any {
	row := map[string]any{"host": o.device.Host, "vendor": o.device.Vendor, "status": string(o.state)}
	if o.named {
		row["name"] = nullableString(o.device.Name)
	}
	if o.audited {
		row["receipts"], row["steps"] = slices.Clone(o.audit.Receipts), o.steps()
		row["neighbors"], row["issues"], row["changes"] = cloneTopologyComparisons(o.audit.Comparisons), slices.Clone(o.audit.Issues), o.audit.Changes()
		if includeCommands {
			row["commands"] = o.commands()
		}
	}
	if o.applied {
		row["receipts"], row["steps"] = slices.Clone(o.result.Receipts), o.steps()
		row["verified"], row["persisted"] = o.result.Verified, o.result.Persisted
	}
	if !o.startedAt.IsZero() {
		row["started_at"] = o.startedAt.UTC().Format(time.RFC3339)
		row["finished_at"] = o.finishedAt.UTC().Format(time.RFC3339)
		row["duration_ms"] = o.duration.Milliseconds()
	}
	if d := o.diagnostic; d.code != "" || d.phase != "" || d.errorType != "" {
		diagnostic := map[string]any{}
		if d.code != "" {
			diagnostic["error_code"] = d.code
		}
		if d.phase != "" {
			diagnostic["phase"] = d.phase
		}
		if d.errorType != "" {
			diagnostic["error_type"] = d.errorType
		}
		row["diagnostic"] = diagnostic
	}
	for key, value := range map[string]string{"session_log": o.sessionLog, "device_report": o.deviceReport, "script_file": o.scriptFile, "script_sha256": o.scriptSHA256} {
		if value != "" {
			row[key] = value
		}
	}
	if o.eventError {
		row["event_error_type"] = "IOError"
	}
	if o.progressError {
		row["progress_error_type"] = "IOError"
	}
	return row
}

func cloneTopologyComparisons(source []topology.Comparison) []topology.Comparison {
	comparisons := slices.Clone(source)
	for i := range comparisons {
		if value := comparisons[i].Current; value != nil {
			comparisons[i].Current = new(*value)
		}
		if value := comparisons[i].Expected; value != nil {
			comparisons[i].Expected = new(*value)
		}
	}
	return comparisons
}

func cloneTopologyPlan(source *topology.Plan) *topology.Plan {
	if source == nil {
		return nil
	}
	plan := *source
	plan.Commands = slices.Clone(source.Commands)
	plan.Changes = slices.Clone(source.Changes)
	plan.Receipts = slices.Clone(source.Receipts)
	plan.Evidence = maps.Clone(source.Evidence)
	return &plan
}

type topologyInventoryEntry struct{ host, vendor, issue string }

type topologyPreview struct {
	total, ready int
	issues       map[string]int
	devices      []fleet.Record
}

// TopologyStatistics is computed from typed execution outcomes, before rendering.
type TopologyStatistics = topologyreport.Statistics

// TopologyReport retains execution and persistence facts independently of any
// rendered JSON map. CLI presentation can read its typed statistics and errors.
type TopologyReport struct {
	Mode, Directory, ReportLocation         string
	Success, Interrupted                    bool
	ReportError, ProgressError, OutputError string
	OrchestrationError                      string
	Statistics                              TopologyStatistics
	inventory                               []topologyInventoryEntry
	outcomes                                []topologyOutcome
	counts                                  map[topologyState]int
	preview                                 *topologyPreview
}

func (r *TopologyReport) summarize(unexpected bool) {
	r.counts = make(map[topologyState]int)
	r.Statistics = TopologyStatistics{}
	r.Success = !unexpected && !r.Interrupted
	attempted := 0
	for _, out := range r.outcomes {
		r.counts[out.state]++
		if out.state != topologyUnsupported {
			attempted++
		}
		changes, issues := out.audit.Changes(), len(out.audit.Issues)
		if changes > 0 {
			r.Statistics.ChangeDevices++
			r.Statistics.Changes += changes
		}
		if issues > 0 {
			r.Statistics.ReviewDevices++
			r.Statistics.ReviewInterfaces += issues
		}
		if (out.state == topologyPlanned || out.state == topologyUnchanged) && changes == 0 {
			r.Statistics.Unchanged++
		}
		if out.state.failed() || out.eventError || out.progressError {
			r.Statistics.Failed++
		} else if out.state != topologyUnsupported {
			r.Statistics.Collected++
		}
		if !out.state.successful() || r.Mode == "plan" && changes > 0 {
			r.Success = false
		}
	}
	r.Success = r.Success && attempted > 0
}

func (r *TopologyReport) view() map[string]any {
	if p := r.preview; p != nil {
		devices := make([]map[string]any, 0, len(p.devices))
		for _, d := range p.devices {
			devices = append(devices, map[string]any{"host": d.Host, "name": nullableString(d.Name), "vendor": d.Vendor})
		}
		issues := make(map[string]int, len(p.issues))
		maps.Copy(issues, p.issues)
		return map[string]any{"total": p.total, "ready": p.ready, "issues": issues, "devices": devices}
	}
	devices := make([]map[string]any, 0, len(r.outcomes))
	for _, out := range r.outcomes {
		devices = append(devices, out.row(true))
	}
	inventory := make([]map[string]any, 0, len(r.inventory))
	for _, entry := range r.inventory {
		inventory = append(inventory, map[string]any{"host": nullableString(entry.host), "vendor": nullableString(entry.vendor), "issue": nullableString(entry.issue)})
	}
	view := map[string]any{"mode": r.Mode, "success": r.Success, "directory": r.Directory, "inventory": inventory, "devices": devices}
	if r.counts != nil {
		counts := make(map[string]int, len(r.counts))
		for state, count := range r.counts {
			counts[string(state)] = count
		}
		view["counts"], view["statistics"] = counts, r.Statistics.Values()
	}
	if r.Interrupted {
		view["interrupted"] = true
	}
	for key, value := range map[string]string{"report_error": r.ReportError, "progress_error": r.ProgressError, "output_error": r.OutputError, "orchestration_error_type": r.OrchestrationError, "report_location": r.ReportLocation} {
		if value != "" {
			view[key] = value
		}
	}
	return view
}

func (r *TopologyReport) ScriptCount() int {
	count := 0
	for _, out := range r.outcomes {
		if out.scriptFile != "" {
			count++
		}
	}
	return count
}

// Presentation returns detached values for persistent and terminal reports.
// Mutating the projection cannot affect apply eligibility or retained evidence.
func (r *TopologyReport) Presentation() topologyreport.Summary {
	devices := make([]topologyreport.Device, 0, len(r.outcomes))
	for _, out := range r.outcomes {
		devices = append(devices, out.presentation())
	}
	return topologyreport.Summary{
		Mode: r.Mode, Success: r.Success, Interrupted: r.Interrupted,
		ReportError: r.ReportError, ProgressError: r.ProgressError, OutputError: r.OutputError,
		OrchestrationError: r.OrchestrationError, Statistics: r.Statistics, Devices: devices,
	}
}
func (o topologyOutcome) presentation() topologyreport.Device {
	value := topologyreport.Device{
		Name: o.device.Name, Named: o.named, Host: o.device.Host, Vendor: o.device.Vendor, Status: string(o.state),
		ScriptFile: o.scriptFile, ScriptSHA256: o.scriptSHA256, DeviceReport: o.deviceReport,
		Comparisons: cloneTopologyComparisons(o.audit.Comparisons),
		Issues:      slices.Clone(o.audit.Issues), Commands: o.commands(),
	}
	if o.audited {
		value.Changes = new(o.audit.Changes())
	}
	if o.audited || o.applied {
		value.Steps = new(o.steps())
	}
	if o.diagnostic != (topologyDiagnostic{}) {
		value.Diagnostic = &topologyreport.Diagnostic{Code: o.diagnostic.code}
	}
	return value
}
