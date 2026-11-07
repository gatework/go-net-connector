package archive

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

type channelEndpoint struct {
	reads, writes                 int
	readDeadlines, writeDeadlines int
	failReadAt, failWriteAt       int
	deadlineError                 error
}

func (c *channelEndpoint) Read([]byte) (int, error) {
	c.reads++
	return 0, io.EOF
}
func (c *channelEndpoint) Write(p []byte) (int, error) {
	c.writes++
	return len(p), nil
}
func (c *channelEndpoint) SetReadDeadline(time.Time) error {
	c.readDeadlines++
	if c.readDeadlines == c.failReadAt {
		return c.deadlineError
	}
	return nil
}
func (c *channelEndpoint) SetWriteDeadline(time.Time) error {
	c.writeDeadlines++
	if c.writeDeadlines == c.failWriteAt {
		return c.deadlineError
	}
	return nil
}

func TestChannelRejectsInvalidEndpointsBeforeIO(t *testing.T) {
	for _, kind := range []string{"nil_input", "nil_output", "typed_nil_input", "typed_nil_output", "missing_close"} {
		t.Run(kind, func(t *testing.T) {
			defer func() {
				if v := recover(); v != nil {
					t.Errorf("invalid channel panicked: %v", v)
				}
			}()
			endpoint := &channelEndpoint{}
			closes, ran := 0, false
			ch := Channel{Input: endpoint, Output: endpoint, Close: func() error { closes++; return nil }}
			var nilEndpoint *channelEndpoint
			switch kind {
			case "nil_input":
				ch.Input = nil
			case "nil_output":
				ch.Output = nil
			case "typed_nil_input":
				ch.Input = nilEndpoint
			case "typed_nil_output":
				ch.Output = nilEndpoint
			case "missing_close":
				ch.Close = nil
			}
			err := runChannel(t.Context(), func(context.Context) (Channel, error) { return ch, nil }, func(*Protocol) error { ran = true; return nil }, nil)
			if !errors.Is(err, ErrArchive) || ran {
				t.Errorf("invalid channel: ran=%v err=%v", ran, err)
			}
			if endpoint.reads != 0 || endpoint.writes != 0 || endpoint.readDeadlines != 0 || endpoint.writeDeadlines != 0 {
				t.Error("invalid channel invoked endpoint methods")
			}
			if ch.Close != nil && closes != 1 {
				t.Errorf("close calls = %d, want 1", closes)
			}
		})
	}
}

func TestChannelRejectsUnsupportedDeadlinesBeforeIO(t *testing.T) {
	for _, kind := range []string{"set_write", "set_read", "reset_write", "reset_read"} {
		t.Run(kind, func(t *testing.T) {
			fault := errors.New("deadlines unavailable")
			endpoint := &channelEndpoint{deadlineError: fault}
			switch kind {
			case "set_write":
				endpoint.failWriteAt = 1
			case "set_read":
				endpoint.failReadAt = 1
			case "reset_write":
				endpoint.failWriteAt = 2
			case "reset_read":
				endpoint.failReadAt = 2
			}
			closes, ran := 0, false
			err := runChannel(t.Context(), func(context.Context) (Channel, error) {
				return Channel{Input: endpoint, Output: endpoint, Close: func() error { closes++; return nil }}, nil
			}, func(*Protocol) error { ran = true; return nil }, nil)
			if !errors.Is(err, ErrArchive) || !errors.Is(err, fault) || ran || closes != 1 || endpoint.reads != 0 || endpoint.writes != 0 {
				t.Errorf("unsupported deadlines: ran=%v closes=%d reads=%d writes=%d err=%v", ran, closes, endpoint.reads, endpoint.writes, err)
			}
		})
	}
}

func TestChannelPreservesPrimaryAndCleanupErrors(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	primary, cleanup := errors.New("primary"), errors.New("cleanup")
	err := runChannel(t.Context(), func(context.Context) (Channel, error) {
		return Channel{Input: a, Output: a, Close: func() error { a.Close(); return cleanup }}, nil
	}, func(*Protocol) error { return primary }, nil)
	if !errors.Is(err, primary) || !errors.Is(err, cleanup) {
		t.Fatalf("lost cause: %v", err)
	}
}
func TestChannelOwnsPartialLauncherReturn(t *testing.T) {
	closed := false
	primary, cleanup := errors.New("setup"), errors.New("cleanup")
	err := runChannel(t.Context(), func(context.Context) (Channel, error) {
		return Channel{Close: func() error { closed = true; return cleanup }}, primary
	}, func(*Protocol) error { t.Fatal("ran failed channel"); return nil }, nil)
	if !closed || !errors.Is(err, primary) || !errors.Is(err, cleanup) {
		t.Fatalf("closed=%v err=%v", closed, err)
	}
}
