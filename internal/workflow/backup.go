package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/fleet"
	"github.com/gatework/go-net-connector/storage"
)

func PlanView(p fleet.Plan) map[string]any {
	ready := 0
	skipped := map[string]int{}
	issues := map[string]int{}
	vendors := map[string]int{}
	selected := []map[string]any{}
	for _, e := range p.Entries() {
		if e.Device.Issue == "" {
			ready++
		} else {
			issues[e.Device.Issue]++
		}
		v := e.Device.Vendor
		if v == "" {
			v = "unmapped"
		}
		vendors[v]++
		if e.Status != "ready" {
			skipped[e.Status]++
		}
	}
	for _, task := range p.Tasks() {
		r := task.Device
		style := "hostname_ip"
		filename, _ := r.BackupFilename(style)
		if p.Mode() == "tftp" {
			filename = task.Target.Path
		}

		row := map[string]any{"host": r.Host, "name": nullableString(r.Name), "vendor": r.Vendor, "backup_filename": filename}
		if p.Mode() == "tftp" {
			row["transfer"] = task.Target
		}
		selected = append(selected, row)
	}
	result := map[string]any{"total": len(p.Entries()), "ready": ready, "selected": selected}
	result["vendors"], result["issues"], result["skipped"] = vendors, issues, skipped
	return result
}
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func (r *Runner) Backup(ctx context.Context, runtime Runtime, plan fleet.Plan, o BackupOptions) (Result, error) {
	r.result = Result{}
	p := r.reporter()
	_ = p.Plan(len(plan.Entries()), len(plan.Tasks()), runtime.Workers, plan.Mode())
	observer := r.observe()
	directory := runtime.Directory
	if o.Layout != "flat" {
		var e error
		directory, e = storage.CreateBatchDirectory(directory, time.Now())
		if e != nil {
			return r.fail(e)
		}
	} else if e := storage.PrepareDirectory(directory); e != nil {
		return r.fail(e)
	}
	paths := map[int]string{}
	for _, task := range plan.Tasks() {
		style := "hostname_ip"
		name, e := task.Device.BackupFilename(style)
		if e != nil {
			return r.fail(e)
		}
		path := filepath.Join(directory, name)
		if info, e := os.Lstat(path); e == nil && !info.Mode().IsRegular() {
			return r.fail(storage.ErrUnsafe)
		} else if e != nil && !os.IsNotExist(e) {
			return r.fail(e)
		}
		paths[task.Index] = path
	}
	data, _ := json.MarshalIndent(PlanView(plan), "", "  ")
	if _, e := storage.Write(ctx, filepath.Join(directory, "plan.json"), data); e != nil {
		return r.fail(e)
	}
	var progress io.Writer
	progress = r.Progress
	events, e := openBatchJournal(directory, progress)
	if e != nil {
		return r.fail(e)
	}
	defer events.Close()
	events.reporter = p
	events.progress = nil
	batch, runErr := fleet.Run(ctx, plan, fleet.RunOptions{Workers: runtime.Workers, Run: func(ctx context.Context, task fleet.Task) (artifact fleet.Artifact, err error) {
		_ = p.Start(task.Device.Host, task.Device.Vendor)
		device, closeDevice, e := openDevice(ctx, runtime, task.Device, runtime.logDir("backup", directory), observer)
		if e != nil {
			return artifact, e
		}
		defer func() {
			err = joinDeviceCleanup(err, closeDevice())
		}()
		receipt, e := device.Backup(ctx, connector.BackupOptions{TextOnly: true, PreserveInvalidEncoding: true, Path: paths[task.Index], RejectSymlink: true, LockTimeout: 5 * time.Second})
		if receipt.Path != "" {
			artifact.Backup = &receipt
		}
		return artifact, e
	}, OnOutcome: events.record,
	})
	report, e := fleet.NewReport(batch, o.Policy)
	report = report.WithLogDirectory(runtime.logDir("backup", directory))
	if e != nil {
		return r.fail(e)
	}
	if e = events.Sync(); e != nil {
		report = report.WithReportError(e, "")
	}
	report = fleet.WriteReport(context.WithoutCancel(ctx), report, directory, plan, runtime.Workers)
	r.result = Result{Success: report.Success(), Batch: &BatchResult{Directory: directory, Report: report, Plan: plan, Workers: runtime.Workers}}
	r.finishBatch()
	if ctx.Err() != nil || errors.Is(runErr, context.Canceled) {
		return r.result, context.Canceled
	}
	if runErr != nil {
		return r.fail(runErr)
	}
	if report.Success() {
		return r.complete(true), nil
	}
	return r.complete(false), nil
}
