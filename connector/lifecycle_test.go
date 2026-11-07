package connector

import (
	"context"
	"errors"
	"io"
	"net"
	"runtime"
	"testing"
	"time"
)

func TestCloseInterruptsDialWithoutWaitingForOperation(t *testing.T) {
	entered := make(chan struct{})
	d, err := New(fixtureProfile(), ConnectionConfig{Host: "fixture"}, WithTransportFactory(func(ctx context.Context, _ ConnectionConfig) (io.ReadWriteCloser, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- d.Connect(ctx) }()
	<-entered
	if err := d.Close(); err != nil {
		t.Errorf("Close rejected active dial: %v", err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("lost cancellation: %v", err)
		}
	case <-time.After(time.Second):
		cancel()
		<-done
		t.Error("Close did not interrupt dial")
	}
}

func TestAbnormalCallbackExitRetiresSession(t *testing.T) {
	for _, which := range []string{"panic", "goexit"} {
		t.Run(which, func(t *testing.T) {
			d, _ := fixtureDevice(t, fixtureProfile(), []string{"Question:"})
			done := make(chan struct{})
			go func() {
				defer close(done)
				defer func() {
					if which == "panic" && recover() == nil {
						t.Error("panic swallowed")
					}
				}()
				_, _ = d.ExecuteCommand(t.Context(), Command{Text: "change", Interactions: []Interaction{{Pattern: testPattern("Question:"), Respond: func(context.Context, string) (string, error) {
					if which == "panic" {
						panic("callback")
					}
					runtime.Goexit()
					return "", nil
				}}}}, ExecuteOptions{})
			}()
			<-done
			if d.Connected() {
				t.Error("uncertain session remains reusable")
			}
		})
	}
}

type partialFactoryStream struct {
	closed bool
	cause  error
}

func (*partialFactoryStream) Read([]byte) (int, error)    { return 0, io.EOF }
func (*partialFactoryStream) Write(p []byte) (int, error) { return len(p), nil }
func (s *partialFactoryStream) Close() error              { s.closed = true; return s.cause }
func TestFactoryPartialReturnIsOwned(t *testing.T) {
	primary, cleanup := errors.New("setup"), errors.New("cleanup")
	stream := &partialFactoryStream{cause: cleanup}
	d, err := New(fixtureProfile(), ConnectionConfig{Host: "fixture"}, WithTransportFactory(func(context.Context, ConnectionConfig) (io.ReadWriteCloser, error) { return stream, primary }))
	if err != nil {
		t.Fatal(err)
	}
	err = d.Connect(t.Context())
	if !stream.closed || !errors.Is(err, primary) || !errors.Is(err, cleanup) {
		t.Fatalf("closed=%v err=%v", stream.closed, err)
	}
}

func TestTypedNilLoggerRejected(t *testing.T) {
	var writer *net.TCPConn
	if _, err := New(fixtureProfile(), ConnectionConfig{Host: "fixture", Logger: writer}); err == nil {
		t.Fatal("typed nil logger accepted")
	}
}

func TestCloseFromReceiptCancelsScopeAndKeepsStep(t *testing.T) {
	d, _ := fixtureDevice(t, fixtureProfile(), []string{"done\nsw#"})
	r, err := d.ExecuteCommand(t.Context(), Command{Text: "show"}, ExecuteOptions{OnReceipt: func(ctx context.Context, _ StepResult) error {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Error("callback context not cancelled")
		}
		return nil
	}})
	if !errors.Is(err, context.Canceled) || len(r.Steps()) != 1 {
		t.Fatalf("steps=%d err=%v", len(r.Steps()), err)
	}
	if err := d.Connect(t.Context()); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed device revived: %v", err)
	}
}

func TestTypedNilFactoryStreamRejected(t *testing.T) {
	d, err := New(fixtureProfile(), ConnectionConfig{Host: "fixture"}, WithTransportFactory(func(context.Context, ConnectionConfig) (io.ReadWriteCloser, error) {
		var stream *net.TCPConn
		return stream, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Connect(t.Context()); !errors.Is(err, ErrConnection) {
		t.Fatalf("invalid stream: %v", err)
	}
}

func TestInteractionZeroLimitRepliesOnlyOnce(t *testing.T) {
	d, writes := fixtureDevice(t, fixtureProfile(), []string{"Question:", "Question:"})
	_, err := d.ExecuteCommand(t.Context(), Command{Text: "change", Interactions: []Interaction{{Pattern: testPattern("Question:"), Reply: "yes\n"}}}, ExecuteOptions{})
	if !errors.Is(err, ErrResponseLimit) {
		t.Fatalf("second default response accepted: %v", err)
	}
	d.Close()
	if len(*writes) != 2 {
		t.Fatalf("sent %d lines, want command and one reply", len(*writes))
	}
}

func TestExplicitUnlimitedRepliesAndPollValidation(t *testing.T) {
	if err := validateInteractions([]Interaction{{Pattern: testPattern("Question:"), Limit: 2, Unlimited: true}}); err == nil {
		t.Fatal("ambiguous response limit accepted")
	}
	d, _ := fixtureDevice(t, fixtureProfile(), []string{"Question:", "Question:", "done\nsw#"})
	_, err := d.ExecuteCommand(t.Context(), Command{Text: "change", Interactions: []Interaction{{Pattern: testPattern("Question:"), Reply: "yes\n", Unlimited: true}}}, ExecuteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	positive := time.Second
	if err := (Command{Text: "show", Poll: true, Timeout: &positive}).validate(); err == nil {
		t.Fatal("poll with relative wait accepted")
	}
}
