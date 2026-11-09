package workflow_test

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	expect "github.com/gatework/go-expect"

	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/fleet"
	"github.com/gatework/go-net-connector/internal/workflow"
	"github.com/gatework/go-net-connector/inventory"
)

func mustPattern(t *testing.T, expression string) expect.Pattern {
	t.Helper()
	p, err := expect.Compile(expression)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// fakeSwitch answers every line with its echo, an optional response and the
// prompt. Lines containing "bad" are rejected; "write memory" replies with
// saveReply. It records every line it received.
type fakeSwitch struct {
	saveReply string
	mu        sync.Mutex
	received  map[string][]string
}

func (f *fakeSwitch) lines(host string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.received[host]...)
}

func (f *fakeSwitch) opener(t *testing.T, opened *atomic.Int32) workflow.DeviceOpener {
	return func(_ context.Context, r fleet.Record, logger io.Writer, _ workflow.SessionObserver) (*connector.Device, error) {
		opened.Add(1)
		profile := connector.Profile{
			Name: r.Vendor, LoginPrompt: mustPattern(t, `sw#\s*\z`), CommandPrompt: mustPattern(t, `sw#\s*\z`),
			CommandErrors: []*regexp.Regexp{regexp.MustCompile(`(?m)^% Invalid input`)},
			SaveCommands:  []string{"write memory"},
			ValidateSave: func(_ context.Context, result connector.Result) error {
				out, err := result.JoinedOutput()
				if err != nil || !strings.Contains(out, "[OK]") {
					return connector.ErrUnconfirmed
				}
				return nil
			},
		}
		config := connector.ConnectionConfig{Host: r.Host, Username: "operator", Password: "secret", Protocol: connector.ProtocolSSH, Logger: logger}
		return connector.New(profile, config, connector.WithTransportFactory(func(context.Context, connector.ConnectionConfig) (io.ReadWriteCloser, error) {
			a, b := net.Pipe()
			go func() {
				defer b.Close()
				_, _ = io.WriteString(b, "sw#")
				reader := bufio.NewReader(b)
				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						return
					}
					line = strings.TrimRight(line, "\r\n")
					f.mu.Lock()
					f.received[r.Host] = append(f.received[r.Host], line)
					f.mu.Unlock()
					reply := ""
					switch {
					case strings.Contains(line, "bad"):
						reply = "% Invalid input detected\r\n"
					case line == "write memory":
						reply = f.saveReply + "\r\n"
					}
					if _, err = io.WriteString(b, line+"\r\n"+reply+"sw#"); err != nil {
						return
					}
				}
			}()
			return a, nil
		}))
	}
}

func pushPlan(t *testing.T, rows []inventory.Device) (fleet.Plan, workflow.Runtime) {
	t.Helper()
	s, _ := planViewServices(t, "backup")
	plan, err := fleet.NewPlan(rows, s.Rules(), fleet.PlanOptions{Mode: "backup"})
	if err != nil {
		t.Fatal(err)
	}
	runtime := s.WorkflowRuntime()
	runtime.LogDirectory = nil
	return plan, runtime
}

func parseScript(t *testing.T, text string) connector.Script {
	t.Helper()
	script, err := connector.ParseScript(text, "change.txt")
	if err != nil {
		t.Fatal(err)
	}
	return script
}

func TestPushPlanNeverOpensDevicesAndSkipsPlatformsWithoutScript(t *testing.T) {
	plan, runtime := pushPlan(t, []inventory.Device{{IP: "192.0.2.1", Vendor: "cisco", OS: "ios"}, {IP: "192.0.2.2", Vendor: "huawei"}})
	var opened atomic.Int32
	runtime.OpenDevice = (&fakeSwitch{received: map[string][]string{}}).opener(t, &opened)
	scripts := workflow.PushScripts{ByPlatform: map[string]connector.Script{"cisco_ios": parseScript(t, "interface Gi0/1\n description uplink\n")}}
	result, err := (&workflow.Runner{}).Push(t.Context(), runtime, plan, workflow.PushOptions{Scripts: scripts, Plan: true, Directory: t.TempDir()})
	if err != nil || opened.Load() != 0 {
		t.Fatalf("plan opened %d devices: %v", opened.Load(), err)
	}
	got := result.Push.Plan
	if len(got.Selected) != 1 || got.Selected[0].Platform != "cisco_ios" || got.Selected[0].Commands != 2 || got.Skipped["missing_script"] != 1 || result.Push.Report != nil {
		t.Fatalf("unexpected plan: %+v", got)
	}
}

func TestPushRecordsRejectionWithoutSendingLaterCommands(t *testing.T) {
	plan, runtime := pushPlan(t, []inventory.Device{{IP: "192.0.2.1", Vendor: "cisco", OS: "ios"}, {IP: "192.0.2.2", Vendor: "cisco", OS: "ios"}})
	fake := &fakeSwitch{saveReply: "[OK]", received: map[string][]string{}}
	var opened atomic.Int32
	runtime.OpenDevice = fake.opener(t, &opened)
	runtime.Workers = 1
	good := parseScript(t, "vlan 10\nname users\n")
	scripts := workflow.PushScripts{Default: &good}
	result, err := (&workflow.Runner{}).Push(t.Context(), runtime, plan, workflow.PushOptions{Scripts: scripts, Save: true, Directory: t.TempDir()})
	if err != nil || !result.Success || result.Push.Report.Counts["saved"] != 2 {
		t.Fatalf("saved push failed: %v %+v", err, result.Push.Report)
	}
	if lines := fake.lines("192.0.2.1"); strings.Join(lines, "|") != "vlan 10|name users|write memory" {
		t.Fatalf("unexpected device input: %q", lines)
	}

	bad := parseScript(t, "vlan 20\nbad command\nname never-sent\n")
	fake.received = map[string][]string{}
	result, err = (&workflow.Runner{}).Push(t.Context(), runtime, plan, workflow.PushOptions{Scripts: workflow.PushScripts{Default: &bad}, Save: true, Directory: t.TempDir()})
	if err != nil || result.Success {
		t.Fatalf("rejected push reported success: %v", err)
	}
	out := result.Push.Report.Devices[0]
	if out.Status != workflow.PushStatusRejected || out.AcceptedCommands != 1 || out.FailedLine != 2 || out.ErrorCode != "device_error" || len(out.Attempts) != 2 || !out.Attempts[1].Failed || out.Attempts[1].Written == 0 {
		t.Fatalf("rejection evidence lost: %+v", out)
	}
	if lines := fake.lines("192.0.2.1"); strings.Join(lines, "|") != "vlan 20|bad command" {
		t.Fatalf("commands after a rejection or a save were sent: %q", lines)
	}
}

func TestPushSaveWithoutConfirmationIsNotSaved(t *testing.T) {
	plan, runtime := pushPlan(t, []inventory.Device{{IP: "192.0.2.1", Vendor: "cisco", OS: "ios"}})
	var opened atomic.Int32
	runtime.OpenDevice = (&fakeSwitch{saveReply: "Building configuration...", received: map[string][]string{}}).opener(t, &opened)
	script := parseScript(t, "vlan 30\n")
	result, err := (&workflow.Runner{}).Push(t.Context(), runtime, plan, workflow.PushOptions{Scripts: workflow.PushScripts{Default: &script}, Save: true, Directory: t.TempDir()})
	if err != nil || result.Success {
		t.Fatalf("unconfirmed save reported success: %v", err)
	}
	if out := result.Push.Report.Devices[0]; out.Status != workflow.PushStatusSaveUnconfirmed || out.AcceptedCommands != 1 {
		t.Fatalf("unexpected outcome: %+v", out)
	}
}

func TestPushStopsStartingDevicesAfterFailureLimit(t *testing.T) {
	plan, runtime := pushPlan(t, []inventory.Device{{IP: "192.0.2.1", Vendor: "cisco", OS: "ios"}, {IP: "192.0.2.2", Vendor: "cisco", OS: "ios"}, {IP: "192.0.2.3", Vendor: "cisco", OS: "ios"}})
	var opened atomic.Int32
	runtime.OpenDevice = func(context.Context, fleet.Record, io.Writer, workflow.SessionObserver) (*connector.Device, error) {
		opened.Add(1)
		return nil, fleet.ErrMissingCredentials
	}
	runtime.Workers = 1
	script := parseScript(t, "vlan 40\n")
	result, err := (&workflow.Runner{}).Push(t.Context(), runtime, plan, workflow.PushOptions{Scripts: workflow.PushScripts{Default: &script}, StopAfterFailures: 1, Directory: t.TempDir()})
	if err != nil || result.Success || opened.Load() != 1 {
		t.Fatalf("failure limit ignored: opened=%d err=%v", opened.Load(), err)
	}
	report := result.Push.Report
	if !report.Stopped || report.Counts["failed"] != 1 || report.Counts["stopped"] != 2 || report.Devices[0].ErrorCode != "missing_credentials" {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestPushRejectsNegativeFailureLimit(t *testing.T) {
	plan, runtime := pushPlan(t, []inventory.Device{{IP: "192.0.2.1", Vendor: "cisco", OS: "ios"}})
	_, err := (&workflow.Runner{}).Push(t.Context(), runtime, plan, workflow.PushOptions{StopAfterFailures: -1, Directory: t.TempDir()})
	if !errors.Is(err, workflow.ErrInvalidPush) {
		t.Fatalf("negative limit accepted: %v", err)
	}
}

func TestPushWithSaveSkipsPlatformsWithoutSaveScript(t *testing.T) {
	plan, runtime := pushPlan(t, []inventory.Device{{IP: "192.0.2.1", Vendor: "juniper", OS: "junos"}, {IP: "192.0.2.2", Vendor: "cisco", OS: "ios"}})
	var opened atomic.Int32
	runtime.OpenDevice = (&fakeSwitch{saveReply: "[OK]", received: map[string][]string{}}).opener(t, &opened)
	script := parseScript(t, "set system ntp server 192.0.2.50\n")
	result, err := (&workflow.Runner{}).Push(t.Context(), runtime, plan, workflow.PushOptions{Scripts: workflow.PushScripts{Default: &script}, Save: true, Directory: t.TempDir()})
	if err != nil || !result.Success || opened.Load() != 1 {
		t.Fatalf("opened=%d err=%v", opened.Load(), err)
	}
	if got := result.Push.Plan; got.Skipped["save_unsupported"] != 1 || len(got.Selected) != 1 || got.Selected[0].Platform != "cisco_ios" {
		t.Fatalf("unsaveable platform was selected: %+v", got)
	}
}
