package workflow

import (
	"context"
	"time"

	"github.com/gatework/go-net-connector/archive"
	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/fleet"
)

// Event carries bounded operation facts, never terminal text or response bodies.
// Observe calls are serialized, and an observer error is retained until the
// operation's final report. A failed display never causes a device write replay.
type Event interface{ workflowEvent() }
type Observer func(Event) error

type PlanEvent struct {
	Total, Selected, Workers int
	Mode                     string
}
type StartEvent struct{ Host, Vendor string }
type SessionEvent connector.SessionEvent
type OutcomeEvent struct {
	Host, Status, ErrorCode         string
	Duration                        time.Duration
	TextWarningBytes                int
	SourceCommitted, SourceVerified bool
}
type TransferStageEvent struct{ Host, Stage string }
type Phase string

const (
	PhaseArchivePreflight Phase = "archive_preflight"
	PhaseTransfer         Phase = "transfer"
	PhaseTopologyPlan     Phase = "topology_plan"
	PhaseTopologyApply    Phase = "topology_apply"
)

type PhaseEvent struct {
	Phase   Phase
	Timeout time.Duration
}
type ArchivePreflightEvent struct {
	Progress      archive.PrepareProgress
	Group, Groups int
}
type ArchiveReadyEvent struct{ Server, Directory string }
type BeginPhaseEvent struct{ Total, Workers int }
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
func (r *Runner) outcome(out fleet.Outcome) error {
	event := OutcomeEvent{Host: out.Device.Host, Status: out.Status, ErrorCode: out.ErrorCode, Duration: out.Duration}
	if backup := out.Artifact.Backup; backup != nil && backup.TextWarning != nil {
		event.TextWarningBytes = backup.TextWarning.Count
		event.SourceCommitted, event.SourceVerified = backup.SourceEvidence()
	}
	return r.emit(event)
}
func (r *Runner) begin() {
	r.result = Result{}
	r.progressMu.Lock()
	r.progressErr = nil
	r.progressMu.Unlock()
}
func (r *Runner) finishBatch(ctx context.Context) {
	_ = r.emit(FinishedEvent{})
	r.progressMu.Lock()
	err := r.progressErr
	r.progressMu.Unlock()
	RecordProgressFailure(ctx, &r.result, err)
}
