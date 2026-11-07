package fleet

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	expect "github.com/gatework/go-expect"
	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/inventory"
	"github.com/gatework/go-net-connector/vendors"
)

type diagnosticSink struct{}

func (diagnosticSink) Write([]byte) (int, error) { return 0, errors.New("SECRET_CAUSE") }

type brokenConfiguration struct{}

func (brokenConfiguration) MatchPrompt(_, current string) (expect.Pattern, error) {
	return expect.Compile(current)
}
func (brokenConfiguration) Validate(context.Context, connector.Command, connector.StepResult, connector.Executor) error {
	return errors.New("SECRET_CAUSE")
}
func (brokenConfiguration) Finish(context.Context, connector.Result) (string, error) {
	return "", errors.New("SECRET_CAUSE")
}

type incompleteValidation struct{ brokenConfiguration }

func (incompleteValidation) Validate(context.Context, connector.Command, connector.StepResult, connector.Executor) error {
	return connector.ErrIncomplete
}

type failedFinish struct{ brokenConfiguration }

func (failedFinish) Validate(context.Context, connector.Command, connector.StepResult, connector.Executor) error {
	return nil
}

func TestDeviceFailuresReachFleetVocabulary(t *testing.T) {
	for _, tc := range []struct {
		name, code, kind, phase string
		steps                   int
	}{
		{"login", "login_timeout", "LoginTimeout", "login", 0},
		{"command", "command_timeout", "CommandTimeout", "command", 0},
		{"prompt", "prompt_error", "PromptError", "command", 0},
		{"write", "write_timeout", "WriteTimeout", "command", 0},
		{"eof", "connection_closed", "ConnectionClosed", "command", 0},
		{"log", "log_error", "LogError", "logging", 0},
		{"output", "output_limit_exceeded", "OutputLimitExceeded", "command", 0},
		{"script", "script_output_limit_exceeded", "ScriptOutputLimitExceeded", "script", 1},
		{"encoding", "invalid_output_encoding", "ParsingError", "parse", 0},
		{"config", "incomplete_configuration", "DeviceError", "collect", 1},
		{"validation", "configuration_error", "ConfigurationError", "collect", 1},
		{"validation_incomplete", "incomplete_configuration", "DeviceError", "collect", 1},
		{"finish", "configuration_error", "ConfigurationError", "collect", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := connector.Profile{Name: "fixture", LoginPrompt: testPattern(`sw#$`), CommandPrompt: testPattern(`sw#$`), ConfigCommands: []string{"show config"}}
			if tc.name == "validation" {
				p.ConfigStrategy = func() connector.ConfigStrategy { return brokenConfiguration{} }
			}
			if tc.name == "validation_incomplete" {
				p.ConfigStrategy = func() connector.ConfigStrategy { return incompleteValidation{} }
			}
			if tc.name == "finish" {
				p.ConfigStrategy = func() connector.ConfigStrategy { return failedFinish{} }
			}
			cfg := connector.ConnectionConfig{Host: "fixture", LoginTimeout: 30 * time.Millisecond, CommandTimeout: 30 * time.Millisecond, WriteTimeout: 30 * time.Millisecond, MaxOutputBytes: 1024}
			if tc.name == "log" {
				cfg.Logger = diagnosticSink{}
			}
			if tc.name == "script" {
				cfg.MaxScriptOutputBytes = 1
			}
			done := make(chan struct{})
			release := make(chan struct{})
			defer close(release)
			d, err := connector.New(p, cfg, connector.WithTransportFactory(func(context.Context, connector.ConnectionConfig) (io.ReadWriteCloser, error) {
				a, b := net.Pipe()
				go func() {
					defer close(done)
					defer b.Close()
					if tc.name == "login" {
						<-release
						return
					}
					_, _ = io.WriteString(b, "sw#")
					if tc.name == "write" {
						<-release
						return
					}
					if _, err := bufio.NewReader(b).ReadString('\n'); err != nil {
						return
					}
					switch tc.name {
					case "eof":
						return
					case "command", "prompt":
						<-release
						return
					case "output":
						_, _ = io.WriteString(b, strings.Repeat("SECRET_CAUSE", 200))
					case "encoding":
						_, _ = b.Write([]byte{'\xff', '\n', 's', 'w', '#'})
					case "config":
						_, _ = io.WriteString(b, "show config\r\nsw#")
					default:
						_, _ = io.WriteString(b, "ok\r\nsw#")
					}
					_, _ = io.Copy(io.Discard, b)
				}()
				return a, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			rules, _ := NewRules(RuleOptions{}, vendors.NewBuiltinRegistry())
			plan, _ := NewPlan([]inventory.Device{{IP: "192.0.2.1", Vendor: "h3c"}}, rules, PlanOptions{Mode: "backup"})
			var result connector.Result
			batch, err := Run(t.Context(), plan, RunOptions{Workers: 1, Run: func(ctx context.Context, _ Task) (Artifact, error) {
				if tc.name == "config" || tc.name == "validation" || tc.name == "validation_incomplete" || tc.name == "finish" {
					result, err = d.RunningConfig(ctx)
					return Artifact{}, err
				}

				c := connector.Command{Text: "show"}
				if tc.name == "prompt" {
					c.Prompt = testPattern(`never#$`)
				}
				result, err = d.ExecuteCommand(ctx, c, connector.ExecuteOptions{})
				return Artifact{}, err
			}, OnOutcome: func(context.Context, Outcome) error { return errors.New("SECRET_SECONDARY") }})
			if err != nil {
				t.Fatal(err)
			}
			got := batch.Outcomes()[0].Diagnostic
			if got.ErrorCode != tc.code || got.ErrorType != "Net::Connector::"+tc.kind || got.Phase != tc.phase {
				t.Errorf("diagnostic=%+v want %s %s %s", got, tc.code, tc.kind, tc.phase)
			}
			if len(result.Steps()) != tc.steps {
				t.Errorf("steps=%d want=%d", len(result.Steps()), tc.steps)
			}
			if len(batch.CallbackErrors()) != 1 {
				t.Error("secondary callback lost")
			}
			report, _ := NewReport(batch, "selected")
			b, e := json.Marshal(report)
			if e != nil {
				t.Fatal(e)
			}
			if strings.Contains(string(b), "SECRET_") {
				t.Fatal("secret in report", string(b))
			}

			t.Logf("%s: %s/%s phase=%s steps=%d", tc.name, got.ErrorCode, got.ErrorType, got.Phase, len(result.Steps()))
		})
	}
}

func TestCallbackDeadlineRemainsGeneric(t *testing.T) {
	got := diagnoseError(fmt.Errorf("SECRET_CAUSE: %w", context.DeadlineExceeded), Artifact{})
	if got.ErrorType != "Timeout::Error" {
		t.Fatal(got)
	}
}

func TestBackupWriteFailuresRespectCommitBoundary(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission-denial boundary requires an unprivileged process")
	}
	for _, tc := range []struct {
		name                             string
		mode                             os.FileMode
		state, phase, code, kind, status string
	}{
		{"before_commit", 0500, "not_committed", "temporary_file", "file_write_failed", "Storage::AtomicFile::WriteError", "failed"},
		{"after_commit", 0300, "committed", "directory_open", "backup_persistence_unconfirmed", "BackupPersistenceError", "saved_with_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			directory := t.TempDir()
			path := filepath.Join(directory, "config")
			// The lock is acquired first. Mode 0500 forbids creating the temporary file;
			// 0300 allows create and rename, then denies the directory's read-only open.
			p := connector.Profile{Name: "fixture", LoginPrompt: testPattern(`sw#$`), CommandPrompt: testPattern(`sw#$`), ConfigCommands: []string{"show config"}, AfterCommand: func(context.Context, connector.Command, connector.StepResult, connector.Executor) error {
				return os.Chmod(directory, tc.mode)
			}}
			defer os.Chmod(directory, 0700)
			d, e := connector.New(p, connector.ConnectionConfig{Host: "fixture"}, connector.WithTransportFactory(func(context.Context, connector.ConnectionConfig) (io.ReadWriteCloser, error) {
				a, b := net.Pipe()
				go func() {
					defer b.Close()
					_, _ = io.WriteString(b, "sw#")
					if _, e := bufio.NewReader(b).ReadString('\n'); e == nil {
						_, _ = io.WriteString(b, "PRIVATE_CONFIG\r\nsw#")
					}
					_, _ = io.Copy(io.Discard, b)
				}()
				return a, nil
			}))
			if e != nil {
				t.Fatal(e)
			}
			defer d.Close()
			rules, _ := NewRules(RuleOptions{}, vendors.NewBuiltinRegistry())
			plan, _ := NewPlan([]inventory.Device{{IP: "192.0.2.1", Vendor: "h3c"}}, rules, PlanOptions{Mode: "backup"})
			batch, e := Run(t.Context(), plan, RunOptions{Run: func(ctx context.Context, _ Task) (Artifact, error) {
				receipt, err := d.Backup(ctx, connector.BackupOptions{Path: path})
				return Artifact{Backup: &receipt}, err
			}})
			if e != nil {
				t.Fatal(e)
			}
			out := batch.Outcomes()[0]
			got := out.Diagnostic
			if got.ErrorCode != tc.code || got.ErrorType != "Net::Connector::"+tc.kind || got.UnderlyingType != "Errno::EACCES" || got.ArtifactState != tc.state || got.ArtifactPhase != tc.phase || !out.Artifact.Backup.DeviceCompleted || out.Status != tc.status {
				t.Fatalf("diagnostic=%+v status=%s receipt=%+v", got, out.Status, out.Artifact.Backup)
			}
			if err := os.Chmod(directory, 0700); err != nil {
				t.Fatal(err)
			}
			// Raw source is published before the reading view. A source postcommit
			// failure must retain its artifact without manufacturing a text commit.
			contentPath := path
			if tc.state == "committed" {
				contentPath = out.Artifact.Backup.Source.Path
			}
			data, err := os.ReadFile(contentPath)
			if tc.state == "committed" {
				if err != nil || !strings.Contains(string(data), "PRIVATE_CONFIG") {
					t.Fatal("committed content missing", err)
				}
			} else if !os.IsNotExist(err) {
				t.Fatal("precommit error manufactured file", err)
			}
			report, _ := NewReport(batch, "selected")
			body, _ := json.Marshal(report)
			if strings.Contains(string(body), "PRIVATE_CONFIG") {
				t.Fatal("configuration leaked")
			}
			t.Logf("%s: %s/%s underlying=%s artifact=%s/%s status=%s", tc.name, got.ErrorCode, got.ErrorType, got.UnderlyingType, got.ArtifactState, got.ArtifactPhase, out.Status)
		})
	}
}

func TestInvalidConfigurationStrategyIsClassifiedBeforeConnection(t *testing.T) {
	for _, missing := range []bool{true, false} {
		t.Run(fmt.Sprint(missing), func(t *testing.T) {
			p := connector.Profile{Name: "fixture", LoginPrompt: testPattern(`sw#$`), CommandPrompt: testPattern(`sw#$`)}
			if !missing {
				p.ConfigCommands = []string{"show config"}
				p.ConfigStrategy = func() connector.ConfigStrategy { return nil }
			}
			d, e := connector.New(p, connector.ConnectionConfig{Host: "fixture"}, connector.WithTransportFactory(func(context.Context, connector.ConnectionConfig) (io.ReadWriteCloser, error) {
				t.Error("invalid strategy connected")
				return nil, errors.New("unexpected connection")
			}))
			if e != nil {
				t.Fatal(e)
			}
			rules, _ := NewRules(RuleOptions{}, vendors.NewBuiltinRegistry())
			plan, _ := NewPlan([]inventory.Device{{IP: "192.0.2.1", Vendor: "h3c"}}, rules, PlanOptions{Mode: "backup"})
			batch, e := Run(t.Context(), plan, RunOptions{Run: func(ctx context.Context, _ Task) (Artifact, error) {
				_, e := d.RunningConfig(ctx)
				return Artifact{}, e
			}})
			if e != nil {
				t.Fatal(e)
			}
			got := batch.Outcomes()[0].Diagnostic
			if got.ErrorCode != "configuration_error" || got.ErrorType != "Net::Connector::ConfigurationError" || got.Phase != "collect" {
				t.Fatal(got)
			}
		})
	}
}
