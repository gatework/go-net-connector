package workflow

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/gatework/go-net-connector/archive"
	"github.com/gatework/go-net-connector/config"
	"github.com/gatework/go-net-connector/fleet"
	"github.com/gatework/go-net-connector/internal/batchreport"
	"github.com/gatework/go-net-connector/inventory"
	"github.com/gatework/go-net-connector/storage"
	"github.com/gatework/go-net-connector/topology"
	"github.com/gatework/go-net-connector/transfer"
	"github.com/gatework/go-net-connector/vendors/dialect"
)

// ErrTFTPArchivePreflight identifies preparation failures before any device upload.
var ErrTFTPArchivePreflight = errors.New("TFTP archive preflight failed")

// BackupOptions selects the success policy and output layout of a batch.
type BackupOptions struct {
	Policy, Layout string
	// Save persists the running configuration before collecting it.
	Save bool
}

func (r *Runner) batchDirectory(root, layout string) (string, error) {
	if batchreport.Layout(layout) == batchreport.LayoutFlat {
		return root, storage.PrepareDirectory(root)
	}
	return storage.CreateBatchDirectory(root, time.Now())
}

// BackupOperation collects configurations, optionally saving them first.
func BackupOperation(directory string, save bool) Operation {
	stages := []Stage{}
	if save {
		stages = append(stages, SaveStage{})
	}
	return Operation{Name: "backup", Outcome: "backed_up", Stages: append(stages, BackupStage{Directory: directory})}
}

// Backup collects every planned device's configuration into a batch directory.
func (r *Runner) Backup(ctx context.Context, runtime Runtime, plan fleet.Plan, o BackupOptions, preview bool) (Result, error) {
	if preview {
		report := r.Plan(runtime, plan, BackupOperation("", o.Save), o.Policy)
		return Result{Success: report.Success, Report: &report, Planned: true}, nil
	}
	directory, err := r.batchDirectory(runtime.Directory, o.Layout)
	if err != nil {
		return Result{}, err
	}
	return r.run(ctx, runtime, plan, BackupOperation(directory, o.Save), ExecuteOptions{Directory: directory, Policy: o.Policy}, nil)
}

func (r *Runner) run(ctx context.Context, runtime Runtime, plan fleet.Plan, op Operation, o ExecuteOptions, archives []string) (Result, error) {
	r.begin()
	report, err := r.Execute(ctx, runtime, plan, op, o)
	r.finish(&report)
	result := Result{Success: report.Success, Report: &report, ArchiveDirectories: archives}
	if ctx.Err() != nil {
		return result, context.Canceled
	}
	return result, err
}

// TFTPOptions controls a native TFTP backup.
type TFTPOptions struct {
	BackupOptions
	Plan, All bool
}

// TFTP makes devices upload their configuration files to a TFTP server and,
// with a verification root, verifies and retrieves every uploaded file.
func (r *Runner) TFTP(ctx context.Context, runtime Runtime, settings TFTPSettings, rows []inventory.Device, o TFTPOptions) (Result, error) {
	if settings.Policy == nil {
		return Result{}, errors.New("workflow: TFTP policy is not configured")
	}
	plan, err := r.tftpPlan(runtime, settings, rows, o)
	if err != nil {
		return Result{}, err
	}
	stage := TFTPStage{Registry: runtime.Registry, Pools: map[string]*archive.Pool{}}
	stages := []Stage{}
	if o.Save {
		stages = append(stages, SaveStage{})
	}
	op := Operation{Name: "tftp", Outcome: "uploaded", Stages: append(stages, stage)}
	if o.Plan {
		report := r.Plan(runtime, plan, op, o.Policy)
		return Result{Success: report.Success, Report: &report, Planned: true}, nil
	}
	directory, err := r.batchDirectory(runtime.Directory, o.Layout)
	if err != nil {
		return Result{}, err
	}
	// Prepare every archive before resolving any device credentials.
	_ = r.emit(PhaseEvent{Phase: PhaseArchivePreflight})
	defer func() {
		for _, pool := range stage.Pools {
			_ = pool.Close()
		}
	}()
	roots, targets := []string{}, map[string]transfer.Target{}
	for _, task := range plan.Tasks() {
		if root := task.Target.Root; root != "" && targets[root].Root == "" {
			roots, targets[root] = append(roots, root), task.Target
		}
	}
	archives := []string{}
	for index, root := range roots {
		password := ""
		if settings.ArchivePassword != nil {
			password = settings.ArchivePassword()
		}
		pool, err := archive.NewPool(archive.PoolOptions{Root: root, Directory: directory, WorkerPath: settings.ArchiveWorkerPath, Password: password, Parallel: runtime.Workers, Timeouts: settings.ArchiveTimeouts})
		if err != nil {
			return Result{}, errors.Join(ErrTFTPArchivePreflight, err)
		}
		stage.Pools[root] = pool
		if err = pool.Prepare(ctx, func(prepared archive.PrepareProgress) {
			_ = r.emit(ArchivePreflightEvent{Progress: prepared, Group: index + 1, Groups: len(roots)})
		}); err != nil {
			return Result{}, errors.Join(ErrTFTPArchivePreflight, err)
		}
		archives = append(archives, pool.ArchiveDirectory())
		_ = r.emit(ArchiveReadyEvent{Server: targets[root].Server, Directory: pool.ArchiveDirectory()})
	}
	_ = r.emit(PhaseEvent{Phase: PhaseTransfer, Timeout: settings.ArchiveTimeouts.Upload})
	result, err := r.run(ctx, runtime, plan, op, ExecuteOptions{Directory: directory, Policy: o.Policy}, archives)
	for _, pool := range stage.Pools {
		if closeErr := pool.Close(); closeErr != nil && result.Report != nil && result.Report.ReportError == "" {
			result.Report.ReportError, result.Report.Success, result.Success = "archive_close_failed", false, false
		}
	}
	return result, err
}

func (r *Runner) tftpPlan(runtime Runtime, settings TFTPSettings, rows []inventory.Device, o TFTPOptions) (fleet.Plan, error) {
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return fleet.Plan{}, err
	}
	suffix := time.Now().UTC().Format("20060102T150405") + "-" + hex.EncodeToString(nonce[:])
	limit := settings.LimitPerVendor
	if o.All {
		limit = 0
	}
	return fleet.NewPlan(rows, runtime.Rules, fleet.PlanOptions{Mode: "tftp", LimitPerVendor: limit, AllowFixedNameReuse: true, Target: func(d fleet.Record) (transfer.Target, error) {
		target, err := settings.Policy.For(d.Host, d.Vendor)
		if err != nil {
			return transfer.Target{}, err
		}
		target.Timeout = settings.ArchiveTimeouts.Upload
		path, err := transfer.InventoryFilename(runtime.Registry, d.Vendor, d.Host, d.Name)
		if err != nil {
			return transfer.Target{}, err
		}
		target.Path = path
		if target.Root == "" {
			extension := filepath.Ext(path)
			tail := "-" + suffix + extension
			stem := strings.TrimSuffix(path, extension)
			if maximum := 220 - len(tail); len(stem) > maximum {
				stem = stem[:maximum]
			}
			target.Path = stem + tail
		}
		if target.Server == "" {
			return transfer.Target{}, &config.FieldError{Field: "tftp.server", Problem: "required"}
		}
		if err = transfer.Validate(runtime.Registry, d.Vendor, d.Host, target.TFTPOptions); err != nil {
			if errors.Is(err, transfer.ErrSourceFileRequired) {
				return transfer.Target{}, &config.FieldError{Field: "tftp.source_files." + d.Vendor, Problem: "required"}
			}
			return transfer.Target{}, err
		}
		return target, nil
	}})
}

// DeployOptions controls a script deployment.
type DeployOptions struct {
	Scripts DeployScripts
	// Save persists the configuration after every command was accepted.
	Save bool
	// Backup collects the configuration after a successful deployment.
	Backup bool
	// StopAfterFailures stops starting new devices once this many failed.
	StopAfterFailures int
	Plan              bool
	Directory, Policy string
}

// DeployOperation composes execute → save → backup for one session per device.
func DeployOperation(o DeployOptions, directory string) Operation {
	stages := []Stage{ExecuteStage{Scripts: o.Scripts}}
	if o.Save {
		stages = append(stages, SaveStage{})
	}
	if o.Backup {
		stages = append(stages, BackupStage{Directory: directory})
	}
	return Operation{Name: "deploy", Outcome: "deployed", Stages: stages}
}

// Deploy sends validated scripts to every planned device. A failed device is
// never retried, so no write is replayed after an uncertain result.
func (r *Runner) Deploy(ctx context.Context, runtime Runtime, plan fleet.Plan, o DeployOptions) (Result, error) {
	if o.StopAfterFailures < 0 {
		return Result{}, ErrInvalidRequest
	}
	if o.Plan {
		report := r.Plan(runtime, plan, DeployOperation(o, ""), o.Policy)
		return Result{Success: report.Success, Report: &report, Planned: true}, nil
	}
	directory, err := storage.CreateBatchDirectory(o.Directory, time.Now())
	if err != nil {
		return Result{}, err
	}
	return r.run(ctx, runtime, plan, DeployOperation(o, directory), ExecuteOptions{Directory: directory, Policy: o.Policy, StopAfterFailures: o.StopAfterFailures}, nil)
}

// TopologyOptions controls a topology run.
type TopologyOptions struct {
	InventoryOnly bool
	Limit         int
}

// TopologyEvidence is the per-device topology artifact in the report.
type TopologyEvidence struct {
	Audit        topology.Audit   `json:"audit"`
	Plan         *topology.Plan   `json:"plan,omitempty"`
	Result       *topology.Result `json:"result,omitempty"`
	ScriptFile   string           `json:"script_file,omitempty"`
	ScriptSHA256 string           `json:"script_sha256,omitempty"`
}

var topologyWhitespace = regexp.MustCompile(`\s+`)

// Topology audits neighbor-derived interface descriptions and, in apply mode,
// applies the reviewed changes after every plan has been saved.
func (r *Runner) Topology(ctx context.Context, runtime Runtime, settings TopologySettings, rows []inventory.Device, o TopologyOptions) (Result, error) {
	mode := settings.Mode
	if mode != "plan" && mode != "discover" && mode != "apply" {
		return Result{}, ErrInvalidRequest
	}
	plan, err := fleet.NewPlan(rows, runtime.Rules, fleet.PlanOptions{Mode: "backup"})
	if err != nil {
		return Result{}, err
	}
	if o.InventoryOnly {
		return Result{Success: len(plan.Tasks()) > 0, Inventory: plan.Entries()}, nil
	}
	if settings.Workers > 0 {
		runtime.Workers = settings.Workers
	}
	limit := o.Limit
	if limit == 0 {
		limit = settings.Limit
	}
	directory, err := storage.CreateBatchDirectory(settings.Directory, time.Now())
	if err != nil {
		return Result{}, err
	}
	op := Operation{
		Name: "topology", Outcome: "discovered", Limit: limit,
		Stages:   []Stage{AuditStage{Mode: mode, Registry: runtime.Registry, Options: topologyFormat(settings)}},
		Tolerate: []string{"unsupported_operation"},
		Finish: func(report *batchreport.Report) {
			if mode == "plan" && pendingChanges(report) {
				report.Success, report.Status = false, batchreport.BatchIncomplete
			}
		},
	}
	if mode == "apply" {
		op.Then = &Round{
			Stages: []Stage{ApplyStage{Registry: runtime.Registry}},
			Before: func(ctx context.Context, rows []batchreport.Device, evidence []*Evidence) error {
				_ = r.emit(PhaseEvent{Phase: PhaseTopologyPlan})
				return saveTopologyReview(ctx, directory, rows, evidence, runtime.Registry)
			},
			Admit: func(_ batchreport.Device, e *Evidence) bool {
				t, _ := e.Artifacts.Topology.(*TopologyEvidence)
				return t != nil && t.Plan != nil && len(t.Plan.Changes) > 0 && (e.Outcome == "planned" || e.Outcome == "partial_planned")
			},
		}
	}
	return r.run(ctx, runtime, plan, op, ExecuteOptions{Directory: directory, Policy: string(batchreport.PolicySelected)}, nil)
}

func pendingChanges(report *batchreport.Report) bool {
	for _, device := range report.Devices {
		if t, ok := device.Artifacts.Topology.(*TopologyEvidence); ok && t.Plan != nil && len(t.Plan.Changes) > 0 {
			return true
		}
	}
	return false
}

func topologyFormat(settings TopologySettings) topology.FormatOptions {
	format := settings.DescriptionFormat
	if format == "" {
		format = "To_%{neighbor}_%{port}"
	}
	abbreviate, lower := settings.AbbreviatePorts, settings.LowercasePorts
	prefixes := append([]string(nil), settings.NeighborNamePrefixes...)
	formatter := func(n topology.Neighbor) (string, error) {
		port := n.NeighborInterface
		if abbreviate {
			port = topology.ShortInterface(port, lower)
		} else if lower {
			port = strings.ToLower(port)
		}
		return strings.ReplaceAll(strings.ReplaceAll(format, "%{neighbor}", topologyWhitespace.ReplaceAllString(n.NeighborName, "_")), "%{port}", topologyWhitespace.ReplaceAllString(port, "_")), nil
	}
	return topology.FormatOptions{FullInterfaceNames: !abbreviate, Lowercase: lower, Formatter: formatter, DescriptionMatcher: func(current, expected string, n topology.Neighbor) bool {
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
}

func supportsTopologyMode(platform dialect.Platform, mode string) bool {
	if _, ok := platform.(topology.NeighborDiscovery); !ok {
		return false
	}
	if _, ok := platform.(topology.DescriptionReader); !ok {
		return false
	}
	if _, ok := platform.(topology.InterfaceIdentity); !ok {
		return false
	}
	if mode == "apply" {
		_, ok := platform.(topology.DescriptionWriter)
		return ok
	}
	return true
}

// AuditStage discovers neighbors and descriptions and compares them with the
// expected descriptions; outside discover mode it also plans the changes.
type AuditStage struct {
	Mode     string
	Registry dialect.Registry
	Options  topology.FormatOptions
}

// Name returns the stable stage identifier recorded in reports.
func (AuditStage) Name() string { return "audit" }

// Admit checks whether the device and platform support this stage before I/O.
func (a AuditStage) Admit(_ fleet.Record, platform dialect.Platform) string {
	if !supportsTopologyMode(platform, a.Mode) {
		return "unsupported_operation"
	}
	return ""
}

// Run executes the stage and retains its evidence even on failure.
func (a AuditStage) Run(ctx context.Context, s *Session, e *Evidence) error {
	device, err := s.Device(ctx)
	if err != nil {
		return err
	}
	client, err := topology.New(device, a.Registry)
	if err != nil {
		return err
	}
	audit, err := client.Audit(ctx, a.Options)
	evidence := &TopologyEvidence{Audit: audit, Plan: audit.Plan}
	if a.Mode == "discover" {
		evidence.Plan = nil
	}
	e.Artifacts.Topology = evidence
	if err != nil {
		return err
	}
	switch {
	case !audit.Complete() && evidence.Plan != nil && len(evidence.Plan.Changes) > 0:
		e.Outcome = "partial_planned"
		e.MarkReview()
	case !audit.Complete():
		e.Outcome = "unverified"
		e.MarkReview()
	case a.Mode == "discover":
		e.Outcome = "discovered"
	case audit.ManualRequired:
		e.Outcome = "manual_required"
		e.MarkReview()
	case evidence.Plan != nil && len(evidence.Plan.Changes) == 0:
		e.Outcome = "unchanged"
	default:
		e.Outcome = "planned"
	}
	return nil
}

// ApplyStage revalidates and applies a reviewed plan in a new session, then
// confirms the readback and the save. It never rolls back or replays.
type ApplyStage struct{ Registry dialect.Registry }

// Name returns the stable stage identifier recorded in reports.
func (ApplyStage) Name() string { return "apply" }

// Admit checks whether the device and platform support this stage before I/O.
func (ApplyStage) Admit(fleet.Record, dialect.Platform) string { return "" }

// Run executes the stage and retains its evidence even on failure.
func (a ApplyStage) Run(ctx context.Context, s *Session, e *Evidence) error {
	evidence, _ := e.Artifacts.Topology.(*TopologyEvidence)
	if evidence == nil || evidence.Plan == nil {
		return ErrInvalidRequest
	}
	device, err := s.Device(ctx)
	if err != nil {
		return err
	}
	client, err := topology.New(device, a.Registry)
	if err != nil {
		return err
	}
	result, err := client.Apply(ctx, *evidence.Plan, topology.ApplyOptions{Confirmed: true})
	evidence.Result = &result
	if len(result.Receipts) > 0 {
		e.MarkPartial()
	}
	e.Outcome = "applied"
	if err == nil && len(evidence.Audit.Issues) > 0 {
		e.Outcome = "partial_applied"
		e.MarkReview()
	}
	if err != nil && result.Persisted {
		e.Outcome = "persisted_with_error"
	}
	return err
}

// saveTopologyReview validates every plan and writes the change scripts and
// topology-plan.json before any device is admitted to the apply round.
func saveTopologyReview(ctx context.Context, directory string, rows []batchreport.Device, evidence []*Evidence, registry dialect.Registry) error {
	type planned struct {
		Host         string            `json:"host"`
		Name         string            `json:"name,omitempty"`
		Platform     string            `json:"platform"`
		Commands     []string          `json:"commands"`
		ScriptFile   string            `json:"script_file,omitempty"`
		ScriptSHA256 string            `json:"script_sha256,omitempty"`
		Changes      []topology.Change `json:"changes"`
	}
	plans := []planned{}
	for i, row := range rows {
		t, _ := evidence[i].Artifacts.Topology.(*TopologyEvidence)
		if t == nil || t.Plan == nil {
			continue
		}
		if err := topology.ValidatePlan(registry, *t.Plan, t.Plan.Host, t.Plan.Vendor); err != nil {
			return err
		}
		if len(t.Plan.Commands) > 0 {
			name, err := (fleet.Record{Host: row.Host, Name: row.Name}).BackupFilename("hostname_ip")
			if err != nil {
				return err
			}
			path := filepath.Join(directory, "scripts", strings.TrimSuffix(name, ".txt")+".cfg")
			body := strings.Join(t.Plan.Commands, "\n") + "\n"
			if err = storage.PrepareDirectory(filepath.Dir(path)); err != nil {
				return err
			}
			if _, err = storage.Write(ctx, path, []byte(body)); err != nil {
				return err
			}
			t.ScriptFile, t.ScriptSHA256 = path, fmt.Sprintf("%x", sha256.Sum256([]byte(body)))
		}
		plans = append(plans, planned{Host: row.Host, Name: row.Name, Platform: row.Platform, Commands: t.Plan.Commands, ScriptFile: t.ScriptFile, ScriptSHA256: t.ScriptSHA256, Changes: t.Plan.Changes})
	}
	data, err := json.MarshalIndent(map[string]any{"devices": plans}, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(directory, "topology-plan.json")
	if _, err = storage.Write(ctx, path, data); err != nil {
		return err
	}
	_, err = storage.Write(ctx, path+".sha256", fmt.Appendf(nil, "%x\n", sha256.Sum256(data)))
	return err
}

// ExecOptions controls read-only command collection.
type ExecOptions struct {
	Commands          []string
	Plan              bool
	Directory, Policy string
}

// Exec runs read-only commands on every planned device and saves the outputs.
func (r *Runner) Exec(ctx context.Context, runtime Runtime, plan fleet.Plan, o ExecOptions) (Result, error) {
	if len(o.Commands) == 0 {
		return Result{}, ErrInvalidRequest
	}
	op := func(directory string) Operation {
		return Operation{Name: "exec", Outcome: "executed", Stages: []Stage{ExecStage{Commands: o.Commands, Directory: directory}}}
	}
	if o.Plan {
		report := r.Plan(runtime, plan, op(""), o.Policy)
		return Result{Success: report.Success, Report: &report, Planned: true}, nil
	}
	directory, err := storage.CreateBatchDirectory(o.Directory, time.Now())
	if err != nil {
		return Result{}, err
	}
	return r.run(ctx, runtime, plan, op(directory), ExecuteOptions{Directory: directory, Policy: o.Policy}, nil)
}

// RemoteBackupOptions controls a native remote configuration export.
type RemoteBackupOptions struct {
	transfer.RemoteOptions
	Plan              bool
	Directory, Policy string
}

// RemoteBackup makes every capable device export its configuration to a
// remote server. Only device-reported completion is available, so the
// verified policy is rejected.
func (r *Runner) RemoteBackup(ctx context.Context, runtime Runtime, plan fleet.Plan, o RemoteBackupOptions) (Result, error) {
	if batchreport.SuccessPolicy(o.Policy) == batchreport.PolicyVerified {
		return Result{}, ErrInvalidRequest
	}
	stamp := time.Now().UTC().Format("20060102T150405")
	probe := o.RemoteOptions
	probe.Path = RemoteStage{Options: o.RemoteOptions, Stamp: stamp}.path("192.0.2.1")
	if err := transfer.ValidateRemote(probe); err != nil {
		return Result{}, err
	}
	op := Operation{Name: "remote", Outcome: "exported", Stages: []Stage{RemoteStage{Registry: runtime.Registry, Options: o.RemoteOptions, Stamp: stamp}}}
	if o.Plan {
		report := r.Plan(runtime, plan, op, o.Policy)
		return Result{Success: report.Success, Report: &report, Planned: true}, nil
	}
	directory, err := storage.CreateBatchDirectory(o.Directory, time.Now())
	if err != nil {
		return Result{}, err
	}
	return r.run(ctx, runtime, plan, op, ExecuteOptions{Directory: directory, Policy: o.Policy}, nil)
}
