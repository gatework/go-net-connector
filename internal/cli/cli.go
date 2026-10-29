// Package cli hosts the four command entrypoints without process-global state.
package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gatework/go-net-connector/config"
	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/fleet"
	"github.com/gatework/go-net-connector/internal/bootstrap"
	"github.com/gatework/go-net-connector/inventory"
	"github.com/gatework/go-net-connector/netdisco"
	"github.com/gatework/go-net-connector/storage"
)

type Runtime struct {
	Args               []string
	Environment        map[string]string
	Input              io.Reader
	Output, Error      io.Writer
	CWD, Home, Program string
	Directories        []string
	Source             netdisco.Source
	DeviceFactory      bootstrap.DeviceFactory
}

func integerString(n int) string { return strconv.Itoa(n) }
func (r Runtime) Run(ctx context.Context, program string) int {
	if r.Input == nil {
		r.Input = os.Stdin
	}
	if r.Output == nil {
		r.Output = os.Stdout
	}
	if r.Error == nil {
		r.Error = os.Stderr
	}
	if r.Environment == nil {
		r.Environment = map[string]string{}
		for _, entry := range os.Environ() {
			k, v, _ := strings.Cut(entry, "=")
			r.Environment[k] = v
		}
	}
	o, e := parse(r.Args, program, r.Output)
	if e != nil {
		return r.fail(program, e)
	}
	if o.help {
		return 0
	}
	overrides, e := config.FromEnvironment(o.public)
	if e != nil {
		return r.fail(program, e)
	}
	defaults := config.Defaults(o.mode)
	if program == "net-backup" && !o.all && o.sample == 0 {
		defaults["backup"].(map[string]any)["limit_per_vendor"] = 5
	}
	if o.all {
		delete(defaults["backup"].(map[string]any), "limit_per_vendor")
	}
	name := "backup"
	if o.mode == "topology" {
		name = "topology"
	}
	policy, e := config.Load(config.Options{Name: name, CWD: r.CWD, Home: r.Home, Program: r.Program, Directories: r.Directories, ConfigFile: o.config, EnvFile: o.envFile, Environment: r.Environment, Overrides: overrides, Defaults: defaults, AllowMissing: o.init || o.interactive})
	if e != nil {
		return r.fail(program, e)
	}
	if o.init || o.interactive {
		createConfig, createEnv := true, true
		reader := bufio.NewReader(r.Input)
		if o.init {
			configPath, envPath := policy.InitPaths()
			createConfig, e = missingSetupFile(configPath)
			if e != nil {
				return r.fail(program, e)
			}
			createEnv, e = missingSetupFile(envPath)
			if e != nil {
				return r.fail(program, e)
			}
			if o.interactive && (createConfig || createEnv) {
				if _, ok := r.terminal(); !ok {
					return r.fail(program, config.ErrInvalid)
				}
			}
		} else if o.interactive {
			createConfig, createEnv, e = r.setupConsent(policy, reader)
			if errors.Is(e, errSetupDeclined) {
				return 1
			}
			if e != nil {
				return r.fail(program, e)
			}
		}
		if o.init || createConfig || createEnv {
			if o.interactive {
				if createConfig {
					policy, e = r.configure(policy, o, name, reader)
				}
				if e != nil {
					return r.fail(program, e)
				}
			}
			created, e := config.InitializeSelected(policy, name, createConfig, createEnv)
			if e != nil {
				return r.fail(program, e)
			}
			for _, path := range created {
				fmt.Fprintln(r.Error, "Initialized:", path)
			}
			fmt.Fprintln(r.Error, "Review configuration and run again; no inventory or devices were accessed.")
			return 0
		}
	}
	if o.show {
		data := policy.Public()
		data["sources"] = policy.Sources()
		return r.print(data)
	}
	if o.export != "" {
		directory, _ := policy.Value("backup.directory").(string)
		if o.output != "" {
			_, e = storage.ExportSaved(ctx, directory, o.export, o.output)
			if e == nil {
				return r.print(map[string]any{"host": o.export, "output": o.output})
			}
		} else {
			var b []byte
			b, e = storage.ReadSaved(ctx, directory, o.export)
			if e == nil {
				var n int
				n, e = r.Output.Write(b)
				if e == nil && n != len(b) {
					e = io.ErrShortWrite
				}
			}
		}
		if e != nil {
			return r.fail(program, e)
		}
		return 0
	}
	if o.mode == "topology" {
		if e := validateTopologyPolicy(policy, o); e != nil {
			return r.fail(program, e)
		}
	}
	if e := r.passwords(&o); e != nil {
		return r.fail(program, e)
	}
	env := policy.Environment()
	for key, value := range o.secrets {
		suffix := ""
		switch key {
		case "NC_DEVICE_USERNAME":
			suffix = "USERNAME"
		case "NC_DEVICE_PASSWORD":
			suffix = "PASSWORD"
		case "NC_ENABLE_PASSWORD":
			suffix = "ENABLE_PASSWORD"
		}
		if suffix != "" {
			for name := range env {
				if strings.HasPrefix(name, "NC_") && strings.HasSuffix(name, "_"+suffix) && (suffix != "PASSWORD" || !strings.HasSuffix(name, "_ENABLE_PASSWORD")) {
					delete(env, name)
				}
			}
		}
		if key == "ND_USERNAME" || key == "ND_PASSWORD" {
			delete(env, "ND_API_KEY")
		}
		env[key] = value
	}
	reader := bufio.NewReader(r.Input)
	if o.stdin {
		line, e := readLine(reader, 64<<10)
		if e != nil {
			return r.fail(program, e)
		}
		var creds map[string]string
		if json.Unmarshal([]byte(line), &creds) != nil {
			return r.fail(program, config.ErrInvalid)
		}
		for key, target := range map[string]string{"netdisco_username": "ND_USERNAME", "netdisco_password": "ND_PASSWORD", "device_username": "NC_DEVICE_USERNAME", "device_password": "NC_DEVICE_PASSWORD"} {
			if strings.TrimSpace(creds[key]) == "" {
				return r.fail(program, config.ErrInvalid)
			}
			if target == "NC_DEVICE_USERNAME" || target == "NC_DEVICE_PASSWORD" {
				suffix := strings.TrimPrefix(target, "NC_DEVICE_")
				for name := range env {
					if strings.HasPrefix(name, "NC_") && strings.HasSuffix(name, "_"+suffix) && !strings.HasSuffix(name, "_ENABLE_PASSWORD") {
						delete(env, name)
					}
				}
			}
			env[target] = creds[key]
		}
		delete(env, "ND_API_KEY")
	}
	services, e := bootstrap.New(policy, func(key string) string { return env[key] })
	if e != nil {
		return r.fail(program, e)
	}
	defer services.Close()
	if r.DeviceFactory != nil {
		services = services.WithDeviceFactory(r.DeviceFactory)
	}
	var rows []inventory.Device
	if r.Source != nil {
		rows, e = r.Source.Devices(ctx)
	} else {
		var result netdisco.CacheResult
		result, e = services.Inventory(ctx, inventoryMode(policy, o))
		rows = result.Rows
		if result.RefreshErrorCode != "" {
			fmt.Fprintln(r.Error, "Inventory refresh failed; validated cache used")
		}
		if result.WriteErrorType != "" {
			fmt.Fprintln(r.Error, "Inventory cache write failed; fresh inventory retained")
		}
	}
	if e != nil {
		if ctx.Err() != nil {
			return 130
		}
		return r.fail(program, e)
	}
	if o.mode == "topology" {
		return r.runTopology(ctx, services, rows, o, reader)
	}
	if o.mode == "tftp" && !(o.plan && o.flags.Changed("inventory-only")) {
		return r.runTFTP(ctx, services, rows, o, reader, program)
	}
	limit := services.Limit()
	if o.all {
		limit = 0
	}
	plan, e := fleet.NewPlan(rows, services.Rules(), fleet.PlanOptions{Mode: "backup", LimitPerVendor: limit})
	if e != nil {
		return r.fail(program, e)
	}
	if o.plan {
		return r.preview(plan, planView(plan, program), o, program)
	}
	if o.stdin {
		line, e := readLine(reader, 64)
		if e != nil || strings.TrimSpace(line) != "RUN" {
			return r.fail(program, config.ErrInvalid)
		}
	}
	return r.backup(ctx, services, plan, o, program)
}
func inventoryMode(policy config.Snapshot, o options) string {
	if o.refresh {
		return "refresh"
	}
	if o.flags.Changed("cache-only") || o.flags.Changed("cache-mode") {
		mode, _ := policy.Value("netdisco.cache_mode").(string)
		return mode
	}
	if o.mode == "topology" {
		mode, _ := policy.Value("topology.cache_mode").(string)
		return mode
	}
	return ""
}
func readLine(r *bufio.Reader, limit int) (string, error) {
	var b strings.Builder
	for {
		part, prefix, e := r.ReadLine()
		if e != nil {
			return "", config.ErrInvalid
		}
		if b.Len()+len(part) > limit {
			return "", config.ErrInvalid
		}
		b.Write(part)
		if !prefix {
			return b.String(), nil
		}
	}
}
func (r Runtime) print(v any) int {
	enc := json.NewEncoder(r.Output)
	enc.SetIndent("", "  ")
	if e := enc.Encode(v); e != nil {
		return r.fail("net-connector", e)
	}
	return 0
}
func (r Runtime) fail(program string, e error) int {
	if errors.Is(e, context.Canceled) {
		return 130
	}
	fmt.Fprintln(r.Error, program+": operation failed (invalid input, unavailable source, or I/O error)")
	return 2
}
func planView(p fleet.Plan, program string) map[string]any {
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
		if program == "net-backup" {
			style = "ip"
		}
		filename, _ := r.BackupFilename(style)
		if p.Mode() == "tftp" {
			filename = task.Target.Path
		}
		if program == "net-backup" {
			selected = append(selected, map[string]any{"host": r.Host, "name": nullable(r.Name), "connector": r.Vendor, "filename": filename})
		} else {
			row := map[string]any{"host": r.Host, "name": nullable(r.Name), "vendor": r.Vendor, "backup_filename": filename}
			if p.Mode() == "tftp" {
				row["transfer"] = task.Target
			}
			selected = append(selected, row)
		}
	}
	result := map[string]any{"total": len(p.Entries()), "ready": ready, "selected": selected}
	if program == "net-backup" {
		result["skipped"] = skipped
	} else {
		result["vendors"], result["issues"] = vendors, issues
	}
	return result
}
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func (r Runtime) backup(ctx context.Context, services *bootstrap.Services, plan fleet.Plan, o options, program string) int {
	directory := services.Directory()
	if program != "net-backup" {
		var e error
		directory, e = storage.CreateBatchDirectory(directory, time.Now())
		if e != nil {
			return r.fail(program, e)
		}
	} else if e := storage.PrepareDirectory(directory); e != nil {
		return r.fail(program, e)
	}
	paths := map[int]string{}
	for _, task := range plan.Tasks() {
		style := "hostname_ip"
		if program == "net-backup" {
			style = "ip"
		}
		name, e := task.Device.BackupFilename(style)
		if e != nil {
			return r.fail(program, e)
		}
		path := filepath.Join(directory, name)
		if info, e := os.Lstat(path); e == nil && !info.Mode().IsRegular() {
			return r.fail(program, storage.ErrUnsafe)
		} else if e != nil && !os.IsNotExist(e) {
			return r.fail(program, e)
		}
		paths[task.Index] = path
	}
	data, _ := json.MarshalIndent(planView(plan, program), "", "  ")
	if _, e := storage.Write(ctx, filepath.Join(directory, "plan.json"), data); e != nil {
		return r.fail(program, e)
	}
	events, e := storage.OpenAppend(filepath.Join(directory, "events.jsonl"))
	if e != nil {
		return r.fail(program, e)
	}
	defer events.Close()
	batch, runErr := fleet.Run(ctx, plan, fleet.RunOptions{Workers: services.Workers(), Run: func(ctx context.Context, task fleet.Task) (artifact fleet.Artifact, err error) {
		device, closeDevice, e := openDevice(ctx, services, task.Device, services.LogDirectory(program, "backup", directory))
		if e != nil {
			return artifact, e
		}
		defer func() {
			err = joinDeviceCleanup(err, closeDevice())
		}()
		receipt, e := device.Backup(ctx, connector.BackupOptions{Path: paths[task.Index], RejectSymlink: true, LockTimeout: 5 * time.Second})
		if receipt.Path != "" {
			artifact.Backup = &receipt
		}
		return artifact, e
	}, OnOutcome: func(_ context.Context, out fleet.Outcome) error {
		var progressErr error
		if !o.quiet {
			_, progressErr = fmt.Fprintf(r.Error, "%s: %s\n", out.Device.Host, out.Status)
		}
		eventErr := json.NewEncoder(events).Encode(map[string]any{"host": out.Device.Host, "vendor": out.Device.Vendor, "artifact": artifactView(out.Artifact), "status": out.Status, "duration_ms": out.Duration.Milliseconds(), "error_code": nullable(out.ErrorCode), "error_type": nullable(out.ErrorType)})
		return errors.Join(progressErr, eventErr)
	}})
	report, e := fleet.NewReport(batch, o.policy)
	report = report.WithLogDirectory(services.LogDirectory(program, "backup", directory))
	if e != nil {
		return r.fail(program, e)
	}
	if e = events.Sync(); e != nil {
		report = report.WithReportError(e, "")
	}
	report = fleet.WriteReport(context.WithoutCancel(ctx), report, directory, plan, services.Workers())
	outputErr := r.finishReport(report, directory, o, program, ctx.Err() != nil)
	if outputErr != nil && report.Statistics()["report_error"] == nil {
		location, _ := report.Statistics()["report_location"].(string)
		report = fleet.WriteReport(context.WithoutCancel(ctx), report.WithReportError(outputErr, location), directory, plan, services.Workers())
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
