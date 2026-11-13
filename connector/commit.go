package connector

import (
	"context"
	"errors"
)

// CommitOptions selects how a candidate configuration is committed.
type CommitOptions struct {
	// ConfirmMinutes, when positive, commits with an automatic rollback after
	// this many minutes and then confirms it. If the change costs the session,
	// the confirmation never arrives and the device rolls back by itself.
	ConfirmMinutes int
}

// CommitTraits describes a platform's candidate-configuration model.
type CommitTraits struct {
	// ConfirmSupported reports whether CommitOptions.ConfirmMinutes is available.
	ConfirmSupported bool
	// CandidateShared reports that uncommitted changes outlive the session, so
	// a failed deployment leaves them for the operator to revert.
	CandidateShared bool
}

// Committer is an optional Driver capability for platforms that change
// configuration through a candidate that must be committed. On such a
// platform a commit is also the persistence step; there is no separate save.
type Committer interface {
	CommitTraits() CommitTraits
	// CandidateScript wraps validated configuration commands in the commands
	// that enter the candidate, commit it and leave.
	CandidateScript(body Script, o CommitOptions) (Script, error)
	// ValidateCommit requires affirmative device evidence that the commit
	// completed; a prompt alone is not proof. It returns ErrUnconfirmed (or a
	// wrapped cause) when the evidence is missing.
	ValidateCommit(ctx context.Context, result Result) error
}

// AsCommitter returns d's commit capability, looking through WithTerminal.
func AsCommitter(d Driver) (Committer, bool) {
	if o, ok := d.(terminalOverride); ok {
		d = o.Driver
	}
	c, ok := d.(Committer)
	return c, ok
}

// Commit applies body through the platform's candidate configuration and
// requires the platform's affirmative commit evidence. The whole wrapped
// script is validated before anything is sent, and nothing is replayed: a
// failure retires the session and keeps every accepted step and attempt.
func (o *Operation) Commit(ctx context.Context, body Script, opts CommitOptions) (Result, error) {
	committer, ok := AsCommitter(o.device.driver)
	if !ok {
		return Result{}, ErrUnsupported
	}
	if opts.ConfirmMinutes < 0 || opts.ConfirmMinutes > 60 || opts.ConfirmMinutes > 0 && !committer.CommitTraits().ConfirmSupported {
		return Result{}, ErrInvalid
	}
	if _, err := body.clone(); err != nil {
		return Result{}, err
	}
	script, err := committer.CandidateScript(body, opts)
	if err != nil {
		return Result{}, err
	}
	result, err := o.ExecuteScript(ctx, script, ExecuteOptions{})
	if err != nil {
		return result, err
	}
	if err = committer.ValidateCommit(ctx, result); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return result, err
		}
		return result, newError("commit_unconfirmed", "commit", Command{}, errors.Join(ErrUnconfirmed, err))
	}
	return result, nil
}
