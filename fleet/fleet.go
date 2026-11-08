package fleet

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/gatework/go-net-connector/archive"
	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/parallel"
	"github.com/gatework/go-net-connector/transfer"
)

var ErrMissingCredentials = errors.New("netdisco: missing credentials")

type Artifact struct {
	Backup   *connector.BackupReceipt
	Transfer *transfer.Receipt
	Archive  *archive.Receipt
}

func (a Artifact) clone() Artifact {
	if a.Archive != nil {
		v := a.Archive.Clone()
		a.Archive = &v
	}
	if a.Backup != nil {
		v := a.Backup.Clone()
		a.Backup = &v
	}
	if a.Transfer != nil {
		v := *a.Transfer
		if v.RequestedPath != nil {
			requestedPath := *v.RequestedPath
			v.RequestedPath = &requestedPath
		}
		a.Transfer = &v
	}
	return a
}

type Outcome struct {
	Device                Record
	Status                string
	Artifact              Artifact
	ErrorCode, ErrorType  string
	StartedAt, FinishedAt time.Time
	Duration              time.Duration
	Diagnostic            Diagnostic
}

func (o Outcome) Success() bool   { return outcomeStatus(o.Status).succeeded() }
func (o Outcome) Partial() bool   { return outcomeStatus(o.Status).partiallySucceeded() }
func (o Outcome) Attempted() bool { return outcomeStatus(o.Status).attempted() }
func (o Outcome) clone() Outcome  { o.Artifact = o.Artifact.clone(); return o }

type CallbackError struct {
	Host      string `json:"host"`
	ErrorType string `json:"error_type"`
}
type Batch struct {
	mode                        mode
	outcomes                    []Outcome
	startedAt, finishedAt       time.Time
	callbackErrors              []CallbackError
	reportLocation, reportError string
	interrupted                 bool
}

func (b Batch) Outcomes() []Outcome {
	out := make([]Outcome, len(b.outcomes))
	for i, v := range b.outcomes {
		out[i] = v.clone()
	}
	return out
}
func (b Batch) CallbackErrors() []CallbackError {
	return append([]CallbackError(nil), b.callbackErrors...)
}
func (b Batch) Counts() map[string]int {
	m := map[string]int{}
	for _, o := range b.outcomes {
		m[string(outcomeStatus(o.Status))]++
	}
	return m
}
func (b Batch) Success() bool {
	if b.interrupted || len(b.outcomes) == 0 || len(b.callbackErrors) > 0 || b.reportError != "" {
		return false
	}
	for _, o := range b.outcomes {
		if !o.Success() {
			return false
		}
	}
	return true
}
func (b Batch) Status() string {
	if b.interrupted {
		return string(batchStatusInterrupted)
	}
	if len(b.outcomes) == 0 {
		return string(batchStatusNoDevices)
	}
	if b.Success() {
		return string(batchStatusSucceeded)
	}
	return string(batchStatusIncomplete)
}

type RunOptions struct {
	Workers int
	// Preflight runs once before any task; credentials belong inside Run.
	Preflight func(context.Context, Plan) error
	Run       func(context.Context, Task) (Artifact, error)
	OnOutcome func(context.Context, Outcome) error
}

func Run(ctx context.Context, p Plan, o RunOptions) (Batch, error) {
	b := Batch{mode: p.mode, startedAt: time.Now().UTC()}
	if o.Run == nil || o.Workers < 0 || o.Workers > 500 || (p.mode != modeBackup && p.mode != modeTFTP) {
		return b, ErrInvalidPlan
	}
	if o.Preflight != nil {
		if e := o.Preflight(ctx, p); e != nil {
			return b, e
		}
	}
	b.outcomes = make([]Outcome, len(p.entries))
	for i, entry := range p.entries {
		status := outcomeStatus(entry.Status)
		if planStatus(entry.Status) == planStatusReady {
			status = outcomeStatusNotStarted
		}
		b.outcomes[i] = Outcome{Device: entry.Device, Status: string(status)}
	}
	var callbacks sync.Mutex
	_, err := parallel.Map(ctx, o.Workers, p.tasks, func(ctx context.Context, task Task) (struct{}, error) {
		start := time.Now()
		out := Outcome{Device: task.Device, StartedAt: start.UTC()}
		runReturned := false
		defer func() {
			if !runReturned {
				out.Status = string(outcomeStatusFailed)
				out.FinishedAt = time.Now().UTC()
				out.Duration = time.Since(start)
				out.Diagnostic = Diagnostic{ErrorCode: "internal_error", ErrorType: "StandardError", Phase: "callback"}
				out.ErrorCode, out.ErrorType = out.Diagnostic.ErrorCode, out.Diagnostic.ErrorType
			}
			// Each task owns one slot. Commit evidence even if an observer exits.
			b.outcomes[task.Index] = out
		}()
		a, e := o.Run(ctx, task)
		runReturned = true
		out.Artifact = a.clone()
		out.FinishedAt = time.Now().UTC()
		out.Duration = time.Since(start)
		switch {
		case errors.Is(e, ErrMissingCredentials):
			out.Status = string(outcomeStatusMissingCredentials)
			e = nil
		case p.mode == modeTFTP && a.Transfer != nil && a.Transfer.Reported():
			if a.Archive != nil && !a.Archive.Complete() && e == nil {
				e = connector.ErrIncomplete
			}
			out.Status = string(outcomeStatusReportedUploaded)
			if e != nil {
				out.Status = string(outcomeStatusReportedWithError)
			}
		case p.mode == modeBackup && a.Backup != nil && a.Backup.HasSavedEvidence():
			out.Status = string(outcomeStatusBackedUp)
			if a.Backup.HasUnverifiedPair() && e == nil {
				e = connector.ErrIncomplete
			}
			if e != nil {
				out.Status = string(outcomeStatusSavedWithError)
			}
		case (errors.Is(e, context.Canceled) || errors.Is(e, context.DeadlineExceeded)) && ctx.Err() != nil:
			out.Status = string(outcomeStatusInterrupted)
		default:
			out.Status = string(outcomeStatusFailed)
			if e == nil {
				e = connector.ErrIncomplete
			}
		}
		out.Diagnostic = diagnoseError(e, a)
		out.ErrorCode, out.ErrorType = out.Diagnostic.ErrorCode, out.Diagnostic.ErrorType
		b.outcomes[task.Index] = out
		if o.OnOutcome != nil {
			func() {
				callbacks.Lock()
				defer callbacks.Unlock()
				returned := false
				defer func() {
					if !returned {
						b.callbackErrors = append(b.callbackErrors, CallbackError{task.Device.Host, "StandardError"})
					}
				}()
				callbackErr := o.OnOutcome(ctx, out.clone())
				returned = true
				if callbackErr != nil {
					b.callbackErrors = append(b.callbackErrors, CallbackError{task.Device.Host, diagnoseError(callbackErr, Artifact{}).ErrorType})
				}
			}()
		}
		return struct{}{}, nil
	})
	b.finishedAt = time.Now().UTC()
	b.interrupted = ctx.Err() != nil || errors.Is(err, parallel.ErrWorkerExit)
	return b, err
}
