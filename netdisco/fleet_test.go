package netdisco

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/inventory"
	"github.com/gatework/go-net-connector/storage"
	"github.com/gatework/go-net-connector/transfer"
	"github.com/gatework/go-net-connector/vendors"
)

func TestFleetRetainsArtifactAndIsolatesCallback(t *testing.T) {
	rules, _ := NewRules(RuleOptions{}, vendors.Profile)
	p, _ := NewPlan([]inventory.Device{{IP: "192.0.2.1", Vendor: "h3c"}, {IP: "192.0.2.2", Vendor: "h3c"}}, rules, PlanOptions{Mode: "backup"})
	b, e := Run(context.Background(), p, FleetOptions{Workers: 1, Run: func(_ context.Context, task Task) (Artifact, error) {
		a := Artifact{Backup: &connector.BackupReceipt{Path: task.Device.Host, File: storage.Receipt{State: "committed"}}}
		if task.Index == 0 {
			return a, errors.New("secret body")
		}
		return a, nil
	}, OnOutcome: func(context.Context, Outcome) error { return errors.New("secret callback") }})
	if e != nil || len(b.Outcomes()) != 2 || b.Outcomes()[0].Status != "saved_with_error" || b.Outcomes()[1].Status != "backed_up" || len(b.CallbackErrors()) != 2 {
		t.Fatal(b, e)
	}
	report, e := NewReport(b, "selected")
	if e != nil || report.Success() {
		t.Fatal(report, e)
	}
}
func TestFleetCancelStopsDispatch(t *testing.T) {
	rules, _ := NewRules(RuleOptions{}, vendors.Profile)
	p, _ := NewPlan([]inventory.Device{{IP: "192.0.2.1", Vendor: "h3c"}, {IP: "192.0.2.2", Vendor: "h3c"}}, rules, PlanOptions{Mode: "backup"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b, e := Run(ctx, p, FleetOptions{Workers: 1, Run: func(context.Context, Task) (Artifact, error) { cancel(); return Artifact{}, context.Canceled }})
	if !errors.Is(e, context.Canceled) || b.Outcomes()[0].Status != "interrupted" || b.Outcomes()[1].Status != "not_started" {
		t.Fatal(b, e)
	}
}
func TestReportSelectedIgnoresOnlyKnownSkips(t *testing.T) {
	for _, test := range []struct {
		status string
		want   bool
	}{{"filtered", true}, {"sample_limit", true}, {"mystery", false}, {"unsupported_vendor", false}} {
		b := Batch{mode: "backup", outcomes: []Outcome{{Status: "backed_up"}, {Status: test.status}}}
		r, e := NewReport(b, "selected")
		if e != nil || r.Success() != test.want {
			t.Fatal(test, r, e)
		}
	}
}

func TestMissingCredentialsIsExplicitSkip(t *testing.T) {
	rules, _ := NewRules(RuleOptions{}, vendors.Profile)
	plan, _ := NewPlan([]inventory.Device{{IP: "192.0.2.1", Vendor: "h3c"}}, rules, PlanOptions{Mode: "backup"})
	batch, e := Run(context.Background(), plan, FleetOptions{Run: func(context.Context, Task) (Artifact, error) { return Artifact{}, ErrMissingCredentials }})
	if e != nil || batch.Outcomes()[0].Status != "missing_credentials" || batch.Outcomes()[0].ErrorType != "" {
		t.Fatal(batch, e)
	}
}

func TestFleetBackupCompletionRequiresExpectedMode(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		artifact   Artifact
		runErr     error
		want       string
	}{
		{"verified unchanged", "backup", Artifact{Backup: &connector.BackupReceipt{DeviceCompleted: true, Change: "unchanged", NoWriteVerified: true, File: storage.Receipt{State: "not_committed", Phase: "compare"}}}, nil, "backed_up"},
		{"verified unchanged then close failed", "backup", Artifact{Backup: &connector.BackupReceipt{DeviceCompleted: true, Change: "unchanged", NoWriteVerified: true, File: storage.Receipt{State: "not_committed", Phase: "compare"}}}, errors.New("private close failure"), "saved_with_error"},
		{"unchanged without verification", "backup", Artifact{Backup: &connector.BackupReceipt{DeviceCompleted: true, Change: "unchanged", File: storage.Receipt{State: "not_committed", Phase: "compare"}}}, nil, "failed"},
		{"unchanged replacement failed before commit", "backup", Artifact{Backup: &connector.BackupReceipt{DeviceCompleted: true, Change: "unchanged", File: storage.Receipt{State: "not_committed", Phase: "write"}}}, errors.New("private write failure"), "failed"},
		{"collection only", "backup", Artifact{Backup: &connector.BackupReceipt{DeviceCompleted: true, File: storage.Receipt{State: "not_committed"}}}, nil, "failed"},
		{"unchanged without collection", "backup", Artifact{Backup: &connector.BackupReceipt{Change: "unchanged", File: storage.Receipt{State: "not_committed"}}}, nil, "failed"},
		{"transfer in backup", "backup", Artifact{Transfer: &transfer.Receipt{Verification: "device_reported"}}, nil, "failed"},
		{"transfer partial in backup", "backup", Artifact{Transfer: &transfer.Receipt{Verification: "device_reported"}}, errors.New("private"), "failed"},
		{"backup in transfer", "tftp", Artifact{Backup: &connector.BackupReceipt{File: storage.Receipt{State: "durable"}}}, nil, "failed"},
		{"backup partial in transfer", "tftp", Artifact{Backup: &connector.BackupReceipt{File: storage.Receipt{State: "durable"}}}, errors.New("private"), "failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rules, err := NewRules(RuleOptions{}, vendors.Profile)
			if err != nil {
				t.Fatal(err)
			}
			p, err := NewPlan([]inventory.Device{{IP: "192.0.2.1", Vendor: "h3c"}}, rules, PlanOptions{Mode: tc.mode, Target: func(Record) (Target, error) { return Target{Server: "192.0.2.9", Path: "config.txt"}, nil }})
			if err != nil {
				t.Fatal(err)
			}
			b, err := Run(t.Context(), p, FleetOptions{Run: func(context.Context, Task) (Artifact, error) { return tc.artifact, tc.runErr }})
			if err != nil {
				t.Fatal(err)
			}
			got := b.Outcomes()[0]
			if got.Status != tc.want || b.Success() != (tc.want == "backed_up") {
				t.Fatalf("status=%s success=%t", got.Status, b.Success())
			}
			if (got.Artifact.Backup != nil) != (tc.artifact.Backup != nil) || (got.Artifact.Transfer != nil) != (tc.artifact.Transfer != nil) {
				t.Fatal("returned evidence lost")
			}
		})
	}
}
func TestFleetSecondConnectorBackupSucceedsWithoutRewrite(t *testing.T) {
	profile := connector.Profile{Name: "fixture", LoginPrompt: regexp.MustCompile(`sw#\s*$`), CommandPrompt: regexp.MustCompile(`sw#\s*$`), ConfigCommands: []string{"show config"}}
	done := make(chan struct{})
	d, err := connector.New(profile, connector.ConnectionConfig{Host: "192.0.2.1", CommandTimeout: time.Second, LoginTimeout: time.Second}, connector.WithTransportFactory(func(context.Context, connector.ConnectionConfig) (io.ReadWriteCloser, error) {
		a, b := net.Pipe()
		go func() {
			defer close(done)
			defer b.Close()
			if _, err := io.WriteString(b, "sw#"); err != nil {
				return
			}
			scan := bufio.NewScanner(b)
			for scan.Scan() {
				if _, err := io.WriteString(b, scan.Text()+"\r\nhostname test\r\nsw#"); err != nil {
					return
				}
			}
		}()
		return a, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("fixture did not stop")
		}
	}()
	path := filepath.Join(t.TempDir(), "device.txt")
	first, err := d.Backup(t.Context(), connector.BackupOptions{Path: path})
	if err != nil || !first.File.Durable() {
		t.Fatalf("first backup: %#v %v", first, err)
	}
	when := time.Unix(100, 0)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
	rules, err := NewRules(RuleOptions{HostOverrides: map[string]string{"192.0.2.1": "fixture"}}, func(string) (connector.Profile, error) { return profile, nil })
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewPlan([]inventory.Device{{IP: "192.0.2.1"}}, rules, PlanOptions{Mode: "backup"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := Run(t.Context(), p, FleetOptions{Run: func(ctx context.Context, _ Task) (Artifact, error) {
		r, e := d.Backup(ctx, connector.BackupOptions{Path: path})
		return Artifact{Backup: &r}, e
	}})
	if err != nil {
		t.Fatal(err)
	}
	out := b.Outcomes()[0]
	if out.Status != "backed_up" || !b.Success() || out.Artifact.Backup.Change != "unchanged" || !out.Artifact.Backup.NoWriteVerified || out.Artifact.Backup.File.Committed() {
		t.Fatalf("second backup: %#v", out)
	}
	stat, err := os.Stat(path)
	if err != nil || !stat.ModTime().Equal(when) {
		t.Fatalf("backup rewritten: %v %v", stat, err)
	}
	for _, policy := range []string{"strict", "selected"} {
		r, e := NewReport(b, policy)
		if e != nil || !r.Success() {
			t.Fatalf("%s report failed: %v", policy, e)
		}
	}
}

func TestFleetClonesTransferRequestedPath(t *testing.T) {
	requested := "config.txt"
	rules, err := NewRules(RuleOptions{}, vendors.Profile)
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewPlan([]inventory.Device{{IP: "192.0.2.1", Vendor: "h3c"}}, rules, PlanOptions{Mode: "tftp", Target: func(Record) (Target, error) { return Target{Server: "192.0.2.9", Path: requested}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	b, err := Run(t.Context(), p, FleetOptions{Run: func(context.Context, Task) (Artifact, error) {
		return Artifact{Transfer: &transfer.Receipt{RequestedPath: &requested, Verification: "device_reported"}}, nil
	}, OnOutcome: func(_ context.Context, o Outcome) error {
		*o.Artifact.Transfer.RequestedPath = "callback mutation"
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if requested != "config.txt" || *b.Outcomes()[0].Artifact.Transfer.RequestedPath != "config.txt" {
		t.Fatal("callback mutated adapter or batch transfer receipt")
	}
	outcomes := b.Outcomes()
	*outcomes[0].Artifact.Transfer.RequestedPath = "accessor mutation"
	if *b.Outcomes()[0].Artifact.Transfer.RequestedPath != "config.txt" {
		t.Fatal("accessor mutated batch transfer receipt")
	}
}
