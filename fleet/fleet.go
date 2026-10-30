package fleet

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/parallel"
	"github.com/gatework/go-net-connector/transfer"
)

var ErrMissingCredentials = errors.New("netdisco: missing credentials")

type Artifact struct {
	Backup   *connector.BackupReceipt
	Transfer *transfer.Receipt
}

func (a Artifact) clone() Artifact {
	if a.Backup != nil {
		v := *a.Backup
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

func (o Outcome) Success() bool { return o.Status == "backed_up" || o.Status == "reported_uploaded" }
func (o Outcome) Partial() bool {
	return o.Status == "saved_with_error" || o.Status == "reported_with_error"
}
func (o Outcome) Attempted() bool {
	return o.Success() || o.Partial() || o.Status == "failed" || o.Status == "interrupted"
}
func (o Outcome) clone() Outcome { o.Artifact = o.Artifact.clone(); return o }

type CallbackError struct {
	Host      string `json:"host"`
	ErrorType string `json:"error_type"`
}
type Batch struct {
	mode                        string
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
		m[o.Status]++
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
		return "interrupted"
	}
	if len(b.outcomes) == 0 {
		return "no_devices"
	}
	if b.Success() {
		return "succeeded"
	}
	return "incomplete"
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
	if o.Run == nil || o.Workers < 0 || o.Workers > 500 || (p.mode != "backup" && p.mode != "tftp") {
		return b, ErrPlan
	}
	if o.Preflight != nil {
		if e := o.Preflight(ctx, p); e != nil {
			return b, e
		}
	}
	b.outcomes = make([]Outcome, len(p.entries))
	for i, entry := range p.entries {
		status := entry.Status
		if status == "ready" {
			status = "not_started"
		}
		b.outcomes[i] = Outcome{Device: entry.Device, Status: status}
	}
	var callbacks sync.Mutex
	_, err := parallel.Map(ctx, o.Workers, p.tasks, func(ctx context.Context, task Task) (struct{}, error) {
		start := time.Now()
		out := Outcome{Device: task.Device, StartedAt: start.UTC()}
		runReturned := false
		defer func() {
			if !runReturned {
				out.Status = "failed"
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
			out.Status = "missing_credentials"
			e = nil
		case p.mode == "tftp" && a.Transfer != nil && a.Transfer.Reported():
			out.Status = "reported_uploaded"
			if e != nil {
				out.Status = "reported_with_error"
			}
		case p.mode == "backup" && a.Backup != nil && (a.Backup.File.Committed() || a.Backup.DeviceCompleted && a.Backup.Change == "unchanged" && a.Backup.NoWriteVerified):
			out.Status = "backed_up"
			if e != nil {
				out.Status = "saved_with_error"
			}
		case errors.Is(e, context.Canceled) || errors.Is(e, context.DeadlineExceeded) && ctx.Err() != nil:
			out.Status = "interrupted"
		default:
			out.Status = "failed"
			if e == nil {
				e = connector.ErrIncomplete
			}
		}
		out.Diagnostic = diagnose(e, a)
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
					b.callbackErrors = append(b.callbackErrors, CallbackError{task.Device.Host, diagnose(callbackErr, Artifact{}).ErrorType})
				}
			}()
		}
		return struct{}{}, nil
	})
	b.finishedAt = time.Now().UTC()
	b.interrupted = ctx.Err() != nil || errors.Is(err, parallel.ErrWorkerExit)
	return b, err
}
