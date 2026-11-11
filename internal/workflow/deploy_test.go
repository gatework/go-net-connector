package workflow_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	expect "github.com/gatework/go-expect"

	"github.com/gatework/go-net-connector/config"
	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/fleet"
	"github.com/gatework/go-net-connector/internal/batchreport"
	"github.com/gatework/go-net-connector/internal/bootstrap"
	"github.com/gatework/go-net-connector/internal/workflow"
	"github.com/gatework/go-net-connector/inventory"
	"github.com/gatework/go-net-connector/transfer"
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
	t.Helper()
	return func(_ context.Context, r fleet.Record, logger io.Writer, _ workflow.SessionObserver) (*connector.Device, error) {
		opened.Add(1)
		profile := connector.StaticDriver{
			Grammar: connector.Terminal{
				Name: r.Vendor, LoginPrompt: mustPattern(t, `sw#\s*\z`), CommandPrompt: mustPattern(t, `sw#\s*\z`),
				CommandErrors:  []*regexp.Regexp{regexp.MustCompile(`(?m)^% Invalid input`)},
				SaveCommands:   []string{"write memory"},
				ConfigCommands: []string{"show running-config"},
			},
			Save: func(_ context.Context, result connector.Result) error {
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
					case line == "show running-config":
						reply = "hostname sw\r\ninterface Gi0/1\r\n"
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

func testServices(t *testing.T) *bootstrap.Services {
	t.Helper()
	root := t.TempDir()
	defaults := config.Defaults("backup")
	defaults["backup"].(map[string]any)["directory"] = filepath.Join(root, "backups")
	p, err := config.Load(context.Background(), config.Options{Name: "backup", CWD: root, Home: root, Directories: []string{}, Environment: map[string]string{}, Defaults: defaults, AllowMissing: true})
	if err != nil {
		t.Fatal(err)
	}
	s, err := bootstrap.New(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func deployPlan(t *testing.T, rows []inventory.Device) (fleet.Plan, workflow.Runtime) {
	t.Helper()
	s := testServices(t)
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

func TestDeployPlanNeverOpensDevicesAndSkipsPlatformsWithoutScript(t *testing.T) {
	plan, runtime := deployPlan(t, []inventory.Device{{IP: "192.0.2.1", Vendor: "cisco", OS: "ios"}, {IP: "192.0.2.2", Vendor: "huawei"}})
	var opened atomic.Int32
	runtime.OpenDevice = (&fakeSwitch{received: map[string][]string{}}).opener(t, &opened)
	scripts := workflow.DeployScripts{ByPlatform: map[string]connector.Script{"cisco_ios": parseScript(t, "interface Gi0/1\n description uplink\n")}}
	result, err := (&workflow.Runner{}).Deploy(t.Context(), runtime, plan, workflow.DeployOptions{Scripts: scripts, Plan: true, Directory: t.TempDir(), Policy: "selected"})
	if err != nil || opened.Load() != 0 || !result.Planned {
		t.Fatalf("plan opened %d devices: %v", opened.Load(), err)
	}
	devices := result.Report.Devices
	if devices[0].Status != batchreport.StatusNotStarted || devices[0].Target.Script != "change.txt" || devices[0].Target.Commands != 2 || devices[1].Status != "missing_script" {
		t.Fatalf("unexpected plan: %+v", devices)
	}
}

func TestDeployRecordsRejectionWithoutSendingLaterCommands(t *testing.T) {
	plan, runtime := deployPlan(t, []inventory.Device{{IP: "192.0.2.1", Vendor: "cisco", OS: "ios"}, {IP: "192.0.2.2", Vendor: "cisco", OS: "ios"}})
	fake := &fakeSwitch{saveReply: "[OK]", received: map[string][]string{}}
	var opened atomic.Int32
	runtime.OpenDevice = fake.opener(t, &opened)
	runtime.Workers = 1
	good := parseScript(t, "vlan 10\nname users\n")
	result, err := (&workflow.Runner{}).Deploy(t.Context(), runtime, plan, workflow.DeployOptions{Scripts: workflow.DeployScripts{Default: &good}, Save: true, Backup: true, Directory: t.TempDir(), Policy: "selected"})
	if err != nil || !result.Success || result.Report.Counts[batchreport.StatusSucceeded] != 2 {
		t.Fatalf("deploy failed: %v %+v", err, result.Report)
	}
	first := result.Report.Devices[0]
	if strings.Join(stageNames(first), ",") != "authenticate,execute,save,backup" || first.Outcome != "deployed" || !first.Artifacts.Save.Confirmed || first.Artifacts.Backup == nil || first.Artifacts.Backup.Bytes == 0 {
		t.Fatalf("pipeline evidence: %+v", first)
	}
	if lines := fake.lines("192.0.2.1"); strings.Join(lines, "|") != "vlan 10|name users|write memory|show running-config" {
		t.Fatalf("unexpected device input: %q", lines)
	}

	bad := parseScript(t, "vlan 20\nbad command\nname never-sent\n")
	fake.received = map[string][]string{}
	result, err = (&workflow.Runner{}).Deploy(t.Context(), runtime, plan, workflow.DeployOptions{Scripts: workflow.DeployScripts{Default: &bad}, Save: true, Backup: true, Directory: t.TempDir(), Policy: "selected"})
	if err != nil || result.Success {
		t.Fatalf("rejected deploy reported success: %v", err)
	}
	out := result.Report.Devices[0]
	script := out.Artifacts.Script
	if out.Status != batchreport.StatusPartial || out.FailedStage != "execute" || out.Outcome != "rejected" || out.Diagnostic.Code != "device_error" || script.Accepted != 1 || script.FailedLine != 2 || len(script.Attempts) != 2 || !script.Attempts[1].Failed || script.Attempts[1].Written == 0 {
		t.Fatalf("rejection evidence lost: %+v %+v", out, script)
	}
	if got := stageStatuses(out); got != "authenticate:succeeded,execute:failed,save:skipped,backup:skipped" {
		t.Fatalf("later stages ran: %s", got)
	}
	if lines := fake.lines("192.0.2.1"); strings.Join(lines, "|") != "vlan 20|bad command" {
		t.Fatalf("commands after a rejection were sent: %q", lines)
	}
}

func TestDeploySaveWithoutConfirmationIsPartial(t *testing.T) {
	plan, runtime := deployPlan(t, []inventory.Device{{IP: "192.0.2.1", Vendor: "cisco", OS: "ios"}})
	var opened atomic.Int32
	runtime.OpenDevice = (&fakeSwitch{saveReply: "Building configuration...", received: map[string][]string{}}).opener(t, &opened)
	script := parseScript(t, "vlan 30\n")
	result, err := (&workflow.Runner{}).Deploy(t.Context(), runtime, plan, workflow.DeployOptions{Scripts: workflow.DeployScripts{Default: &script}, Save: true, Directory: t.TempDir(), Policy: "selected"})
	if err != nil || result.Success {
		t.Fatalf("unconfirmed save reported success: %v", err)
	}
	out := result.Report.Devices[0]
	if out.Status != batchreport.StatusPartial || out.FailedStage != "save" || out.Artifacts.Save.Confirmed || out.Artifacts.Script.Accepted != 1 {
		t.Fatalf("unexpected outcome: %+v", out)
	}
}

func TestDeployStopsStartingDevicesAfterFailureLimit(t *testing.T) {
	plan, runtime := deployPlan(t, []inventory.Device{{IP: "192.0.2.1", Vendor: "cisco", OS: "ios"}, {IP: "192.0.2.2", Vendor: "cisco", OS: "ios"}, {IP: "192.0.2.3", Vendor: "cisco", OS: "ios"}})
	var opened atomic.Int32
	runtime.OpenDevice = func(context.Context, fleet.Record, io.Writer, workflow.SessionObserver) (*connector.Device, error) {
		opened.Add(1)
		return nil, fleet.ErrMissingCredentials
	}
	runtime.Workers = 1
	script := parseScript(t, "vlan 40\n")
	result, err := (&workflow.Runner{}).Deploy(t.Context(), runtime, plan, workflow.DeployOptions{Scripts: workflow.DeployScripts{Default: &script}, StopAfterFailures: 1, Directory: t.TempDir(), Policy: "selected"})
	if err != nil || result.Success || opened.Load() != 1 {
		t.Fatalf("failure limit ignored: opened=%d err=%v", opened.Load(), err)
	}
	report := result.Report
	first := report.Devices[0]
	if !report.Stopped || report.Counts[batchreport.StatusFailed] != 1 || report.Counts[batchreport.StatusStopped] != 2 || first.FailedStage != "authenticate" || first.Diagnostic.Code != "missing_credentials" {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestDeployRejectsNegativeFailureLimit(t *testing.T) {
	plan, runtime := deployPlan(t, []inventory.Device{{IP: "192.0.2.1", Vendor: "cisco", OS: "ios"}})
	_, err := (&workflow.Runner{}).Deploy(t.Context(), runtime, plan, workflow.DeployOptions{StopAfterFailures: -1, Directory: t.TempDir()})
	if !errors.Is(err, workflow.ErrInvalidRequest) {
		t.Fatalf("negative limit accepted: %v", err)
	}
}

func TestDeployWithSaveSkipsPlatformsWithoutSaveScript(t *testing.T) {
	plan, runtime := deployPlan(t, []inventory.Device{{IP: "192.0.2.1", Vendor: "juniper", OS: "junos"}, {IP: "192.0.2.2", Vendor: "cisco", OS: "ios"}})
	var opened atomic.Int32
	runtime.OpenDevice = (&fakeSwitch{saveReply: "[OK]", received: map[string][]string{}}).opener(t, &opened)
	script := parseScript(t, "set system ntp server 192.0.2.50\n")
	result, err := (&workflow.Runner{}).Deploy(t.Context(), runtime, plan, workflow.DeployOptions{Scripts: workflow.DeployScripts{Default: &script}, Save: true, Directory: t.TempDir(), Policy: "selected"})
	if err != nil || opened.Load() != 1 {
		t.Fatalf("opened=%d err=%v", opened.Load(), err)
	}
	if got := result.Report.Devices[0].Status; got != "save_unsupported" {
		t.Fatalf("unsaveable platform was admitted: %s", got)
	}
}

func stageNames(d batchreport.Device) []string {
	names := []string{}
	for _, s := range d.Stages {
		names = append(names, s.Name)
	}
	return names
}

func stageStatuses(d batchreport.Device) string {
	parts := []string{}
	for _, s := range d.Stages {
		parts = append(parts, s.Name+":"+s.Status)
	}
	return strings.Join(parts, ",")
}

func TestExecSavesReadOnlyOutputsAndRefusesWrites(t *testing.T) {
	plan, runtime := deployPlan(t, []inventory.Device{{IP: "192.0.2.1", Vendor: "cisco", OS: "ios", Name: "core"}})
	fake := &fakeSwitch{received: map[string][]string{}}
	var opened atomic.Int32
	runtime.OpenDevice = fake.opener(t, &opened)
	directory := t.TempDir()
	result, err := (&workflow.Runner{}).Exec(t.Context(), runtime, plan, workflow.ExecOptions{Commands: []string{"show running-config"}, Directory: directory, Policy: "selected"})
	if err != nil || !result.Success {
		t.Fatalf("exec failed: %v %+v", err, result.Report)
	}
	device := result.Report.Devices[0]
	if device.Outcome != "executed" || len(device.Artifacts.Outputs) != 1 {
		t.Fatalf("outputs: %+v", device)
	}
	body, err := os.ReadFile(device.Artifacts.Outputs[0].Path)
	if err != nil || string(body) != "### show running-config\nhostname sw\ninterface Gi0/1\n" {
		t.Fatalf("saved output %q %v", body, err)
	}
	if filepath.Base(device.Artifacts.Outputs[0].Path) != "core-192.0.2.1.txt" {
		t.Fatal(device.Artifacts.Outputs[0].Path)
	}

	opened.Store(0)
	result, err = (&workflow.Runner{}).Exec(t.Context(), runtime, plan, workflow.ExecOptions{Commands: []string{"show version", "configure terminal"}, Directory: t.TempDir(), Policy: "selected"})
	if err != nil || result.Success || opened.Load() != 0 || result.Report.Devices[0].Status != "not_read_only" {
		t.Fatalf("write command admitted: opened=%d %+v %v", opened.Load(), result.Report, err)
	}
}

func TestRemoteBackupExportsOnlyCapablePlatforms(t *testing.T) {
	plan, runtime := deployPlan(t, []inventory.Device{{IP: "192.0.2.1", Vendor: "juniper", OS: "junos"}, {IP: "192.0.2.2", Vendor: "cisco", OS: "ios"}})
	var commands []string
	var opened atomic.Int32
	runtime.OpenDevice = func(_ context.Context, r fleet.Record, logger io.Writer, _ workflow.SessionObserver) (*connector.Device, error) {
		opened.Add(1)
		driver := connector.StaticDriver{Grammar: connector.Terminal{Name: r.Vendor, LoginPrompt: mustPattern(t, `ops@mx>\s*\z`), CommandPrompt: mustPattern(t, `ops@mx>\s*\z`)}}
		return connector.New(driver, connector.ConnectionConfig{Host: r.Host, Username: "ops", Password: "device-secret", Logger: logger}, connector.WithTransportFactory(func(context.Context, connector.ConnectionConfig) (io.ReadWriteCloser, error) {
			a, b := net.Pipe()
			go func() {
				defer b.Close()
				_, _ = io.WriteString(b, "ops@mx>")
				line, err := bufio.NewReader(b).ReadString('\n')
				if err != nil {
					return
				}
				line = strings.TrimSpace(line)
				commands = append(commands, line)
				destination := strings.TrimPrefix(line, "show configuration | save ")
				_, _ = io.WriteString(b, line+"\r\nWrote 42 lines of output to '"+destination+"'\r\nops@mx>")
				_, _ = io.Copy(io.Discard, b)
			}()
			return a, nil
		}))
	}
	options := workflow.RemoteBackupOptions{RemoteOptions: transfer.RemoteOptions{Server: "192.0.2.9", Path: "backups", Username: "backup", Protocol: "scp", Password: "remote-secret"}, Directory: t.TempDir(), Policy: "selected"}
	result, err := (&workflow.Runner{}).RemoteBackup(t.Context(), runtime, plan, options)
	if err != nil || opened.Load() != 1 {
		t.Fatalf("opened=%d err=%v", opened.Load(), err)
	}
	juniper, cisco := result.Report.Devices[0], result.Report.Devices[1]
	if juniper.Status != batchreport.StatusSucceeded || juniper.Outcome != "exported" || !juniper.Artifacts.Remote.Reported || cisco.Status != "unsupported_operation" {
		t.Fatalf("remote backup: %+v %+v", juniper, cisco)
	}
	if len(commands) != 1 || !strings.HasPrefix(commands[0], "show configuration | save scp://backup@192.0.2.9/backups/192.0.2.1-") || !strings.HasSuffix(commands[0], ".conf") {
		t.Fatalf("command: %q", commands)
	}
	data, _ := json.Marshal(result.Report)
	if strings.Contains(string(data), "remote-secret") {
		t.Fatal("password in report")
	}
	if _, err = (&workflow.Runner{}).RemoteBackup(t.Context(), runtime, plan, workflow.RemoteBackupOptions{RemoteOptions: options.RemoteOptions, Policy: "verified", Directory: t.TempDir()}); !errors.Is(err, workflow.ErrInvalidRequest) {
		t.Fatalf("verified policy accepted: %v", err)
	}
}
