package connector

import (
	"context"
	"errors"
	"io"
	"testing"
)

type retryFactoryStream struct {
	calls   int
	cleanup error
}

func (*retryFactoryStream) Read([]byte) (int, error)    { return 0, io.EOF }
func (*retryFactoryStream) Write(p []byte) (int, error) { return len(p), nil }
func (s *retryFactoryStream) Close() error {
	s.calls++
	if s.calls < 3 {
		return s.cleanup
	}
	return nil
}
func TestFactoryPartialReturnRetainsCleanupOwner(t *testing.T) {
	primary, cleanup := errors.New("setup"), errors.New("cleanup")
	stream := &retryFactoryStream{cleanup: cleanup}
	d, err := New(fixtureProfile(), ConnectionConfig{Host: "fixture"}, WithTransportFactory(func(context.Context, ConnectionConfig) (io.ReadWriteCloser, error) { return stream, primary }))
	if err != nil {
		t.Fatal(err)
	}
	err = d.Connect(t.Context())
	if !errors.Is(err, primary) || !errors.Is(err, cleanup) {
		t.Fatalf("lost errors: %v", err)
	}
	for range 3 {
		_ = d.Close()
	}
	if stream.calls != 3 {
		t.Fatalf("cleanup calls=%d, want 3", stream.calls)
	}
}
func TestAfterLoginStateHasValueSemantics(t *testing.T) {
	shared := LoginState{HAState: "active"}
	p := fixtureProfile()
	p.AfterLogin = func(context.Context, StepResult) (LoginState, error) { return shared, nil }
	a, _ := fixtureDevice(t, p, nil)
	b, _ := fixtureDevice(t, p, nil)
	if err := a.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := b.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if shared.HAState != "active" || b.HAState() != "active" {
		t.Fatal("close modified borrowed login state")
	}
}

type renderedStep interface{ Rendered() (string, error) }

func TestRenderedSnapshotPreservesOutput(t *testing.T) {
	d, _ := fixtureDevice(t, fixtureProfile(), []string{"old\rnew\x1b[K\r\nsw#"})
	r, err := d.ExecuteCommand(t.Context(), Command{Text: "show"}, ExecuteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	step := r.Steps()[0]
	view, ok := any(step.Response()).(renderedStep)
	if !ok {
		t.Fatal("missing Rendered accessor")
	}
	got, err := view.Rendered()
	if err != nil || got != "new\nsw#" {
		t.Fatalf("rendered=%q err=%v", got, err)
	}
	if step.Response().Raw() != "old\rnew\x1b[K\r\nsw#" || testStepOutput(step) != step.Response().Raw() {
		t.Fatal("rendering changed raw or output contract")
	}
	if err = d.Close(); err != nil {
		t.Fatal(err)
	}
	got, err = view.Rendered()
	if err != nil || got != "new\nsw#" {
		t.Fatal("view lost after Close")
	}
}
func TestDisabledEchoDoesNotBuildPipeline(t *testing.T) {
	for _, level := range []string{"", "info"} {
		l := &logWriter{level: level, encoding: "auto"}
		if level == "info" {
			l.target = io.Discard
		}
		if err := l.scope([]string{"secret"}, false); err != nil {
			t.Fatal(err)
		}
		counted := 0
		l.count = func(n int) { counted += n }
		if _, err := l.Write([]byte("secret")); err != nil || counted != 6 {
			t.Fatal("lost budget counting")
		}
		if l.redactor != nil || l.renderer != nil || l.rendered != nil {
			t.Fatal("disabled echo constructed pipeline")
		}
	}
}
