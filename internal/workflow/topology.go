package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gatework/go-net-connector/fleet"
	"github.com/gatework/go-net-connector/inventory"
	"github.com/gatework/go-net-connector/parallel"
	"github.com/gatework/go-net-connector/storage"
	"github.com/gatework/go-net-connector/topology"
)

var topologyWhitespace = regexp.MustCompile(`\s+`)

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
	r.begin()
	mode := settings.Mode
	if mode != "plan" && mode != "discover" && mode != "apply" {
		return r.fail(ErrInvalidRequest)
	}
	// The inventory plan owns duplicate-address classification for every CLI.
	plan, e := fleet.NewPlan(rows, runtime.Rules, fleet.PlanOptions{Mode: "backup"})
	if e != nil {
		return r.fail(e)
	}
	devices := []fleet.Record{}
	inventoryRows := []topologyInventoryEntry{}
	issues := map[string]int{}
	unexpected := false
	for _, entry := range plan.Entries() {
		record := entry.Device
		issue := record.Issue
		if entry.Status == "duplicate_host" {
			issue = entry.Status
		}
		inventoryRows = append(inventoryRows, topologyInventoryEntry{record.Host, record.Vendor, issue})
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
		r.result = Result{Success: len(devices) > 0, Topology: &TopologyResult{Inventory: plan.Entries(), Report: &TopologyReport{preview: &topologyPreview{len(rows), len(devices), issues, devices}}}}
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
	_ = r.emit(PlanEvent{Total: len(rows), Selected: len(devices), Workers: workers, Mode: "topology"})
	observer := r.observe()
	format := settings.DescriptionFormat
	if format == "" {
		format = "To_%{neighbor}_%{port}"
	}
	abbreviate := settings.AbbreviatePorts
	lower := settings.LowercasePorts
	prefixes := append([]string(nil), settings.NeighborNamePrefixes...)
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
		if e := json.NewEncoder(eventsFile).Encode(out.row(false)); e != nil {
			out.eventError = true
			if eventError == nil {
				eventError = e
			}
		}
		if e := r.emit(OutcomeEvent{Host: out.device.Host, Status: string(out.state), ErrorCode: out.diagnostic.code, Duration: out.duration}); e != nil {
			out.progressError = true
			if progressError == nil {
				progressError = e
			}
		}
	}
	execute := func(ctx context.Context, d fleet.Record, previous *topologyOutcome) (out topologyOutcome, err error) {
		_ = r.emit(StartEvent{Host: d.Host, Vendor: d.Vendor})
		start := time.Now()
		out = topologyOutcome{device: d, state: topologyFailed}
		if previous != nil {
			out = *previous
		}
		defer func() {
			out.startedAt, out.finishedAt, out.duration = start, time.Now(), time.Since(start)
			if err != nil {
				if out.result.Persisted {
					out.state = topologyPersistedWithError
				} else if errors.Is(err, fleet.ErrMissingCredentials) {
					out.state = topologyMissingCredentials
				} else if ctx.Err() != nil {
					out.state = topologyInterrupted
				} else {
					if out.steps() > 0 {
						out.state = topologyPartial
					} else {
						out.state = topologyFailed
					}
				}
				var te *topology.Error
				if errors.As(err, &te) {
					out.diagnostic = topologyDiagnostic{te.Code, te.Phase, "Net::Connector::Error"}
				} else {
					out.diagnostic = topologyDiagnostic{errorType: "StandardError"}
				}
			}
			out.sessionLog = filepath.Join(runtime.logDir("topology", directory), strings.ReplaceAll(d.Host, ":", "_")+".log")
			notify(&out)
		}()
		declaration, lookupErr := runtime.Registry.Lookup(d.Vendor)
		if lookupErr != nil || !supportsTopologyMode(declaration, mode) {
			out.state = topologyUnsupported
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
			out.result, out.applied = result, true
			out.state = topologyApplied
			if len(out.audit.Issues) > 0 {
				out.state = topologyPartialApplied
			}
			return out, e
		}
		audit, e := t.Audit(ctx, opts)
		out.audit, out.audited = audit, true
		out.plan = audit.Plan
		if mode == "discover" {
			out.plan = nil
		}
		if e != nil {
			return out, e
		}
		status := topologyDiscovered
		if !audit.Complete() {
			status = topologyUnverified
			if out.plan != nil && len(out.plan.Changes) > 0 && mode != "discover" {
				status = topologyPartialPlanned
			}
		} else if mode != "discover" {
			if audit.ManualRequired {
				status = topologyManualRequired
			} else if out.plan != nil {
				status = topologyPlanned
			}
		}
		out.state = status
		return out, nil
	}
	collected, collectErr := parallel.Map(ctx, workers, devices, func(ctx context.Context, d fleet.Record) (topologyOutcome, error) { return execute(ctx, d, nil) })
	outcomes := make([]topologyOutcome, len(devices))
	for i, item := range collected {
		if item.Started && item.Value.state != "" {
			outcomes[i] = item.Value
		} else {
			outcomes[i] = topologyOutcome{device: devices[i], state: topologyNotStarted}
			if item.Started {
				outcomes[i].state = topologyFailed
				outcomes[i].diagnostic = topologyDiagnostic{code: "internal_error", phase: "callback"}
			}
		}
	}
	// Preserve every collected audit before any later persistence can fail.
	report := &TopologyReport{Mode: mode, Directory: directory, inventory: inventoryRows, outcomes: outcomes}
	r.result = Result{Topology: &TopologyResult{Directory: directory, Outcomes: makeTopologyOutcomes(outcomes), Report: report}}
	// All review artifacts are durable before any application task is admitted.
	_ = r.emit(PhaseEvent{Phase: PhaseTopologyPlan})
	if e = saveTopologyPlan(context.WithoutCancel(ctx), directory, outcomes, runtime.Registry); e != nil {
		report.ReportError = "IOError"
		_ = persistTopologySummary(context.WithoutCancel(ctx), directory, report)
		return r.fail(e)
	}
	if mode == "apply" && collectErr == nil {
		pending := 0
		for _, out := range outcomes {
			if out.plan != nil && len(out.plan.Changes) > 0 && out.state.permitsApply() {
				pending++
			}
		}
		_ = r.emit(PhaseEvent{Phase: PhaseTopologyApply})
		_ = r.emit(BeginPhaseEvent{Total: pending, Workers: workers})
		applied, applyErr := parallel.Map(ctx, workers, outcomes, func(ctx context.Context, out topologyOutcome) (topologyOutcome, error) {
			if out.plan == nil || !out.state.permitsApply() {
				return out, nil
			}
			if len(out.plan.Changes) == 0 {
				out.state = topologyUnchanged
				notify(&out)
				return out, nil
			}
			return execute(ctx, out.device, &out)
		})
		for i, item := range applied {
			if item.Started && item.Value.state != "" {
				outcomes[i] = item.Value
			} else if item.Started {
				outcomes[i].state = topologyFailed
				outcomes[i].diagnostic = topologyDiagnostic{code: "internal_error", phase: "callback"}
			}
		}
		collectErr = applyErr
	}
	report.Interrupted = ctx.Err() != nil
	report.summarize(unexpected)
	if collectErr != nil {
		report.OrchestrationError = "StandardError"
		report.Success = false
	}
	if e = eventsFile.Sync(); eventError == nil {
		eventError = e
	}
	if eventError != nil || progressError != nil {
		report.Success = false
		report.ReportError = "IOError"
	}
	if progressError != nil {
		report.ProgressError = "IOError"
	}
	if mode == "apply" {
		for _, out := range outcomes {
			if out.state == topologyNotStarted {
				continue
			}
			if e = writeTopologyDeviceReport(context.WithoutCancel(ctx), directory, out); e != nil {
				report.Success = false
				report.ReportError = "IOError"
				break
			}
		}
	}
	_ = persistTopologySummary(context.WithoutCancel(ctx), directory, report)
	r.result = Result{Success: report.Success, Topology: &TopologyResult{Directory: directory, Report: report, Outcomes: makeTopologyOutcomes(outcomes)}}
	r.finishBatch(ctx)
	if ctx.Err() != nil {
		return r.result, context.Canceled
	}
	if report.Success {
		return r.complete(true), nil
	}
	return r.complete(false), nil
}
