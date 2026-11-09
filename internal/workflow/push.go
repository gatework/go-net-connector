package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/fleet"
	"github.com/gatework/go-net-connector/parallel"
	"github.com/gatework/go-net-connector/storage"
	"github.com/gatework/go-net-connector/vendors/dialect"
)

// PushStatus is the per-device outcome of a script push. Its values appear in
// push reports and progress output.
type PushStatus string

const (
	// PushStatusPushed means every command was accepted and saving was not requested.
	PushStatusPushed PushStatus = "pushed"
	// PushStatusSaved means every command was accepted and the platform confirmed the save.
	PushStatusSaved PushStatus = "saved"
	// PushStatusSaveUnconfirmed means every command was accepted but the save
	// lacked affirmative device evidence. The running configuration changed.
	PushStatusSaveUnconfirmed PushStatus = "save_unconfirmed"
	// PushStatusRejected means the device rejected a command; earlier commands
	// remain applied and later commands were not sent.
	PushStatusRejected PushStatus = "rejected"
	// PushStatusFailed covers connection, authentication, timeout and transport failures.
	PushStatusFailed PushStatus = "failed"
	// PushStatusStopped means the device was not started because the failure
	// limit was reached. No connection was opened.
	PushStatusStopped PushStatus = "stopped"
	// PushStatusNotStarted means cancellation arrived before the device started.
	PushStatusNotStarted PushStatus = "not_started"
)

// Succeeded reports whether s is a complete push outcome.
func (s PushStatus) Succeeded() bool { return s == PushStatusPushed || s == PushStatusSaved }

// PushScripts selects the script for each canonical platform key. A platform
// without its own script uses Default; with neither, the device is skipped.
type PushScripts struct {
	Default    *connector.Script
	ByPlatform map[string]connector.Script
}

// For returns the script that applies to platform.
func (s PushScripts) For(platform string) (connector.Script, bool) {
	if script, ok := s.ByPlatform[platform]; ok {
		return script, true
	}
	if s.Default != nil {
		return *s.Default, true
	}
	return connector.Script{}, false
}

// PushOptions controls a script push. Scripts must already be parsed with
// connector.ParseScript so no device is contacted for an invalid script.
type PushOptions struct {
	Scripts PushScripts
	// Save runs the platform save script after a fully accepted push.
	Save bool
	// StopAfterFailures stops starting new devices once this many devices
	// have failed. Devices already running finish; zero never stops.
	StopAfterFailures int
	// Plan previews targets and scripts without connecting.
	Plan bool
	// Directory receives plan.json, report.json and session logs.
	Directory string
}

// PushPlanDevice is one device selected for a push.
type PushPlanDevice struct {
	Host     string  `json:"host"`
	Name     *string `json:"name"`
	Platform string  `json:"platform"`
	Script   string  `json:"script"`
	Commands int     `json:"commands"`
}

// PushPlan lists selected devices and the inventory entries that will not be pushed.
type PushPlan struct {
	Total    int              `json:"total"`
	Selected []PushPlanDevice `json:"selected"`
	Skipped  map[string]int   `json:"skipped"`
	Save     bool             `json:"save"`
}

// PushAttempt records how far one command got, including failed attempts.
type PushAttempt struct {
	Line     int  `json:"line"`
	Written  int  `json:"written"`
	Complete bool `json:"complete"`
	Failed   bool `json:"failed"`
}

// PushOutcome is the evidence retained for one device. AcceptedCommands
// counts only commands whose responses were accepted; Attempts also include
// the failed command so a partial write is never mistaken for no write.
type PushOutcome struct {
	Host             string        `json:"host"`
	Name             *string       `json:"name"`
	Platform         string        `json:"platform"`
	Script           string        `json:"script"`
	Status           PushStatus    `json:"status"`
	Commands         int           `json:"commands"`
	AcceptedCommands int           `json:"accepted_commands"`
	Attempts         []PushAttempt `json:"attempts"`
	ErrorCode        string        `json:"error_code,omitempty"`
	ErrorPhase       string        `json:"error_phase,omitempty"`
	FailedLine       int           `json:"failed_line,omitempty"`
	Duration         float64       `json:"duration"`
}

// PushReport is written to report.json after the push finishes.
type PushReport struct {
	Success     bool           `json:"success"`
	Interrupted bool           `json:"interrupted"`
	Stopped     bool           `json:"stopped"`
	Save        bool           `json:"save"`
	Directory   string         `json:"directory"`
	Counts      map[string]int `json:"counts"`
	Devices     []PushOutcome  `json:"devices"`
	ReportError string         `json:"report_error,omitempty"`
}

// PushResult carries the push plan and, after a run, its report.
type PushResult struct {
	Plan   PushPlan
	Report *PushReport
}

// Push sends one validated script per selected device and, when requested,
// saves the configuration. A failed device is never retried, so no write is
// replayed after an uncertain result.
func (r *Runner) Push(ctx context.Context, runtime Runtime, plan fleet.Plan, o PushOptions) (Result, error) {
	r.begin()
	if o.StopAfterFailures < 0 {
		return r.fail(ErrInvalidPush)
	}
	pushPlan, tasks := newPushPlan(plan, o, runtime.Registry)
	r.result.Push = &PushResult{Plan: pushPlan}
	_ = r.emit(PlanEvent{Total: pushPlan.Total, Selected: len(tasks), Workers: runtime.Workers, Mode: "push"})
	if o.Plan {
		return r.complete(len(tasks) > 0), nil
	}
	directory, e := storage.CreateBatchDirectory(o.Directory, time.Now())
	if e != nil {
		return r.fail(e)
	}
	data, _ := json.MarshalIndent(pushPlan, "", "  ")
	if _, e := storage.Write(ctx, filepath.Join(directory, "plan.json"), data); e != nil {
		return r.fail(e)
	}
	observer := r.observe()
	var failures atomic.Int64
	results, runErr := parallel.Map(ctx, runtime.Workers, tasks, func(ctx context.Context, task pushTask) (PushOutcome, error) {
		if o.StopAfterFailures > 0 && failures.Load() >= int64(o.StopAfterFailures) {
			return task.outcome(PushStatusStopped), nil
		}
		_ = r.emit(StartEvent{Host: task.device.Host, Vendor: task.device.Vendor})
		out := r.pushDevice(ctx, runtime, task, o.Save, runtime.logDir("push", directory), observer)
		if !out.Status.Succeeded() {
			failures.Add(1)
		}
		_ = r.emit(OutcomeEvent{Host: out.Host, Status: string(out.Status), ErrorCode: out.ErrorCode, Duration: time.Duration(out.Duration * float64(time.Second))})
		return out, nil
	})
	report := &PushReport{Save: o.Save, Directory: directory, Counts: map[string]int{}, Interrupted: ctx.Err() != nil, Success: len(tasks) > 0}
	for i, task := range tasks {
		out := task.outcome(PushStatusNotStarted)
		if i < len(results) && results[i].Started && results[i].Err == nil {
			out = results[i].Value
		}
		report.Stopped = report.Stopped || out.Status == PushStatusStopped
		report.Success = report.Success && out.Status.Succeeded()
		report.Counts[string(out.Status)]++
		report.Devices = append(report.Devices, out)
	}
	report.Success = report.Success && !report.Interrupted
	r.result.Push.Report = report
	data, _ = json.MarshalIndent(report, "", "  ")
	if _, e := storage.Write(context.WithoutCancel(ctx), filepath.Join(directory, "report.json"), data); e != nil {
		report.Success = false
		report.ReportError = "IOError"
	}
	r.finishBatch(ctx)
	result := r.complete(report.Success)
	if runErr != nil && !errors.Is(runErr, context.Canceled) && !errors.Is(runErr, context.DeadlineExceeded) {
		return result, runErr
	}
	return result, nil
}

// ErrInvalidPush reports push options that cannot be executed.
var ErrInvalidPush = errors.New("workflow: invalid push options")

type pushTask struct {
	device fleet.Record
	script connector.Script
}

func (t pushTask) outcome(status PushStatus) PushOutcome {
	return PushOutcome{
		Host: t.device.Host, Name: optionalString(t.device.Name), Platform: t.device.Vendor,
		Script: t.script.Name, Status: status, Commands: len(t.script.Commands), Attempts: []PushAttempt{},
	}
}

// newPushPlan keeps the fleet plan's selection and adds the per-platform
// script choice. Ready devices without a script, or that cannot save when
// saving is requested, are skipped and never connected: changing a device
// that is then known to be unsaveable would leave unpersisted changes.
func newPushPlan(plan fleet.Plan, o PushOptions, registry platformLookup) (PushPlan, []pushTask) {
	view := PushPlan{Total: len(plan.Entries()), Selected: []PushPlanDevice{}, Skipped: map[string]int{}, Save: o.Save}
	for _, e := range plan.Entries() {
		if e.Status != "ready" {
			view.Skipped[e.Status]++
		}
	}
	tasks := []pushTask{}
	for _, task := range plan.Tasks() {
		script, ok := o.Scripts.For(task.Device.Vendor)
		if !ok || len(script.Commands) == 0 {
			view.Skipped["missing_script"]++
			continue
		}
		if o.Save && !canSave(registry, task.Device.Vendor) {
			view.Skipped["save_unsupported"]++
			continue
		}
		tasks = append(tasks, pushTask{device: task.Device, script: script})
		view.Selected = append(view.Selected, PushPlanDevice{
			Host: task.Device.Host, Name: optionalString(task.Device.Name), Platform: task.Device.Vendor,
			Script: script.Name, Commands: len(script.Commands),
		})
	}
	return view, tasks
}

// platformLookup is the registry capability push planning needs.
type platformLookup interface {
	Lookup(name string) (dialect.Platform, error)
}

func canSave(registry platformLookup, platform string) bool {
	if registry == nil {
		return false
	}
	p, err := registry.Lookup(platform)
	return err == nil && len(p.Profile().SaveCommands) > 0
}

func (r *Runner) pushDevice(ctx context.Context, runtime Runtime, task pushTask, save bool, logs string, observer SessionObserver) (out PushOutcome) {
	out = task.outcome(PushStatusFailed)
	start := time.Now()
	defer func() { out.Duration = time.Since(start).Seconds() }()
	device, closeDevice, err := openDevice(ctx, runtime, task.device, logs, observer)
	if err != nil {
		out.ErrorCode, out.ErrorPhase = pushErrorCode(err)
		return out
	}
	defer func() {
		if cleanup := closeDevice(); cleanup != nil && out.ErrorCode == "" {
			// The device result stands; only record that cleanup failed.
			out.ErrorCode, out.ErrorPhase = "cleanup_failed", "cleanup"
		}
	}()
	if err = device.Connect(ctx); err != nil {
		out.ErrorCode, out.ErrorPhase = pushErrorCode(err)
		return out
	}
	err = device.WithOperation(ctx, func(ctx context.Context, op *connector.Operation) error {
		result, err := op.ExecuteScript(ctx, task.script, connector.ExecuteOptions{})
		out.AcceptedCommands = len(result.Steps())
		for _, attempt := range result.Attempts() {
			out.Attempts = append(out.Attempts, PushAttempt{Line: attempt.Command().Line, Written: attempt.Written(), Complete: attempt.Complete(), Failed: attempt.Err() != nil})
			if attempt.Err() != nil && out.FailedLine == 0 {
				out.FailedLine = attempt.Command().Line
			}
		}
		if err != nil {
			out.Status = PushStatusFailed
			if errors.Is(err, connector.ErrDevice) {
				out.Status = PushStatusRejected
			}
			out.ErrorCode, out.ErrorPhase = pushErrorCode(err)
			return err
		}
		if !save {
			out.Status = PushStatusPushed
			return nil
		}
		// Only Profile.ValidateSave evidence counts as saved; a prompt alone does not.
		if _, err = op.SaveConfig(ctx, connector.ExecuteOptions{}); err != nil {
			out.Status = PushStatusSaveUnconfirmed
			out.ErrorCode, out.ErrorPhase = pushErrorCode(err)
			return err
		}
		out.Status = PushStatusSaved
		return nil
	})
	if err != nil && out.ErrorCode == "" {
		out.ErrorCode, out.ErrorPhase = pushErrorCode(err)
	}
	return out
}

// pushErrorCode exposes only classified metadata, never error text that could
// contain device output or secrets.
func pushErrorCode(err error) (code, phase string) {
	if classified, ok := errors.AsType[*connector.Error](err); ok {
		return classified.Code, classified.Phase
	}
	switch {
	case errors.Is(err, fleet.ErrMissingCredentials):
		return "missing_credentials", "credentials"
	case errors.Is(err, connector.ErrUnconfirmed):
		return "persistence_unconfirmed", "persist"
	case errors.Is(err, connector.ErrUnsupported):
		return "unsupported_operation", "save"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "interrupted", "operation"
	}
	return "internal_error", "operation"
}
