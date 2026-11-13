package connector

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"regexp"
	"slices"
	"sync"
	"testing"
	"time"
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
	d, err := New(fixtureDriver(), ConnectionConfig{Host: "fixture"}, WithTransportFactory(func(context.Context, ConnectionConfig) (io.ReadWriteCloser, error) { return stream, primary }))
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
	shared := LoginState{Labels: map[string]string{"ha_state": "active"}}
	p := fixtureDriver()
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
	shared.Labels["ha_state"] = "standby"
	if b.LoginLabel("ha_state") != "active" || a.LoginLabel("ha_state") != "" {
		t.Fatal("login labels are shared with the driver or survive Close")
	}
}

type renderedStep interface{ Rendered() (string, error) }

func TestRenderedSnapshotPreservesOutput(t *testing.T) {
	d, _ := fixtureDevice(t, fixtureDriver(), []string{"old\rnew\x1b[K\r\nsw#"})
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
	for _, level := range []LogLevel{"", LogInfo} {
		l := &logWriter{level: level, encoding: "auto"}
		if level == LogInfo {
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

func TestPrivilegedPromptIsDeclaredByTheTerminal(t *testing.T) {
	for _, tt := range []struct {
		name       string
		privileged *regexp.Regexp
		expected   []string
	}{
		{"undeclared bracket prompt still enters privilege", nil, []string{"enable", "show"}},
		{"declared bracket prompt skips the privilege command", regexp.MustCompile(`\]\s*\z`), []string{"show"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			prompt := testPattern(`\[sw\]\s*\z`)
			grammar := Terminal{
				Name: "fixture", LoginPrompt: prompt, CommandPrompt: prompt,
				PrivilegeCommand: "enable", PrivilegePrompt: prompt, PrivilegedPrompt: tt.privileged,
			}
			var mu sync.Mutex
			var writes []string
			done := make(chan struct{})
			factory := func(context.Context, ConnectionConfig) (io.ReadWriteCloser, error) {
				a, b := net.Pipe()
				go func() {
					defer close(done)
					defer b.Close()
					io.WriteString(b, "[sw]")
					scan := bufio.NewScanner(b)
					for scan.Scan() {
						mu.Lock()
						writes = append(writes, scan.Text())
						mu.Unlock()
						io.WriteString(b, "\r\n[sw]")
					}
				}()
				return a, nil
			}
			d, err := New(StaticDriver{Grammar: grammar}, ConnectionConfig{Host: "fixture", LoginTimeout: time.Second, CommandTimeout: time.Second}, WithTransportFactory(factory))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = d.ExecuteCommand(t.Context(), Command{Text: "show"}, ExecuteOptions{}); err != nil {
				t.Fatal(err)
			}
			if err = d.Close(); err != nil {
				t.Fatal(err)
			}
			<-done
			mu.Lock()
			defer mu.Unlock()
			if !slices.Equal(writes, tt.expected) {
				t.Fatalf("device writes = %q, want %q", writes, tt.expected)
			}
		})
	}
	bad := fixtureTerminal()
	bad.PrivilegedPrompt = regexp.MustCompile(`x*`)
	if err := bad.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty-matching privileged prompt accepted: %v", err)
	}
}
