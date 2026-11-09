package connector

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func fixtureDriver() StaticDriver { return StaticDriver{Grammar: fixtureTerminal()} }

func fixtureTerminal() Terminal {
	return Terminal{Name: "fixture", LoginPrompt: testPattern(`(?m)(?:^|[\r\n])sw#\s*$`), CommandPrompt: testPattern(`(?m)(?:^|[\r\n])sw#\s*$`), CommandErrors: []*regexp.Regexp{regexp.MustCompile(`(?mi)^% Invalid`)}, ConfigCommands: []string{"show config"}}
}
func fixtureDevice(t *testing.T, p StaticDriver, responses []string) (*Device, *[]string) {
	t.Helper()
	writes := new([]string)
	done := make(chan struct{})
	factory := func(ctx context.Context, _ ConnectionConfig) (io.ReadWriteCloser, error) {
		a, b := net.Pipe()
		go func() {
			defer close(done)
			defer b.Close()
			io.WriteString(b, "sw#")
			scan := bufio.NewScanner(b)
			for _, response := range responses {
				if !scan.Scan() {
					return
				}
				*writes = append(*writes, scan.Text())
				io.WriteString(b, response)
			}
			io.Copy(io.Discard, b)
		}()
		return a, nil
	}
	d, e := New(p, ConnectionConfig{Host: "fixture", LoginTimeout: time.Second, CommandTimeout: time.Second}, WithTransportFactory(factory))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		d.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("peer did not exit")
		}
	})
	return d, writes
}
func TestWholeScriptValidationBeforeConnection(t *testing.T) {
	var opens atomic.Int32
	d, e := New(fixtureDriver(), ConnectionConfig{Host: "test"}, WithTransportFactory(func(context.Context, ConnectionConfig) (io.ReadWriteCloser, error) {
		opens.Add(1)
		return nil, errors.New("unexpected")
	}))
	if e != nil {
		t.Fatal(e)
	}
	_, e = d.ExecuteScript(t.Context(), Script{Commands: []Command{{Text: "valid"}, {Text: "bad\ninjection"}}}, ExecuteOptions{})
	if e == nil || opens.Load() != 0 {
		t.Fatal("invalid script performed I/O", e)
	}
}
func TestReceiptSurvivesHookFailure(t *testing.T) {
	p := fixtureDriver()
	hookErr := errors.New("secret callback body")
	p.AfterCommand = func(ctx context.Context, _ Command, _ StepResult, executor Executor) error {
		if _, err := executor.Execute(ctx, Command{Text: "hook"}); err != nil {
			return err
		}
		return hookErr
	}
	d, writes := fixtureDevice(t, p, []string{"show\r\nvalue\r\nsw#", "hook\r\ndone\r\nsw#"})
	receipts := 0
	r, e := d.ExecuteScript(t.Context(), Script{Commands: []Command{{Text: "show"}, {Text: "next"}}}, ExecuteOptions{OnReceipt: func(context.Context, StepResult) error { receipts++; return nil }})
	if !errors.Is(e, hookErr) || len(r.Steps()) != 1 || len(r.Attempts()) != 2 || receipts != 1 {
		t.Fatalf("steps=%d receipts=%d err=%v", len(r.Steps()), receipts, e)
	}
	step, err := r.SingleAccepted("show")
	if err != nil {
		t.Fatal(err)
	}
	if body, err := step.Body(); err != nil || body != "value\n" {
		t.Fatalf("accepted body=%q err=%v", body, err)
	}
	if strings.Contains(fmt.Sprintf("%+v %#v", r, e), "secret") {
		t.Fatal("secret in diagnostics")
	}
	d.Close()
	if len(*writes) != 2 || (*writes)[1] != "hook" {
		t.Fatal(*writes)
	}
}
func TestDeviceRejectionProducesNoReceipt(t *testing.T) {
	d, _ := fixtureDevice(t, fixtureDriver(), []string{"show\n% Invalid input\nsw#"})
	r, e := d.ExecuteCommand(t.Context(), Command{Text: "show"}, ExecuteOptions{})
	if !errors.Is(e, ErrDevice) || len(r.Steps()) != 0 {
		t.Fatalf("%+v %v", r, e)
	}
	if _, err := r.SingleAccepted("show"); !errors.Is(err, ErrInvalidResult) {
		t.Fatalf("rejected command produced accepted receipt: %v", err)
	}
}
func TestPagingAccumulatesRawAndExcludesMarker(t *testing.T) {
	p := fixtureDriver()
	p.Grammar.Pager = testPattern(`--More--`)
	p.Grammar.PagerResponse = "\n"
	d, w := fixtureDevice(t, p, []string{"one\n--More--", "two\n--More--", "three\nsw#"})
	r, e := d.ExecuteCommand(t.Context(), Command{Text: "show"}, ExecuteOptions{})
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(r.Steps()[0].Response().Raw(), "--More--") || strings.Contains(testResultOutput(r), "--More--") || !strings.Contains(testResultOutput(r), "three") {
		t.Fatalf("raw=%q out=%q", r.Steps()[0].Response().Raw(), testResultOutput(r))
	}
	step, err := r.SingleAccepted("show")
	if err != nil {
		t.Fatal(err)
	}
	if body, err := step.Body(); err != nil || body != "one\ntwo\nthree\n" {
		t.Fatalf("paged body=%q err=%v", body, err)
	}
	d.Close()
	if len(*w) != 3 {
		t.Fatal(*w)
	}
}
func TestObserverReentryBusyAndInputSnapshot(t *testing.T) {
	d, _ := fixtureDevice(t, fixtureDriver(), []string{"ok\nsw#"})
	r, e := d.ExecuteCommand(t.Context(), Command{Text: "show"}, ExecuteOptions{OnReceipt: func(ctx context.Context, _ StepResult) error {
		_, e := d.ExecuteCommand(ctx, Command{Text: "nested"}, ExecuteOptions{})
		if !errors.Is(e, ErrBusy) {
			t.Errorf("reentry=%v", e)
		}
		return nil
	}})
	if e != nil {
		t.Fatal(e)
	}
	steps := r.Steps()
	steps[0] = StepResult{}
	if testStepOutput(r.Steps()[0]) == "" {
		t.Fatal("result mutated")
	}
}

func TestHookExecutorExpiresBeforeReceiptCallback(t *testing.T) {
	var borrowed Executor
	p := fixtureDriver()
	p.AfterCommand = func(_ context.Context, _ Command, _ StepResult, x Executor) error { borrowed = x; return nil }
	d, _ := fixtureDevice(t, p, []string{"done\nsw#"})
	_, e := d.ExecuteCommand(t.Context(), Command{Text: "show"}, ExecuteOptions{OnReceipt: func(ctx context.Context, _ StepResult) error {
		_, e := borrowed.Execute(ctx, Command{Text: "forbidden"})
		if !errors.Is(e, ErrScope) {
			t.Errorf("borrowed scope still live: %v", e)
		}
		return nil
	}})
	if e != nil {
		t.Fatal(e)
	}
}

func TestPrivilegeRetainedAcrossOperations(t *testing.T) {
	p := fixtureDriver()
	p.Grammar.PrivilegeCommand = "enable"
	p.Grammar.PrivilegePrompt = p.Grammar.CommandPrompt
	d, w := fixtureDevice(t, p, []string{"sw#", "first\nsw#", "second\nsw#"})
	for range 2 {
		if _, e := d.ExecuteCommand(t.Context(), Command{Text: "show"}, ExecuteOptions{}); e != nil {
			t.Fatal(e)
		}
	}
	d.Close()
	if len(*w) != 3 || (*w)[0] != "enable" || (*w)[2] != "show" {
		t.Fatal(*w)
	}
}

func TestDiagnosticsRetainErasedAndColoredErrors(t *testing.T) {
	p := fixtureDriver()
	p.Grammar.CommandErrors = []*regexp.Regexp{regexp.MustCompile(`(?mi)^Error:`)}
	for _, response := range []string{"Err\x1b[31mor: denied\x1b[0m\nsw#", "Error: denied\rall good\nsw#"} {
		t.Run(response, func(t *testing.T) {
			d, _ := fixtureDevice(t, p, []string{response})
			r, e := d.ExecuteCommand(t.Context(), Command{Text: "show"}, ExecuteOptions{})
			if !errors.Is(e, ErrDevice) || len(r.Steps()) != 0 {
				t.Fatalf("%v %v", r, e)
			}
		})
	}
}
func TestCredentialValidationBeforeIO(t *testing.T) {
	for _, secret := range []string{"p\ncommand", "p\rcommand", "p\x00"} {
		if _, e := New(fixtureDriver(), ConnectionConfig{Host: "fixture", Password: secret}); e == nil {
			t.Fatal("credential accepted")
		}
	}
}
func TestExactScriptBudgetPreventsNextSend(t *testing.T) {
	d, w := fixtureDevice(t, fixtureDriver(), []string{"ok\nsw#"})
	r, e := d.ExecuteScript(t.Context(), Script{Commands: []Command{{Text: "one"}, {Text: "two"}}}, ExecuteOptions{MaxOutputBytes: 6})
	if !errors.Is(e, ErrScriptLimit) || len(r.Steps()) != 1 {
		t.Fatalf("%v %v", r, e)
	}
	d.Close()
	if len(*w) != 1 {
		t.Fatal(*w)
	}
}

func TestTelnetFallbackOnlyAfterConnectionRefused(t *testing.T) {
	for _, message := range []string{"Connection refused", "Permission denied"} {
		t.Run(message, func(t *testing.T) {
			var protocols []string
			var ports []int
			var peers sync.WaitGroup
			f := func(_ context.Context, c ConnectionConfig) (io.ReadWriteCloser, error) {
				protocols = append(protocols, string(c.Protocol))
				ports = append(ports, c.Port)
				a, b := net.Pipe()
				peers.Add(1)
				first := len(protocols) == 1
				go func() {
					defer peers.Done()
					defer b.Close()
					if first {
						io.WriteString(b, message)
						return
					}
					io.WriteString(b, "sw#")
					io.Copy(io.Discard, b)
				}()
				return a, nil
			}
			d, e := New(fixtureDriver(), ConnectionConfig{Host: "fixture", Port: 2222, AllowTelnetFallback: true}, WithTransportFactory(f))
			if e != nil {
				t.Fatal(e)
			}
			e = d.Connect(t.Context())
			d.Close()
			peers.Wait()
			if message == "Connection refused" {
				if e != nil || len(protocols) != 2 || protocols[1] != "telnet" || ports[1] != 0 {
					t.Fatalf("%v %v %v", e, protocols, ports)
				}
			} else if e == nil || len(protocols) != 1 {
				t.Fatalf("%v %v", e, protocols)
			}
		})
	}
}
