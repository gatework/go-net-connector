package cli

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/gatework/go-net-connector/archive"
	"github.com/gatework/go-net-connector/config"
	"github.com/gatework/go-net-connector/internal/bootstrap"
	"github.com/gatework/go-net-connector/inventory"
	"github.com/gatework/go-net-connector/netdisco"
	"github.com/gatework/go-net-connector/storage"
	"github.com/gatework/go-net-connector/transfer"
)

func (r Runtime) runTFTP(ctx context.Context, s *bootstrap.Services, rows []inventory.Device, o options, reader *bufio.Reader, program string) int {
	policy, e := s.TFTPPolicy()
	if e != nil {
		return r.fail(program, e)
	}
	effective := map[string]transfer.Effective{}
	limit := s.Limit()
	if o.all {
		limit = 0
	}
	plan, e := netdisco.NewPlan(rows, s.Rules(), netdisco.PlanOptions{Mode: "tftp", LimitPerVendor: limit, AllowFixedNameReuse: true, Target: func(d netdisco.Record) (netdisco.Target, error) {
		target, e := policy.For(d.Host, d.Vendor)
		if e != nil {
			return netdisco.Target{}, e
		}
		path, e := transfer.InventoryFilename(d.Vendor, d.Host, d.Name)
		if e != nil {
			return netdisco.Target{}, e
		}
		target.Path = path
		if target.Server == "" {
			return netdisco.Target{}, config.ErrInvalid
		}
		if e = transfer.Validate(d.Vendor, d.Host, target.TFTPOptions); e != nil {
			return netdisco.Target{}, e
		}
		if d.Vendor == "palo_alto" && target.Root == "" {
			return netdisco.Target{}, transfer.ErrInvalid
		}
		effective[d.Host] = target
		return netdisco.Target{Server: target.Server, Path: path}, nil
	}})
	if e != nil {
		return r.fail(program, e)
	}
	view := planView(plan, program)
	if selected, ok := view["selected"].([]map[string]any); ok && program != "net-backup" {
		for _, row := range selected {
			target := effective[row["host"].(string)]
			row["transfer"] = map[string]any{"server": nullable(target.Server), "verification_root": nullable(target.Root), "source_file": nullable(target.SourceFile), "source_interface": nullable(target.SourceInterface), "vrf": nullable(target.VRF)}
		}
	}
	if o.plan {
		return r.preview(plan, view, o, program)
	}
	if o.stdin {
		line, e := readLine(reader, 64)
		if e != nil || strings.TrimSpace(line) != "RUN" {
			return r.fail(program, config.ErrInvalid)
		}
	}
	directory := s.Directory()
	if program != "net-backup" {
		directory, e = storage.CreateBatchDirectory(directory, time.Now())
	} else {
		e = storage.PrepareDirectory(directory)
	}
	if e != nil {
		return r.fail(program, e)
	}
	// Prepare every archive before resolving any device credentials.
	pools := map[string]*archive.Pool{}
	defer func() {
		for _, pool := range pools {
			_ = pool.Close()
		}
	}()
	worker, _ := s.Policy().Value("tftp.ssh_worker_path").(string)
	for _, task := range plan.Tasks() {
		target := effective[task.Device.Host]
		if target.Root == "" {
			continue
		}
		if pools[target.Root] != nil {
			continue
		}
		pool, e := archive.NewPool(archive.PoolOptions{Root: target.Root, Directory: directory, WorkerPath: worker, Password: s.ArchivePassword(), Parallel: s.Workers()})
		if e != nil {
			return r.fail(program, e)
		}
		pools[target.Root] = pool
		if e = pool.Prepare(ctx); e != nil {
			return r.fail(program, e)
		}
	}
	var nonce [8]byte
	if _, e = rand.Read(nonce[:]); e != nil {
		return r.fail(program, e)
	}
	suffix := time.Now().UTC().Format("20060102T150405") + "-" + hex.EncodeToString(nonce[:])
	data, _ := json.MarshalIndent(view, "", "  ")
	if _, e = storage.Write(ctx, filepath.Join(directory, "plan.json"), data); e != nil {
		return r.fail(program, e)
	}
	events, e := storage.OpenAppend(filepath.Join(directory, "events.jsonl"))
	if e != nil {
		return r.fail(program, e)
	}
	defer events.Close()
	batch, runErr := netdisco.Run(ctx, plan, netdisco.FleetOptions{Workers: s.Workers(), Run: func(ctx context.Context, task netdisco.Task) (artifact netdisco.Artifact, err error) {
		target := effective[task.Device.Host]
		filename := target.Path
		if target.Root == "" {
			extension := filepath.Ext(filename)
			stem := strings.TrimSuffix(filename, extension)
			tail := "-" + suffix + extension
			if max := 220 - len(tail); len(stem) > max {
				stem = stem[:max]
			}
			filename = stem + tail
		}
		upload := func(ctx context.Context, name string) (receipt transfer.Receipt, err error) {
			device, closeDevice, e := openDevice(ctx, s, task.Device, s.LogDirectory(program, "tftp", directory))
			if e != nil {
				return receipt, e
			}
			defer func() {
				if e := closeDevice(); err == nil {
					err = e
				}
			}()
			opts := target.TFTPOptions
			opts.Path = name
			result, e := transfer.TFTP(ctx, device, opts)
			return result.Receipt, e
		}
		var receipt transfer.Receipt
		if pool := pools[target.Root]; pool != nil {
			name, e := task.Device.BackupFilename("hostname_ip")
			if e != nil {
				return artifact, e
			}
			name = strings.TrimSuffix(name, ".txt") + filepath.Ext(filename)
			receipt, err = pool.Upload(ctx, filename, name, upload)
		} else {
			receipt, err = upload(ctx, filename)
		}
		if receipt.Reported() {
			artifact.Transfer = &receipt
		}
		return artifact, err
	}, OnOutcome: func(_ context.Context, out netdisco.Outcome) error {
		var progressErr error
		if !o.quiet {
			_, progressErr = fmt.Fprintf(r.Error, "%s: %s\n", out.Device.Host, out.Status)
		}
		eventErr := json.NewEncoder(events).Encode(map[string]any{"host": out.Device.Host, "vendor": out.Device.Vendor, "artifact": artifactView(out.Artifact), "status": out.Status, "duration_ms": out.Duration.Milliseconds(), "error_code": nullable(out.ErrorCode), "error_type": nullable(out.ErrorType)})
		return errors.Join(progressErr, eventErr)
	}})
	report, e := netdisco.NewReport(batch, o.policy)
	if e != nil {
		return r.fail(program, e)
	}
	report = report.WithLogDirectory(s.LogDirectory(program, "tftp", directory))
	if e = events.Sync(); e != nil {
		report = report.WithReportError(e, "")
	}
	for _, pool := range pools {
		if e = pool.Close(); e != nil {
			report = report.WithReportError(e, "")
		}
	}
	report = netdisco.WriteReport(context.WithoutCancel(ctx), report, directory, plan, s.Workers())
	outputErr := r.finishReport(report, directory, o, program, ctx.Err() != nil)
	if outputErr != nil && report.Statistics()["report_error"] == nil {
		location, _ := report.Statistics()["report_location"].(string)
		report = netdisco.WriteReport(context.WithoutCancel(ctx), report.WithReportError(outputErr, location), directory, plan, s.Workers())
	}
	if ctx.Err() != nil || errors.Is(runErr, context.Canceled) {
		return 130
	}
	if outputErr != nil {
		return 2
	}
	if runErr != nil {
		return r.fail(program, runErr)
	}
	if report.Success() {
		return 0
	}
	return 1
}
