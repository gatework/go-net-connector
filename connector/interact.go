//go:build linux || darwin

package connector

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"

	"golang.org/x/sys/unix"
	"golang.org/x/term"

	expect "github.com/gatework/go-expect"
)

// InteractOptions borrows a file input and cooperative output. Input must have
// no competing reader for the duration of the call. Empty Escape disables it.
type InteractOptions struct {
	Input   *os.File
	Output  io.Writer
	Escape  []byte
	SkipRaw bool
}
type InteractResult struct {
	Reason       string
	PendingInput []byte
}
type inputResult struct {
	reason  string
	pending []byte
	err     error
}

func (d *Device) Interact(parent context.Context, o InteractOptions) (result InteractResult, err error) {
	if o.Input == nil || isNilValue(o.Output) {
		return result, ErrInvalid
	}
	if e := d.acquire(); e != nil {
		return result, e
	}
	defer d.release()
	parent, stopOperation := d.operationContext(parent)
	defer stopOperation()
	if err = d.connect(parent); err != nil {
		return
	}
	if err = d.log.scope(d.baseSecrets(), true); err != nil {
		return
	}
	fd := int(o.Input.Fd())
	if !o.SkipRaw && term.IsTerminal(fd) {
		state, e := term.MakeRaw(fd)
		if e != nil {
			return result, e
		}
		defer func() {
			err = joinCleanup(err, term.Restore(fd, state))
		}()
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	done := make(chan inputResult, 1)
	escape := append([]byte(nil), o.Escape...)
	session := d.session
	go func() { r := d.relayInput(ctx, session, fd, escape); done <- r; cancel() }()
	returned := false
	defer func() {
		if !returned {
			cancel()
			_ = d.closeResources()
			<-done
		}
	}()
	pattern, _ := expect.Compile(`(?s).+`)
	_, readErr := d.session.Expect(ctx, []expect.Case{{Pattern: pattern, Handler: func(_ context.Context, _ *expect.Session, r expect.Result) (expect.Action, error) {
		p := []byte(r.Before + r.Match)
		n, e := o.Output.Write(p)
		if e == nil && n != len(p) {
			e = io.ErrShortWrite
		}
		return expect.ContinueKeepTimer, e
	}}, {Pattern: expect.EOF()}}, expect.WithTimeout(0))
	cancel()
	cleanupErr := d.closeResources()
	defer func() { err = joinCleanup(err, cleanupErr) }()
	input := <-done
	returned = true
	result = InteractResult{Reason: input.reason, PendingInput: append([]byte(nil), input.pending...)}
	if e := parent.Err(); e != nil {
		return result, e
	}
	if readErr != nil && !errors.Is(readErr, context.Canceled) && !errors.Is(readErr, io.EOF) {
		return result, newError("interaction_failed", "interact", Command{}, readErr)
	}
	if input.err != nil && !errors.Is(input.err, context.Canceled) && !errors.Is(input.err, expect.ErrClosed) {
		return result, newError("interaction_failed", "interact", Command{}, input.err)
	}
	if result.Reason == "" {
		result.Reason = "peer_eof"
	}
	return result, nil
}
func (d *Device) relayInput(ctx context.Context, session *expect.Session, fd int, escape []byte) inputResult {
	buffer := make([]byte, 4096)
	var pending []byte
	for {
		if e := ctx.Err(); e != nil {
			return inputResult{err: e, pending: pending}
		}
		poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		n, e := unix.Poll(poll, 10)
		if errors.Is(e, unix.EINTR) {
			continue
		}
		if e != nil {
			return inputResult{err: e, pending: pending}
		}
		if n == 0 {
			continue
		}
		if poll[0].Revents&unix.POLLNVAL != 0 {
			return inputResult{err: os.ErrInvalid, pending: pending}
		}
		n, e = unix.Read(fd, buffer)
		if errors.Is(e, unix.EINTR) || errors.Is(e, unix.EAGAIN) {
			continue
		}
		if e != nil {
			return inputResult{err: e, pending: pending}
		}
		if n == 0 {
			if len(pending) > 0 {
				if _, e = session.SendAll(ctx, string(pending)); e != nil {
					return inputResult{err: e}
				}
			}
			return inputResult{reason: "input_eof"}
		}
		pending = append(pending, buffer[:n]...)
		send := len(pending)
		if len(escape) > 0 {
			if i := bytes.Index(pending, escape); i >= 0 {
				if i > 0 {
					if _, e = session.SendAll(ctx, string(pending[:i])); e != nil {
						return inputResult{err: e, pending: pending}
					}
				}
				return inputResult{reason: "escape", pending: pending[i+len(escape):]}
			}
			for retain := min(len(pending), len(escape)-1); retain > 0; retain-- {
				if bytes.Equal(pending[len(pending)-retain:], escape[:retain]) {
					send -= retain
					break
				}
			}
		}
		if send > 0 {
			if _, e = session.SendAll(ctx, string(pending[:send])); e != nil {
				return inputResult{err: e, pending: pending}
			}
			pending = append(pending[:0], pending[send:]...)
		}
	}
}
