package workflow

import (
	"context"
	"time"

	"github.com/gatework/go-net-connector/archive"
	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/internal/batchreport"
)

// Event carries bounded operation facts, never terminal text or response bodies.
// Observe calls are serialized, and an observer error is retained until the
// operation's final report. A failed display never causes a device write replay.
type Event interface{ workflowEvent() }

// Observer receives workflow events in order.
type Observer func(Event) error

// PlanEvent reports how many inventory entries were selected and the worker
// count.
type PlanEvent struct {
	Total, Selected, Workers int
	Mode                     string
}

// StartEvent reports that a device started.
type StartEvent struct{ Host, Vendor string }

// SessionEvent relays a connector session event.
type SessionEvent connector.SessionEvent

// OutcomeEvent reports a device's final status.
type OutcomeEvent struct {
	Host, Status, Outcome, ErrorCode string
	Duration                         time.Duration
	TextWarningBytes                 int
	SourceCommitted, SourceVerified  bool
}

// TransferStageEvent reports a device's transfer stage.
type TransferStageEvent struct{ Host, Stage string }

// Phase names a batch-wide phase.
type Phase string

// Batch-wide phases.
const (
	PhaseArchivePreflight Phase = "archive_preflight"
	PhaseTransfer         Phase = "transfer"
	PhaseTopologyPlan     Phase = "topology_plan"
	PhaseTopologyApply    Phase = "topology_apply"
)

// PhaseEvent reports the start of a phase and its time budget.
type PhaseEvent struct {
	Phase   Phase
	Timeout time.Duration
}

// ArchivePreflightEvent reports archive preparation progress for one of
// Groups server groups.
type ArchivePreflightEvent struct {
	Progress      archive.PrepareProgress
	Group, Groups int
}

// ArchiveReadyEvent reports that an archive server is ready.
type ArchiveReadyEvent struct{ Server, Directory string }

// BeginPhaseEvent reports the start of a follow-up phase over Total devices.
type BeginPhaseEvent struct{ Total, Workers int }

// FinishedEvent reports that the batch finished.
type FinishedEvent struct{}

func (PlanEvent) workflowEvent()             {}
func (StartEvent) workflowEvent()            {}
func (SessionEvent) workflowEvent()          {}
func (OutcomeEvent) workflowEvent()          {}
func (TransferStageEvent) workflowEvent()    {}
func (PhaseEvent) workflowEvent()            {}
func (ArchivePreflightEvent) workflowEvent() {}
func (ArchiveReadyEvent) workflowEvent()     {}
func (BeginPhaseEvent) workflowEvent()       {}
func (FinishedEvent) workflowEvent()         {}

func (r *Runner) emit(event Event) error {
	if r.Observe == nil {
		return nil
	}
	r.progressMu.Lock()
	defer r.progressMu.Unlock()
	err := r.Observe(event)
	if err != nil && r.progressErr == nil {
		r.progressErr = err
	}
	return err
}
func (r *Runner) observe() SessionObserver {
	return func(_ context.Context, e connector.SessionEvent) error {
		_ = r.emit(SessionEvent(e))
		return nil
	}
}
func (r *Runner) begin() {
	r.progressMu.Lock()
	r.progressErr = nil
	r.progressMu.Unlock()
}

// finish emits the final event; a progress failure fails the batch while
// keeping every device result.
func (r *Runner) finish(report *batchreport.Report) {
	_ = r.emit(FinishedEvent{})
	r.progressMu.Lock()
	err := r.progressErr
	r.progressMu.Unlock()
	if err != nil {
		result := Result{Report: report}
		RecordFailure(context.Background(), &result, "progress_failed", err)
	}
}
