package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/fleet"
	"github.com/gatework/go-net-connector/internal/batchreport"
	"github.com/gatework/go-net-connector/parallel"
	"github.com/gatework/go-net-connector/storage"
	"github.com/gatework/go-net-connector/transfer"
	"github.com/gatework/go-net-connector/vendors/dialect"
)

// Stage is one step of the per-device business pipeline, such as executing a
// script, saving the configuration or backing it up. Stages of one device run
// in order inside one session; the first failure stops every later stage.
type Stage interface {
	Name() string
	// Admit decides at plan time, without I/O, whether the platform supports
	// this stage. A non-empty result is the skip status of the device.
	Admit(record fleet.Record, platform dialect.Platform) (skip string)
	// Run performs the stage. It records evidence even when it fails, and calls
	// Evidence.MarkPartial once the device or the local disk has been changed.
	Run(ctx context.Context, s *Session, e *Evidence) error
}

// Planner is an optional Stage method that describes the planned target of a
// device in plan output, without I/O.
type Planner interface {
	Plan(task fleet.Task, platform dialect.Platform, target *batchreport.Target)
}

// Operation is a named composition of stages applied to every admitted device.
type Operation struct {
	Name   string
	Stages []Stage
	// Outcome names a fully successful device result, such as "backed_up".
	Outcome string
	// Then is an optional second round that starts only after every device
	// finished Stages, such as applying reviewed topology changes.
	Then *Round
	// Tolerate lists extra device statuses that do not fail the selected and
	// verified policies for this operation.
	Tolerate []string
	// Finish may adjust the summarized report, for example to report pending
	// changes as an incomplete batch.
	Finish func(*batchreport.Report)
	// Limit caps the number of admitted devices; the rest are sample_limit.
	Limit int
}

// Round is a second pipeline round over devices from the first round.
type Round struct {
	Stages []Stage
	// Before runs once between the rounds, with every device's evidence; an
	// error stops the round from starting (for example, unsaved review files).
	Before func(ctx context.Context, rows []batchreport.Device, evidence []*Evidence) error
	// Admit selects the devices that continue into the round.
	Admit func(row batchreport.Device, e *Evidence) bool
}

// Evidence accumulates what each stage proved for one device.
type Evidence struct {
	Artifacts batchreport.Artifacts
	// Outcome overrides Operation.Outcome for a successful device.
	Outcome string
	// Verified marks server-side verification evidence (the verified policy).
	Verified bool
	partial  bool
	review   bool
}

// MarkReview records a result that is not a failure but needs operator
// review, such as an incomplete topology audit; the device is partial.
func (e *Evidence) MarkReview() { e.review = true }

// MarkPartial records that a stage changed the device or saved an artifact, so
// a later failure is a partial result rather than a clean failure.
func (e *Evidence) MarkPartial() { e.partial = true }

// Session is one device's lazily opened and authenticated connection, owned by
// the runner. A stage that must open the connection inside another transaction
// (a TFTP archive) calls Device at that point.
type Session struct {
	Record   fleet.Record
	Target   transfer.Target
	Platform dialect.Platform
	runtime  Runtime
	logs     string
	observer SessionObserver
	device   *connector.Device
	close    func() error
	auth     *batchreport.Stage
	authErr  error
	emit     func(Event) error
}

// Device opens and authenticates the connection on first use. Login failures
// are recorded as the authenticate stage and returned on every later call.
func (s *Session) Device(ctx context.Context) (*connector.Device, error) {
	if s.device != nil || s.authErr != nil {
		return s.device, s.authErr
	}
	start := time.Now()
	device, closeDevice, err := openDevice(ctx, s.runtime, s.Record, s.logs, s.observer)
	if err == nil {
		s.device, s.close = device, closeDevice
		err = device.Connect(ctx)
	}
	stage := batchreport.Stage{Name: "authenticate", Status: "succeeded", DurationMS: milliseconds(time.Since(start))}
	if err != nil {
		diagnostic := classify(err, "authenticate")
		stage.Status, stage.Diagnostic = "failed", &diagnostic
		s.authErr = err
	}
	s.auth = &stage
	return s.device, err
}

// Stage reports progress inside a long stage, such as "upload" or "archive".
func (s *Session) Stage(name string) {
	_ = s.emit(TransferStageEvent{Host: s.Record.Host, Stage: name})
}

func (s *Session) finish() error {
	if s.close == nil {
		return nil
	}
	return s.close()
}

// ExecuteOptions controls one pipeline run.
type ExecuteOptions struct {
	Directory string
	Policy    string
	// StopAfterFailures stops starting new devices once this many devices have
	// failed. Running devices finish; zero never stops.
	StopAfterFailures int
}

// intentionalSkips are inventory entries the operator excluded on purpose;
// they do not fail the selected or verified policies.
var intentionalSkips = map[string]bool{"filtered": true, "sample_limit": true, "missing_script": true}

type pipelineTask struct {
	entry int
	task  fleet.Task
}

// Execute runs op over every ready plan entry and writes the batch directory:
// plan.json, events.jsonl and report.json. Failed devices are never retried,
// so no device write is replayed after an uncertain result.
func (r *Runner) Execute(ctx context.Context, runtime Runtime, plan fleet.Plan, op Operation, o ExecuteOptions) (batchreport.Report, error) {
	started := time.Now().UTC()
	report := batchreport.Report{
		SchemaVersion: batchreport.SchemaVersion, Operation: op.Name, Policy: o.Policy,
		Directory: o.Directory, StartedAt: started.Format(time.RFC3339Nano), Workers: runtime.Workers,
		Counts: map[string]int{}, SessionLogs: runtime.logDir(op.Name, o.Directory),
	}
	if o.StopAfterFailures < 0 {
		return report, ErrInvalidRequest
	}
	tasks := r.admitTasks(runtime, plan, op, &report)
	entries := plan.Entries()
	if err := writeJSON(ctx, filepath.Join(o.Directory, "plan.json"), report); err != nil {
		return report, err
	}
	journal, err := storage.OpenAppend(filepath.Join(o.Directory, "events.jsonl"))
	if err != nil {
		return report, err
	}
	defer journal.Close()
	_ = r.emit(PlanEvent{Total: len(entries), Selected: len(tasks), Workers: runtime.Workers, Mode: op.Name})
	b := &batch{runner: r, runtime: runtime, op: op, options: o, report: &report, tasks: tasks, evidence: make([]*Evidence, len(tasks)), journal: journal}
	runErr := b.round(ctx, op.Stages, func(int) bool { return true })
	if op.Then != nil && runErr == nil && ctx.Err() == nil {
		runErr = b.then(ctx)
	}
	if e := journal.Sync(); e != nil && b.journalErr == nil {
		b.journalErr = e
	}
	report.Interrupted = ctx.Err() != nil
	finished := time.Now().UTC()
	report.FinishedAt = finished.Format(time.RFC3339Nano)
	report.DurationMS = milliseconds(finished.Sub(started))
	summarize(&report, len(tasks), op.Tolerate)
	if op.Finish != nil {
		op.Finish(&report)
	}
	if b.journalErr != nil {
		report.Success, report.ReportError = false, "journal_failed"
	}
	if e := writeJSON(context.WithoutCancel(ctx), filepath.Join(o.Directory, "report.json"), report); e != nil {
		report.Success, report.ReportError = false, "report_write_failed"
	}
	if runErr != nil && !errors.Is(runErr, context.Canceled) && !errors.Is(runErr, context.DeadlineExceeded) {
		return report, runErr
	}
	return report, nil
}

// batch is one Execute run: the admitted tasks, their evidence, the shared
// failure count and the event journal.
type batch struct {
	runner     *Runner
	runtime    Runtime
	op         Operation
	options    ExecuteOptions
	report     *batchreport.Report
	tasks      []pipelineTask
	evidence   []*Evidence
	journal    *os.File
	journalMu  sync.Mutex
	journalErr error
	failures   atomic.Int64
}

// record appends row to the journal and emits its outcome event.
func (b *batch) record(row batchreport.Device) {
	b.journalMu.Lock()
	if e := json.NewEncoder(b.journal).Encode(row); e != nil && b.journalErr == nil {
		b.journalErr = e
	}
	b.journalMu.Unlock()
	event := OutcomeEvent{Host: row.Host, Status: row.Status, Outcome: row.Outcome, ErrorCode: diagnosticCode(row.Diagnostic), Duration: time.Duration(row.DurationMS * float64(time.Millisecond)), TextWarningBytes: textWarningBytes(row)}
	if a := row.Artifacts.Backup; a != nil {
		event.SourceCommitted, event.SourceVerified = a.Saved, a.Complete
	}
	_ = b.runner.emit(event)
}

// round runs stages on the admitted tasks in parallel and stores each
// returned row in the report.
func (b *batch) round(ctx context.Context, stages []Stage, admitted func(i int) bool) error {
	order := make([]int, 0, len(b.tasks))
	for i := range b.tasks {
		if admitted(i) {
			order = append(order, i)
		}
	}
	results, err := parallel.Map(ctx, b.runtime.Workers, order, func(ctx context.Context, i int) (batchreport.Device, error) {
		return b.runTask(ctx, stages, i), nil
	})
	for k, i := range order {
		entry := b.tasks[i].entry
		switch {
		case k >= len(results) || !results[k].Started:
		case results[k].Err == nil:
			b.report.Devices[entry] = results[k].Value
		default:
			// The worker exited without returning (for example a callback
			// called runtime.Goexit): the device started, so it failed.
			row := b.report.Devices[entry]
			row.Status, row.FailedStage = batchreport.StatusFailed, "worker"
			row.Diagnostic = &batchreport.Diagnostic{Code: "internal_error", Phase: "worker"}
			b.report.Devices[entry] = row
		}
	}
	if errors.Is(err, parallel.ErrWorkerExit) {
		b.report.ReportError = "worker_exited"
		return nil
	}
	return err
}

// runTask runs stages for task i unless the failure limit was reached.
func (b *batch) runTask(ctx context.Context, stages []Stage, i int) batchreport.Device {
	t := b.tasks[i]
	row := b.report.Devices[t.entry]
	if limit := b.options.StopAfterFailures; limit > 0 && b.failures.Load() >= int64(limit) {
		row.Status = batchreport.StatusStopped
		return row
	}
	if b.evidence[i] == nil {
		b.evidence[i] = &Evidence{}
	}
	row = b.runner.runDevice(ctx, b.runtime, b.op, stages, t.task, row, b.evidence[i], b.report.SessionLogs)
	if row.Status == batchreport.StatusFailed || row.Status == batchreport.StatusInterrupted {
		b.failures.Add(1)
	}
	b.record(row)
	return row
}

// then runs the operation's follow-up phase on the devices it admits and
// applies the outcomes it recorded.
func (b *batch) then(ctx context.Context) error {
	then, report, tasks := b.op.Then, b.report, b.tasks
	rows := make([]batchreport.Device, len(tasks))
	for i, t := range tasks {
		rows[i] = report.Devices[t.entry]
		if b.evidence[i] == nil {
			b.evidence[i] = &Evidence{}
		}
	}
	var err error
	if then.Before != nil {
		if err = then.Before(context.WithoutCancel(ctx), rows, b.evidence); err != nil {
			report.ReportError = "review_write_failed"
		}
	}
	if err == nil {
		_ = b.runner.emit(BeginPhaseEvent{Total: len(tasks), Workers: b.runtime.Workers})
		err = b.round(ctx, then.Stages, func(i int) bool {
			row := report.Devices[tasks[i].entry]
			return row.Status != batchreport.StatusFailed && row.Status != batchreport.StatusInterrupted && then.Admit(row, b.evidence[i])
		})
	}
	for i, t := range tasks {
		if e := b.evidence[i]; e != nil && e.Outcome != "" && report.Devices[t.entry].Status == batchreport.StatusSucceeded {
			report.Devices[t.entry].Outcome = e.Outcome
		}
	}
	return err
}

// Plan returns the planned report for op without any I/O: every inventory
// entry with its skip status or not_started, and the planned target.
func (r *Runner) Plan(runtime Runtime, plan fleet.Plan, op Operation, policy string) batchreport.Report {
	report := batchreport.Report{SchemaVersion: batchreport.SchemaVersion, Operation: op.Name, Policy: policy, Workers: runtime.Workers, Counts: map[string]int{}}
	tasks := r.admitTasks(runtime, plan, op, &report)
	for _, device := range report.Devices {
		report.Counts[device.Status]++
	}
	report.Status = batchreport.BatchNoDevices
	if len(tasks) > 0 {
		report.Status = "planned"
	}
	report.Success = len(tasks) > 0
	return report
}

func (r *Runner) admitTasks(runtime Runtime, plan fleet.Plan, op Operation, report *batchreport.Report) []pipelineTask {
	entries := plan.Entries()
	report.Total = len(entries)
	report.Devices = make([]batchreport.Device, len(entries))
	byIndex := map[int]fleet.Task{}
	for _, task := range plan.Tasks() {
		byIndex[task.Index] = task
	}
	var tasks []pipelineTask
	for i, entry := range entries {
		report.Devices[i] = deviceRow(entry.Device, entry.Status)
		task, ready := byIndex[i]
		if !ready {
			continue
		}
		platform, err := runtime.Registry.Lookup(entry.Device.Vendor)
		if err != nil {
			report.Devices[i].Status = "unsupported_vendor"
			continue
		}
		if skip := admit(op, entry.Device, platform); skip != "" {
			report.Devices[i].Status = skip
			continue
		}
		target := &batchreport.Target{}
		for _, stage := range op.Stages {
			if planner, ok := stage.(Planner); ok {
				planner.Plan(task, platform, target)
			}
		}
		if *target != (batchreport.Target{}) {
			report.Devices[i].Target = target
		}
		if op.Limit > 0 && len(tasks) >= op.Limit {
			report.Devices[i].Status = "sample_limit"
			continue
		}
		report.Devices[i].Status = batchreport.StatusNotStarted
		tasks = append(tasks, pipelineTask{entry: i, task: task})
	}
	return tasks
}

func admit(op Operation, record fleet.Record, platform dialect.Platform) string {
	for _, stage := range op.Stages {
		if skip := stage.Admit(record, platform); skip != "" {
			return skip
		}
	}
	return ""
}

// runDevice runs every stage of op in one session. The first failing stage
// stops the pipeline; later stages are recorded as skipped.
func (r *Runner) runDevice(ctx context.Context, runtime Runtime, op Operation, stages []Stage, task fleet.Task, row batchreport.Device, evidence *Evidence, logs string) batchreport.Device {
	record := task.Device
	_ = r.emit(StartEvent{Host: record.Host, Vendor: record.Vendor})
	start := time.Now()
	platform, _ := runtime.Registry.Lookup(record.Vendor)
	session := &Session{Record: record, Target: task.Target, Platform: platform, runtime: runtime, logs: logs, observer: r.observe(), emit: r.emit}
	previous := row.DurationMS
	failedStage, failed := runStages(ctx, session, stages, evidence, &row)
	if cleanup := session.finish(); cleanup != nil && failed == nil {
		failed, failedStage = cleanup, "disconnect"
	}
	row.Artifacts, row.Verified = evidence.Artifacts, evidence.Verified
	if row.StartedAt == "" {
		row.StartedAt = start.UTC().Format(time.RFC3339Nano)
	}
	row.DurationMS = previous + milliseconds(time.Since(start))
	if session.device != nil && logs != "" {
		row.SessionLog = filepath.Join(logs, strings.ReplaceAll(record.Host, ":", "_")+".log")
	}
	settle(ctx, &row, op, evidence, failed, failedStage)
	return row
}

// runStages runs stages in order, recording each in row. After the first
// failure, including authentication during a stage, the remaining stages
// are recorded as skipped.
func runStages(ctx context.Context, session *Session, stages []Stage, evidence *Evidence, row *batchreport.Device) (failedStage string, failed error) {
	for _, stage := range stages {
		if failed != nil {
			row.Stages = append(row.Stages, batchreport.Stage{Name: stage.Name(), Status: "skipped"})
			continue
		}
		stageStart := time.Now()
		authBefore := session.auth
		err := stage.Run(ctx, session, evidence)
		if authBefore == nil && session.auth != nil {
			row.Stages = append(row.Stages, *session.auth)
			if session.authErr != nil {
				failed, failedStage = session.authErr, "authenticate"
				row.Stages = append(row.Stages, batchreport.Stage{Name: stage.Name(), Status: "skipped"})
				continue
			}
		}
		result := batchreport.Stage{Name: stage.Name(), Status: "succeeded", DurationMS: milliseconds(time.Since(stageStart))}
		if err != nil {
			diagnostic := classify(err, stage.Name())
			result.Status, result.Diagnostic = "failed", &diagnostic
			failed, failedStage = err, stage.Name()
		}
		row.Stages = append(row.Stages, result)
	}
	return failedStage, failed
}

// settle sets the device status, outcome and diagnostic from the first
// failure and the evidence the stages recorded.
func settle(ctx context.Context, row *batchreport.Device, op Operation, evidence *Evidence, failed error, failedStage string) {
	switch {
	case failed == nil && evidence.review:
		row.Status = batchreport.StatusPartial
		row.Outcome = evidence.Outcome
	case failed == nil:
		row.Status = batchreport.StatusSucceeded
		row.Outcome = op.Outcome
		if evidence.Outcome != "" {
			row.Outcome = evidence.Outcome
		}
	case ctx.Err() != nil && (errors.Is(failed, context.Canceled) || errors.Is(failed, context.DeadlineExceeded)):
		row.Status, row.FailedStage = batchreport.StatusInterrupted, failedStage
	case evidence.partial:
		row.Status, row.FailedStage = batchreport.StatusPartial, failedStage
	default:
		row.Status, row.FailedStage = batchreport.StatusFailed, failedStage
	}
	if failed != nil {
		diagnostic := classify(failed, failedStage)
		row.Diagnostic = &diagnostic
		if evidence.Outcome != "" {
			row.Outcome = evidence.Outcome
		}
	}
}

// summarize computes counts, batch status and the success policy.
//   - strict: every inventory entry succeeded.
//   - selected: at least one device succeeded and every other entry was an
//     intentional skip (filtered, sample limit, no script for its platform).
//   - verified: selected, and every succeeded device has verification evidence.
func summarize(report *batchreport.Report, admitted int, tolerate []string) {
	succeeded := 0
	selected := true
	allSucceeded := len(report.Devices) > 0
	for _, device := range report.Devices {
		report.Counts[device.Status]++
		switch {
		case device.Status == batchreport.StatusSucceeded:
			succeeded++
			if batchreport.SuccessPolicy(report.Policy) == batchreport.PolicyVerified && !device.Verified {
				selected = false
			}
		case intentionalSkips[device.Status] || slices.Contains(tolerate, device.Status):
			allSucceeded = false
		default:
			selected, allSucceeded = false, false
		}
		if device.Status == batchreport.StatusStopped {
			report.Stopped = true
		}
	}
	if batchreport.SuccessPolicy(report.Policy) == batchreport.PolicyStrict {
		report.Success = allSucceeded && !report.Interrupted
	} else {
		report.Success = succeeded > 0 && selected && !report.Interrupted
	}
	switch {
	case report.Interrupted:
		report.Status = batchreport.BatchInterrupted
	case admitted == 0:
		report.Status = batchreport.BatchNoDevices
	case report.Success:
		report.Status = batchreport.BatchSucceeded
	default:
		report.Status = batchreport.BatchIncomplete
	}
}

func deviceRow(record fleet.Record, status string) batchreport.Device {
	host := record.Host
	if host == "" {
		host = record.SourceIP
	}
	return batchreport.Device{Host: host, Name: record.Name, Platform: record.Vendor, SourceVendor: record.SourceVendor, Status: status}
}

func diagnosticCode(d *batchreport.Diagnostic) string {
	if d == nil {
		return ""
	}
	return d.Code
}

func textWarningBytes(row batchreport.Device) int {
	if b := row.Artifacts.Backup; b != nil && b.TextWarning != nil {
		return b.TextWarning.Count
	}
	return 0
}

func milliseconds(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

func writeJSON(ctx context.Context, path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	_, err = storage.Write(ctx, path, append(data, '\n'))
	return err
}
