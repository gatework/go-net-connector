package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/fleet"
	"github.com/gatework/go-net-connector/storage"
)

// PlanView is the JSON preview of a batch plan written to plan.json and
// printed by plan commands. Field order follows the established sorted keys.
type PlanView struct {
	Issues   map[string]int  `json:"issues"`
	Ready    int             `json:"ready"`
	Selected []PlannedDevice `json:"selected"`
	Skipped  map[string]int  `json:"skipped"`
	Total    int             `json:"total"`
	Vendors  map[string]int  `json:"vendors"`
}

// PlannedDevice is one selected device in a plan preview.
type PlannedDevice struct {
	BackupFilename string           `json:"backup_filename"`
	Host           string           `json:"host"`
	Name           *string          `json:"name"`
	Transfer       *PlannedTransfer `json:"transfer,omitempty"`
	Vendor         string           `json:"vendor"`
}

// PlannedTransfer is the resolved TFTP target of a planned device. Unset
// values are null so a preview distinguishes them from empty strings.
type PlannedTransfer struct {
	Server           *string `json:"server"`
	SourceFile       *string `json:"source_file"`
	SourceInterface  *string `json:"source_interface"`
	VerificationRoot *string `json:"verification_root"`
	VRF              *string `json:"vrf"`
}

// NewPlanView summarizes a plan without opening devices.
func NewPlanView(p fleet.Plan) PlanView {
	view := PlanView{Issues: map[string]int{}, Skipped: map[string]int{}, Vendors: map[string]int{}, Selected: []PlannedDevice{}, Total: len(p.Entries())}
	for _, e := range p.Entries() {
		if e.Device.Issue == "" {
			view.Ready++
		} else {
			view.Issues[e.Device.Issue]++
		}
		v := e.Device.Vendor
		if v == "" {
			v = "unmapped"
		}
		view.Vendors[v]++
		if e.Status != "ready" {
			view.Skipped[e.Status]++
		}
	}
	tftp := p.Mode() == "tftp"
	for _, task := range p.Tasks() {
		r := task.Device
		row := PlannedDevice{Host: r.Host, Name: optionalString(r.Name), Vendor: r.Vendor}
		row.BackupFilename, _ = r.BackupFilename("hostname_ip")
		if tftp {
			target := task.Target
			row.BackupFilename = target.Path
			row.Transfer = &PlannedTransfer{
				Server: optionalString(target.Server), SourceFile: optionalString(target.SourceFile),
				SourceInterface: optionalString(target.SourceInterface), VerificationRoot: optionalString(target.Root),
				VRF: optionalString(target.VRF),
			}
		}
		view.Selected = append(view.Selected, row)
	}
	return view
}

// optionalString maps an unset value to a JSON null.
func optionalString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (r *Runner) Backup(ctx context.Context, runtime Runtime, plan fleet.Plan, o BackupOptions) (Result, error) {
	r.begin()
	_ = r.emit(PlanEvent{Total: len(plan.Entries()), Selected: len(plan.Tasks()), Workers: runtime.Workers, Mode: plan.Mode()})
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
	data, _ := json.MarshalIndent(NewPlanView(plan), "", "  ")
	if _, e := storage.Write(ctx, filepath.Join(directory, "plan.json"), data); e != nil {
		return r.fail(e)
	}
	events, e := openBatchJournal(directory, r.outcome)
	if e != nil {
		return r.fail(e)
	}
	defer events.Close()
	batch, runErr := fleet.Run(ctx, plan, fleet.RunOptions{Workers: runtime.Workers, Run: func(ctx context.Context, task fleet.Task) (artifact fleet.Artifact, err error) {
		_ = r.emit(StartEvent{Host: task.Device.Host, Vendor: task.Device.Vendor})
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
	r.finishBatch(ctx)
	if ctx.Err() != nil || errors.Is(runErr, context.Canceled) {
		return r.result, context.Canceled
	}
	if runErr != nil {
		return r.fail(runErr)
	}
	if r.result.Success {
		return r.complete(true), nil
	}
	return r.complete(false), nil
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
