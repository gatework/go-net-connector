// Package workflow coordinates concrete operations independently of command parsing.
package workflow

import (
	"context"
	"io"

	"github.com/gatework/go-net-connector/archive"
	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/fleet"
	"github.com/gatework/go-net-connector/internal/progress"
	"github.com/gatework/go-net-connector/topology"
	"github.com/gatework/go-net-connector/transfer"
	"github.com/gatework/go-net-connector/vendors"
)

type BackupOptions struct{ Policy, Layout string }
type TFTPOptions struct {
	BackupOptions
	Plan, All bool
}
type TopologyOptions struct {
	Mode          string
	InventoryOnly bool
	Limit         int
}

// Runtime contains the application dependencies shared by operation workflows.
// The CLI composition root builds it from validated settings and resource factories.
type Runtime struct {
	Rules        *fleet.Rules
	Registry     *vendors.Registry
	Workers      int
	Directory    string
	LogDirectory func(mode, directory string) string
	OpenDevice   DeviceOpener
}

// SessionObserver receives session events for the current workflow operation.
type SessionObserver func(context.Context, connector.SessionEvent) error

// DeviceOpener creates one device session with operation-local logging and observation.
type DeviceOpener func(context.Context, fleet.Record, io.Writer, SessionObserver) (*connector.Device, error)

type TFTPSettings struct {
	ArchiveTimeouts   archive.Timeouts
	Policy            *transfer.Policy
	LimitPerVendor    int
	ArchiveWorkerPath string
	ArchivePassword   func() string
}

type TopologySettings struct {
	Mode                 string
	Workers              int
	Limit                int
	Directory            string
	DescriptionFormat    string
	AbbreviatePorts      bool
	LowercasePorts       bool
	NeighborNamePrefixes []string
}
type BatchResult struct {
	ServerArchiveDirectories []string
	Directory                string
	Report                   fleet.Report
	Plan                     fleet.Plan
	Workers                  int
}
type TopologyOutcome struct {
	Device fleet.Record
	Audit  topology.Audit
	Result topology.Result
	Plan   *topology.Plan
}
type TopologyResult struct {
	Inventory []fleet.PlanEntry
	Outcomes  []TopologyOutcome
	Directory string
	Summary   map[string]any
}
type PreviewResult struct {
	Plan fleet.Plan
	View map[string]any
}
type Result struct {
	Success  bool
	Batch    *BatchResult
	Topology *TopologyResult
	Preview  *PreviewResult
}
type Runner struct {
	Progress io.Writer
	Reporter *progress.Reporter
	result   Result
}

func (r *Runner) fail(err error) (Result, error) { return r.result, err }
func (r *Runner) complete(success bool) Result   { r.result.Success = success; return r.result }
func Preview(plan fleet.Plan) Result {
	return Result{Success: len(plan.Tasks()) > 0, Preview: &PreviewResult{Plan: plan, View: PlanView(plan)}}
}

// RecordOutputFailure persists output diagnostics without losing committed artifacts.
func RecordOutputFailure(ctx context.Context, result Result, err error) {
	if err == nil {
		return
	}
	ctx = context.WithoutCancel(ctx)
	if b := result.Batch; b != nil {
		loc, _ := b.Report.Statistics()["report_location"].(string)
		b.Report = fleet.WriteReport(ctx, b.Report.WithReportError(err, loc), b.Directory, b.Plan, b.Workers)
	}
	if t := result.Topology; t != nil && t.Directory != "" {
		t.Summary["success"] = false
		t.Summary["output_error"] = "IOError"
		t.Summary["report_error"] = "IOError"
		_ = persistTopologySummary(ctx, t.Directory, t.Summary)
	}
}

// RecordProgressFailure retains committed results while marking display failure.
func RecordProgressFailure(ctx context.Context, result *Result, err error) {
	if result == nil || err == nil {
		return
	}
	result.Success = false
	ctx = context.WithoutCancel(ctx)
	if b := result.Batch; b != nil {
		loc, _ := b.Report.Statistics()["report_location"].(string)
		b.Report = fleet.WriteReport(ctx, b.Report.WithReportError(err, loc), b.Directory, b.Plan, b.Workers)
	}
	if t := result.Topology; t != nil && t.Directory != "" {
		t.Summary["success"] = false
		t.Summary["progress_error"] = "IOError"
		t.Summary["report_error"] = "IOError"
		_ = persistTopologySummary(ctx, t.Directory, t.Summary)
	}
}
