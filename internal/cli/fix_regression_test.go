package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/creack/pty"
	"github.com/gatework/go-net-connector/config"
	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/fleet"
	"github.com/gatework/go-net-connector/inventory"
	"github.com/gatework/go-net-connector/netdisco"
	"github.com/gatework/go-net-connector/topology"
)

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("closed output") }

type cancelingWriter struct{ cancel context.CancelFunc }

func (w cancelingWriter) Write([]byte) (int, error) {
	w.cancel()
	return 0, errors.New("closed output")
}

func tftpReportFixture(t *testing.T, output io.Writer, progress io.Writer, ctx context.Context) (int, string) {
	t.Helper()
	r, _, _ := fixtureRuntime(t)
	r.Args = []string{"--tftp-server", "192.0.2.9", "--username", "user", "--password", "secret", "--json"}
	r.Output, r.Error = output, progress
	r.Source = netdisco.SourceFunc(func(context.Context) ([]inventory.Device, error) {
		return []inventory.Device{{IP: "192.0.2.1", Name: "edge", Vendor: "cisco", OS: "nxos"}}, nil
	})
	r.DeviceFactory = func(_ connector.Profile, c connector.ConnectionConfig) (*connector.Device, error) {
		return connector.New(connector.Profile{Name: "cisco_nxos", LoginPrompt: regexp.MustCompile(`sw#\s*$`), CommandPrompt: regexp.MustCompile(`sw#\s*$`)}, c, connector.WithTransportFactory(func(context.Context, connector.ConnectionConfig) (io.ReadWriteCloser, error) {
			a, b := net.Pipe()
			go func() {
				defer b.Close()
				_, _ = io.WriteString(b, "sw#")
				line, err := bufio.NewReader(b).ReadString('\n')
				if err != nil {
					return
				}
				_, _ = io.WriteString(b, line+"123 bytes copied\r\nsw#")
				_, _ = io.Copy(io.Discard, b)
			}()
			return a, nil
		}))
	}
	code := r.Run(ctx, "net-backup-tftp")
	return code, filepath.Join(r.CWD, "backups")
}

func latestReport(t *testing.T, root string) map[string]any {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 {
		t.Fatalf("batch directory: %v, %d entries", err, len(entries))
	}
	data, err := os.ReadFile(filepath.Join(root, entries[0].Name(), "summary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report map[string]any
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	return report
}

func TestProgressFailureKeepsCompletedTransferAndEvent(t *testing.T) {
	var output bytes.Buffer
	code, root := tftpReportFixture(t, &output, failingWriter{}, t.Context())
	if code != 1 {
		t.Fatal(code, output.String())
	}
	report := latestReport(t, root)
	if report["policy_success"] != false || len(report["callback_errors"].([]any)) == 0 {
		t.Fatal(report)
	}
	row := report["devices"].([]any)[0].(map[string]any)
	if row["status"] != "reported_uploaded" {
		t.Fatal(row)
	}
	events, err := filepath.Glob(filepath.Join(root, "*", "events.jsonl"))
	if err != nil || len(events) != 1 {
		t.Fatal(err, events)
	}
	data, err := os.ReadFile(events[0])
	if err != nil || !bytes.Contains(data, []byte("reported_uploaded")) {
		t.Fatal(err, string(data))
	}
}

func TestCanceledReportOutputRetains130AndDiagnostic(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	code, root := tftpReportFixture(t, cancelingWriter{cancel}, io.Discard, ctx)
	if code != 130 {
		t.Fatal(code)
	}
	report := latestReport(t, root)
	if report["policy_success"] != false || report["report_error"] == nil {
		t.Fatal(report)
	}
}

func TestTopologyTFTPRejectedBeforeInputOrInventory(t *testing.T) {
	r, _, _ := fixtureRuntime(t)
	r.Args = []string{"--tftp", "--stdin-credentials"}
	r.Environment = map[string]string{"NC_CONFIG": "/does/not/exist"}
	r.Source = netdisco.SourceFunc(func(context.Context) ([]inventory.Device, error) { t.Fatal("inventory accessed"); return nil, nil })
	if code := r.Run(t.Context(), "net-topology"); code != 2 {
		t.Fatal(code)
	}
}

func TestTopologyPolicyPreflightBeforeCredentialsAndInventory(t *testing.T) {
	for _, env := range []map[string]string{{"NC_TOPOLOGY_MODE": "apply"}, {"NC_TOPOLOGY_DESCRIPTION_FORMAT": "%{unknown}"}} {
		r, _, _ := fixtureRuntime(t)
		r.Args = []string{"--stdin-credentials"}
		r.Environment = env
		r.Source = netdisco.SourceFunc(func(context.Context) ([]inventory.Device, error) { t.Fatal("inventory accessed"); return nil, nil })
		if code := r.Run(t.Context(), "net-topology"); code != 2 {
			t.Fatal(env, code)
		}
	}
}

func TestInteractiveSetupConsentAndCustomization(t *testing.T) {
	for _, tc := range []struct {
		name, args, answers string
		wantCode            int
		config, env         bool
	}{
		{"refuse_required", "--interactive", "n\n", 1, false, false},
		{"approve_config_refuse_env_customization", "--interactive", "y\nn\nn\n", 0, true, false},
		{"init_decline_customization", "--init --interactive", "n\n", 0, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			master, slave, err := pty.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer master.Close()
			defer slave.Close()
			if _, err := master.Write([]byte(tc.answers)); err != nil {
				t.Fatal(err)
			}
			r := Runtime{Args: strings.Fields(tc.args), CWD: root, Home: root, Directories: []string{}, Environment: map[string]string{}, Input: slave, Error: slave, Output: &bytes.Buffer{}}
			if code := r.Run(t.Context(), "net-backup"); code != tc.wantCode {
				t.Fatal(code)
			}
			configPath := filepath.Join(root, ".config", "net-connector", "backup.yml")
			envPath := filepath.Join(root, ".config", "net-connector", ".env")
			for _, item := range []struct {
				path string
				want bool
			}{{configPath, tc.config}, {envPath, tc.env}} {
				_, err := os.Lstat(item.path)
				if (err == nil) != item.want {
					t.Fatalf("%s: %v", item.path, err)
				}
			}
		})
	}
}

func TestInteractiveExistingFilesContinuesWithoutTerminal(t *testing.T) {
	r, _, _ := fixtureRuntime(t)
	r.Args = []string{"--init", "--non-interactive"}
	if code := r.Run(t.Context(), "net-backup"); code != 0 {
		t.Fatal(code)
	}
	var output, diagnostics bytes.Buffer
	r.Args = []string{"--interactive", "--inventory-only"}
	r.Input, r.Output, r.Error = panicReader{}, &output, &diagnostics
	reads := 0
	r.Source = netdisco.SourceFunc(func(context.Context) ([]inventory.Device, error) {
		reads++
		return []inventory.Device{{IP: "192.0.2.1", Vendor: "cisco", OS: "ios"}}, nil
	})
	if code := r.Run(t.Context(), "net-backup"); code != 0 || reads != 1 {
		t.Fatalf("code=%d reads=%d diagnostics=%s", code, reads, diagnostics.String())
	}
}

func TestInitInteractiveExistingFilesNeedsNoTerminal(t *testing.T) {
	r, _, _ := fixtureRuntime(t)
	r.Args = []string{"--init", "--non-interactive"}
	if code := r.Run(t.Context(), "net-backup"); code != 0 {
		t.Fatal(code)
	}
	configPath := filepath.Join(r.Home, ".config", "net-connector", "backup.yml")
	envPath := filepath.Join(r.Home, ".config", "net-connector", ".env")
	before := map[string][]byte{}
	for _, path := range []string{configPath, envPath} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		before[path] = data
	}
	r.Args = []string{"--init", "--interactive"}
	r.Input = panicReader{}
	r.Source = netdisco.SourceFunc(func(context.Context) ([]inventory.Device, error) { t.Fatal("inventory accessed"); return nil, nil })
	if code := r.Run(t.Context(), "net-backup"); code != 0 {
		t.Fatal(code)
	}
	for path, old := range before {
		data, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(data, old) {
			t.Fatal(path, err)
		}
	}
}

func TestInteractiveDeclinedOptionalEnvContinues(t *testing.T) {
	r, _, _ := fixtureRuntime(t)
	r.Args = []string{"--init", "--non-interactive"}
	if code := r.Run(t.Context(), "net-backup"); code != 0 {
		t.Fatal(code)
	}
	envPath := filepath.Join(r.Home, ".config", "net-connector", ".env")
	if err := os.Remove(envPath); err != nil {
		t.Fatal(err)
	}
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()
	if _, err := master.Write([]byte("n\n")); err != nil {
		t.Fatal(err)
	}
	r.Args, r.Input, r.Error = []string{"--interactive", "--inventory-only"}, slave, slave
	reads := 0
	r.Source = netdisco.SourceFunc(func(context.Context) ([]inventory.Device, error) {
		reads++
		return []inventory.Device{{IP: "192.0.2.1", Vendor: "cisco", OS: "ios"}}, nil
	})
	if code := r.Run(t.Context(), "net-backup"); code != 0 || reads != 1 {
		t.Fatalf("code=%d reads=%d", code, reads)
	}
	if _, err := os.Lstat(envPath); !os.IsNotExist(err) {
		t.Fatal("optional .env created", err)
	}
}

func TestTopologyTextStdoutExplainsPendingPlan(t *testing.T) {
	var output bytes.Buffer
	r := Runtime{Output: &output}
	dir := t.TempDir()
	data := map[string]any{"mode": "plan", "success": false, "directory": dir,
		"statistics": map[string]int{"collected": 1, "change_devices": 1, "changes": 1, "review_devices": 1, "review_interfaces": 1},
		"devices":    []map[string]any{{"script_file": filepath.Join(dir, "scripts", "edge.cfg"), "issues": []topology.Issue{{Interface: "Gi1/0/2", Code: "unknown_neighbor_name"}}}},
	}
	if err := r.displayTopology(data, options{}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Result: plan generated; corrections or verification still required; mode: plan", "Unverified reasons: neighbor name missing 1 interfaces", "Command file: " + filepath.Join(dir, "scripts") + "(1 devices with changes)"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("missing %q in %s", want, output.String())
		}
	}
}

func TestTopologyTextAndJSONRepair(t *testing.T) {
	dir := t.TempDir()
	current, expected := "old", "To_peer_Gi1/0/1"
	out := topologyOutcome{device: fleet.Record{Host: "192.0.2.1", Name: "edge", Vendor: "cisco_ios"}, row: map[string]any{
		"status": "applied", "changes": 1, "steps": 3, "script_file": filepath.Join(dir, "scripts", "edge.cfg"), "script_sha256": strings.Repeat("a", 64),
		"neighbors": []topology.Comparison{{Interface: "Gi1/0/1", Current: &current, Expected: &expected, Status: "content_mismatch"}},
		"issues":    []topology.Issue{{Interface: "Gi1/0/2", Code: "unknown_neighbor_name"}},
	}}
	out.row["status"] = "planned"
	out.row["steps"] = 0
	if err := writeTopologyDeviceReport(t.Context(), dir, out); err != nil {
		t.Fatal(err)
	}
	name, _ := out.device.BackupFilename("hostname_ip")
	initial, err := os.ReadFile(filepath.Join(dir, "devices", name))
	if err != nil || !bytes.Contains(initial, []byte("Status: planned")) {
		t.Fatal(err, string(initial))
	}
	out.row["status"] = "applied"
	out.row["steps"] = 3
	if err := writeTopologyDeviceReport(t.Context(), dir, out); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "devices", name))
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"Status: applied", "Command SHA-256:", "Gi1/0/1 content_mismatch", "Unverified: Gi1/0/2 neighbor name missing"} {
		if !bytes.Contains(data, []byte(value)) {
			t.Fatal(value, string(data))
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "summary.txt"), 0700); err != nil {
		t.Fatal(err)
	}
	summary := map[string]any{"mode": "apply", "success": true, "devices": []map[string]any{out.row}, "statistics": map[string]int{}}
	if err := persistTopologySummary(t.Context(), dir, summary); err == nil {
		t.Fatal("expected text failure")
	}
	data, err = os.ReadFile(filepath.Join(dir, "summary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]any
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved["success"] != false || saved["report_error"] != "IOError" {
		t.Fatal(saved)
	}
}

func TestHelpExamplesAreDistinctAndOffline(t *testing.T) {
	views := map[string]string{}
	for _, flag := range []string{"--help", "--help-all", "--examples"} {
		r, out, _ := fixtureRuntime(t)
		r.Args, r.Input = []string{flag}, panicReader{}
		if code := r.Run(t.Context(), "net-topology"); code != 0 {
			t.Fatal(flag, code)
		}
		views[flag] = out.String()
	}
	if views["--help"] == views["--help-all"] || views["--examples"] == views["--help-all"] || !strings.Contains(views["--examples"], "net-topology --apply") {
		t.Fatal(views)
	}
}

func TestTopologyCancelWinsOverBrokenOutput(t *testing.T) {
	r, _, _ := fixtureRuntime(t)
	r.Args = []string{"--json", "--inventory-only"}
	r.Output = failingWriter{}
	r.Source = netdisco.SourceFunc(func(context.Context) ([]inventory.Device, error) {
		return []inventory.Device{{IP: "192.0.2.1", Vendor: "cisco", OS: "ios"}}, nil
	})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if code := r.Run(ctx, "net-topology"); code != 130 {
		t.Fatal(code)
	}
}

func TestTopologyCacheModeSelection(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct {
		args []string
		want string
	}{
		{nil, "only"},
		{[]string{"--cache-mode", "refresh"}, "refresh"},
		{[]string{"--cache-only"}, "only"},
		{[]string{"--refresh-inventory"}, "refresh"},
	} {
		o, err := parse(tc.args, "net-topology", io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		overrides, err := config.FromEnvironment(o.public)
		if err != nil {
			t.Fatal(err)
		}
		p, err := config.Load(config.Options{Name: "topology", CWD: root, Home: root, Directories: []string{}, Environment: map[string]string{"NC_TOPOLOGY_CACHE_MODE": "only"}, Defaults: config.Defaults("topology"), Overrides: overrides, AllowMissing: true})
		if err != nil {
			t.Fatal(err)
		}
		if got := inventoryMode(p, o); got != tc.want {
			t.Fatalf("%v: got %s, want %s", tc.args, got, tc.want)
		}
	}
}
