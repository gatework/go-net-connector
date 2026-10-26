package archive

import (
	"context"
	"errors"
	"io"
	"time"
)

type Channel struct {
	Input  io.Writer
	Output io.Reader
	Close  func() error
}

// Launcher transfers ownership of a nonzero Channel even when it also returns an error.
type Launcher func(context.Context) (Channel, error)

// runChannel owns a launched channel, including partial returns with an error.
func runChannel(ctx context.Context, launch Launcher, timeout time.Duration, run func(*Protocol) error, retain func(func() error)) (err error) {
	ch, e := launch(ctx)
	closeChannel := func() error {
		err := ch.Close()
		if err != nil && retain != nil {
			retain(ch.Close)
		}
		return err
	}
	if e != nil {
		if ch.Close != nil {
			e = errors.Join(e, closeChannel())
		}
		return &Error{cause: e}
	}
	if ch.Close != nil {
		defer func() {
			if joined := errors.Join(err, closeChannel()); joined != nil {
				err = &Error{cause: joined}
			}
		}()
	}
	input, ok := ch.Input.(interface{ SetWriteDeadline(time.Time) error })
	if !ok || ch.Output == nil {
		return ErrArchive
	}
	output, ok := ch.Output.(interface{ SetReadDeadline(time.Time) error })
	if !ok {
		return ErrArchive
	}
	// Reject endpoints whose deadline methods exist but cannot enforce deadlines.
	deadline := time.Now().Add(time.Second)
	if input.SetWriteDeadline(deadline) != nil || output.SetReadDeadline(deadline) != nil {
		return ErrArchive
	}
	if input.SetWriteDeadline(time.Time{}) != nil || output.SetReadDeadline(time.Time{}) != nil {
		return ErrArchive
	}
	return run(NewProtocol(ch.Input, ch.Output, timeout))
}
