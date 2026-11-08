package workflow

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/gatework/go-net-connector/archive"
	"github.com/gatework/go-net-connector/config"
	"github.com/gatework/go-net-connector/fleet"
	"github.com/gatework/go-net-connector/inventory"
	"github.com/gatework/go-net-connector/storage"
	"github.com/gatework/go-net-connector/transfer"
)

func (r *Runner) TFTP(ctx context.Context, runtime Runtime, settings TFTPSettings, rows []inventory.Device, o TFTPOptions) (Result, error) {
	r.begin()
	if settings.Policy == nil {
		return r.fail(errors.New("workflow: TFTP policy is not configured"))
	}
	policy := settings.Policy
	var nonce [8]byte
	if _, e := rand.Read(nonce[:]); e != nil {
		return r.fail(e)
	}
	suffix := time.Now().UTC().Format("20060102T150405") + "-" + hex.EncodeToString(nonce[:])
	limit := settings.LimitPerVendor
	if o.All {
		limit = 0
	}
	plan, e := fleet.NewPlan(rows, runtime.Rules, fleet.PlanOptions{Mode: "tftp", LimitPerVendor: limit, AllowFixedNameReuse: true, Target: func(d fleet.Record) (transfer.Target, error) {
		target, e := policy.For(d.Host, d.Vendor)
		if e != nil {
			return transfer.Target{}, e
		}
		target.Timeout = settings.ArchiveTimeouts.Upload
		path, e := transfer.InventoryFilename(runtime.Registry, d.Vendor, d.Host, d.Name)
		if e != nil {
			return transfer.Target{}, e
		}
		target.Path = path
		if target.Root == "" {
			extension := filepath.Ext(path)
			tail := "-" + suffix + extension
			stem := strings.TrimSuffix(path, extension)
			if max := 220 - len(tail); len(stem) > max {
				stem = stem[:max]
			}
			target.Path = stem + tail
		}
		if target.Server == "" {
			return transfer.Target{}, &config.FieldError{Field: "tftp.server", Problem: "required"}
		}
		if e = transfer.Validate(runtime.Registry, d.Vendor, d.Host, target.TFTPOptions); e != nil {
			if errors.Is(e, transfer.ErrSourceFileRequired) {
				return transfer.Target{}, &config.FieldError{Field: "tftp.source_files." + d.Vendor, Problem: "required"}
			}
			return transfer.Target{}, e
		}
		return target, nil
	}})
	if e != nil {
		return r.fail(e)
	}
	view := PlanView(plan)
	if selected, ok := view["selected"].([]map[string]any); ok {
		for i, task := range plan.Tasks() {
			target := task.Target
			selected[i]["transfer"] = map[string]any{"server": nullableString(target.Server), "verification_root": nullableString(target.Root), "source_file": nullableString(target.SourceFile), "source_interface": nullableString(target.SourceInterface), "vrf": nullableString(target.VRF)}
		}
	}
	if o.Plan {
		result := Preview(plan)
		result.Preview.View = view
		return result, nil
	}
	_ = r.emit(PlanEvent{Total: len(plan.Entries()), Selected: len(plan.Tasks()), Workers: runtime.Workers, Mode: plan.Mode()})
	observer := r.observe()
	directory := runtime.Directory
	if o.Layout != "flat" {
		directory, e = storage.CreateBatchDirectory(directory, time.Now())
	} else {
		e = storage.PrepareDirectory(directory)
	}
	if e != nil {
		return r.fail(e)
	}
	// Prepare every archive before resolving any device credentials.
	_ = r.emit(PhaseEvent{Phase: PhaseArchivePreflight})
	pools := map[string]*archive.Pool{}
	archiveDirectories := []string{}
	defer func() {
		for _, pool := range pools {
			_ = pool.Close()
		}
	}()
	archiveRoots := []string{}
	archiveTargets := map[string]transfer.Target{}
	for _, task := range plan.Tasks() {
		target := task.Target
		if target.Root == "" || pools[target.Root] != nil || archiveTargets[target.Root].Root != "" {
			continue
		}
		archiveRoots = append(archiveRoots, target.Root)
		archiveTargets[target.Root] = target
	}
	for index, root := range archiveRoots {
		target := archiveTargets[root]
		password := ""
		if settings.ArchivePassword != nil {
			password = settings.ArchivePassword()
		}
		pool, e := archive.NewPool(archive.PoolOptions{Root: target.Root, Directory: directory, WorkerPath: settings.ArchiveWorkerPath, Password: password, Parallel: runtime.Workers, Timeouts: settings.ArchiveTimeouts})
		if e != nil {
			return r.fail(errors.Join(ErrTFTPArchivePreflight, e))
		}
		pools[root] = pool
		if e = pool.PrepareWithProgress(ctx, func(prepared archive.PrepareProgress) {
			_ = r.emit(ArchivePreflightEvent{Progress: prepared, Group: index + 1, Groups: len(archiveRoots)})
		}); e != nil {
			return r.fail(errors.Join(ErrTFTPArchivePreflight, e))
		}
		archiveDirectories = append(archiveDirectories, pool.ArchiveDirectory())
		_ = r.emit(ArchiveReadyEvent{Server: target.Server, Directory: pool.ArchiveDirectory()})
	}
	data, _ := json.MarshalIndent(view, "", "  ")
	if _, e = storage.Write(ctx, filepath.Join(directory, "plan.json"), data); e != nil {
		return r.fail(e)
	}
	events, e := openBatchJournal(directory, r.outcome)
	if e != nil {
		return r.fail(e)
	}
	defer events.Close()
	_ = r.emit(PhaseEvent{Phase: PhaseTransfer, Timeout: settings.ArchiveTimeouts.Upload})
	batch, runErr := fleet.Run(ctx, plan, fleet.RunOptions{Workers: runtime.Workers, Run: func(ctx context.Context, task fleet.Task) (artifact fleet.Artifact, err error) {
		_ = r.emit(StartEvent{Host: task.Device.Host, Vendor: task.Device.Vendor})
		target := task.Target
		filename := target.Path
		upload := func(ctx context.Context, name string) (receipt transfer.Receipt, err error) {
			device, closeDevice, e := openDevice(ctx, runtime, task.Device, runtime.logDir("tftp", directory), observer)
			if e != nil {
				return receipt, e
			}
			defer func() {
				err = joinDeviceCleanup(err, closeDevice())
			}()
			opts := target.TFTPOptions
			opts.Path = name
			_ = r.emit(TransferStageEvent{Host: task.Device.Host, Stage: "upload"})
			result, e := transfer.TFTP(ctx, runtime.Registry, device, opts)
			if result.Receipt.Reported() {
				_ = r.emit(TransferStageEvent{Host: task.Device.Host, Stage: "archive"})
			}
			return result.Receipt, e
		}
		var receipt transfer.Receipt
		if pool := pools[target.Root]; pool != nil {
			name, e := task.Device.BackupFilename("hostname_ip")
			if e != nil {
				return artifact, e
			}
			name = strings.TrimSuffix(name, ".txt") + filepath.Ext(filename)
			result, uploadErr := pool.Upload(ctx, filename, name, upload)
			receipt, err = result.Transfer, uploadErr
			artifact.Archive = &result.Receipt
		} else {
			receipt, err = upload(ctx, filename)
		}
		if receipt.Reported() {
			artifact.Transfer = &receipt
		}
		return artifact, err
	}, OnOutcome: events.record,
	})
	report, e := fleet.NewReport(batch, o.Policy)
	if e != nil {
		return r.fail(e)
	}
	report = report.WithLogDirectory(runtime.logDir("tftp", directory))
	if e = events.Sync(); e != nil {
		report = report.WithReportError(e, "")
	}
	for _, pool := range pools {
		if e = pool.Close(); e != nil {
			report = report.WithReportError(e, "")
		}
	}
	report = fleet.WriteReport(context.WithoutCancel(ctx), report, directory, plan, runtime.Workers)
	r.result = Result{Success: report.Success(), Batch: &BatchResult{Directory: directory, Report: report, Plan: plan, Workers: runtime.Workers, ServerArchiveDirectories: archiveDirectories}}
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
