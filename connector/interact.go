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

// InteractResult reports why an interactive session ended and any input read
// after the escape sequence that was not forwarded.
type InteractResult struct {
	Reason       string
	PendingInput []byte
}
type inputResult struct {
	reason  string
	pending []byte
	err     error
}

// Interact relays o.Input to the device and device output to o.Output until
// the peer closes, input ends or the escape sequence is typed. The terminal
// is put in raw mode unless o.SkipRaw is set, and the session is closed on
// return.
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
	readErr := d.copyOutput(ctx, o.Output)
	cancel()
	cleanupErr := d.closeResources()
	defer func() { err = joinCleanup(err, cleanupErr) }()
	input := <-done
	returned = true
	result = InteractResult{Reason: input.reason, PendingInput: append([]byte(nil), input.pending...)}
	if e := parent.Err(); e != nil {
		return result, e
	}
	if e := interactionFailure(readErr, input.err); e != nil {
		return result, e
	}
	if result.Reason == "" {
		result.Reason = "peer_eof"
	}
	return result, nil
}

// copyOutput writes everything the device sends to w until EOF or ctx ends.
func (d *Device) copyOutput(ctx context.Context, w io.Writer) error {
	pattern, _ := expect.Compile(`(?s).+`)
	write := func(_ context.Context, _ *expect.Session, r expect.Result) (expect.Action, error) {
		p := []byte(r.Before + r.Match)
		n, e := w.Write(p)
		if e == nil && n != len(p) {
			e = io.ErrShortWrite
		}
		return expect.ContinueKeepTimer, e
	}
	_, err := d.session.Expect(ctx, []expect.Case{{Pattern: pattern, Handler: write}, {Pattern: expect.EOF()}}, expect.WithTimeout(0))
	return err
}

// interactionFailure classifies the output and input relay errors, ignoring
// the cancellation and closure that end every interaction.
func interactionFailure(readErr, inputErr error) error {
	if readErr != nil && !errors.Is(readErr, context.Canceled) && !errors.Is(readErr, io.EOF) {
		return newError("interaction_failed", "interact", Command{}, readErr)
	}
	if inputErr != nil && !errors.Is(inputErr, context.Canceled) && !errors.Is(inputErr, expect.ErrClosed) {
		return newError("interaction_failed", "interact", Command{}, inputErr)
	}
	return nil
}

// relayInput forwards terminal input from fd to session until input EOF or
// the escape sequence. A possible escape prefix at the end of the input is
// held back until it is decided.
func (d *Device) relayInput(ctx context.Context, session *expect.Session, fd int, escape []byte) inputResult {
	buffer := make([]byte, 4096)
	var pending []byte
	for {
		if e := ctx.Err(); e != nil {
			return inputResult{err: e, pending: pending}
		}
		n, retry, e := readReady(fd, buffer)
		if e != nil {
			return inputResult{err: e, pending: pending}
		}
		if retry {
			continue
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
		var done *inputResult
		if pending, done = forwardInput(ctx, session, pending, escape); done != nil {
			return *done
		}
	}
}

// readReady waits up to 10ms for input on fd and reads it; retry is true
// when nothing was read and the caller should poll again. n == 0 without
// retry is EOF.
func readReady(fd int, buffer []byte) (n int, retry bool, err error) {
	poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	ready, e := unix.Poll(poll, 10)
	if errors.Is(e, unix.EINTR) {
		return 0, true, nil
	}
	if e != nil {
		return 0, false, e
	}
	if ready == 0 {
		return 0, true, nil
	}
	if poll[0].Revents&unix.POLLNVAL != 0 {
		return 0, false, os.ErrInvalid
	}
	n, e = unix.Read(fd, buffer)
	if errors.Is(e, unix.EINTR) || errors.Is(e, unix.EAGAIN) {
		return 0, true, nil
	}
	return n, false, e
}

// forwardInput sends the pending input up to the escape sequence, keeping a
// trailing escape prefix; done is set when the escape was found or sending
// failed.
func forwardInput(ctx context.Context, session *expect.Session, pending, escape []byte) (rest []byte, done *inputResult) {
	send := len(pending)
	if len(escape) > 0 {
		if i := bytes.Index(pending, escape); i >= 0 {
			if i > 0 {
				if _, e := session.SendAll(ctx, string(pending[:i])); e != nil {
					return pending, &inputResult{err: e, pending: pending}
				}
			}
			return pending, &inputResult{reason: "escape", pending: pending[i+len(escape):]}
		}
		for retain := min(len(pending), len(escape)-1); retain > 0; retain-- {
			if bytes.Equal(pending[len(pending)-retain:], escape[:retain]) {
				send -= retain
				break
			}
		}
	}
	if send > 0 {
		if _, e := session.SendAll(ctx, string(pending[:send])); e != nil {
			return pending, &inputResult{err: e, pending: pending}
		}
		pending = append(pending[:0], pending[send:]...)
	}
	return pending, nil
}
