package workflow_test

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gatework/go-net-connector/config"
	"github.com/gatework/go-net-connector/fleet"
	"github.com/gatework/go-net-connector/internal/bootstrap"
	"github.com/gatework/go-net-connector/internal/workflow"
	"github.com/gatework/go-net-connector/inventory"
)

var updatePlanView = flag.Bool("update-plan-view", false, "rewrite plan view golden files")

func planViewRows() []inventory.Device {
	return []inventory.Device{
		{IP: "192.0.2.1", Vendor: "h3c", Name: "edge"},
		{IP: "192.0.2.2", Vendor: "huawei"},
		{IP: "192.0.2.3", Vendor: "cisco", OS: "ios", Name: "core"},
		{IP: "192.0.2.3", Vendor: "cisco", OS: "ios", Name: "duplicate"},
		{IP: "192.0.2.4", Vendor: "unknown-vendor", Name: "lab"},
		{IP: "not-an-ip", Vendor: "h3c"},
	}
}

func planViewServices(t *testing.T, mode string) (*bootstrap.Services, string) {
	t.Helper()
	root := t.TempDir()
	defaults := config.Defaults(mode)
	defaults["backup"].(map[string]any)["directory"] = filepath.Join(root, "backups")
	if mode == "tftp" {
		defaults["tftp"].(map[string]any)["server"] = "192.0.2.10"
		defaults["tftp"].(map[string]any)["verification_root"] = root
		defaults["tftp"].(map[string]any)["source_files"] = map[string]any{"huawei": "flash:/vrpcfg.zip"}
	}
	p, err := config.Load(config.Options{Name: "backup", CWD: root, Home: root, Directories: []string{}, Environment: map[string]string{}, Defaults: defaults, AllowMissing: true})
	if err != nil {
		t.Fatal(err)
	}
	s, err := bootstrap.New(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s, root
}

func assertPlanViewGolden(t *testing.T, name, root string, view any) {
	t.Helper()
	got, err := json.MarshalIndent(view, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		got = []byte(strings.ReplaceAll(string(got), resolved, "$ROOT"))
	}
	got = []byte(strings.ReplaceAll(string(got), root, "$ROOT"))
	path := filepath.Join("testdata", name)
	if *updatePlanView {
		if err := os.WriteFile(path, got, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("%s changed:\n%s\nwant:\n%s", name, got, want)
	}
}

func TestBackupPlanViewGolden(t *testing.T) {
	s, root := planViewServices(t, "backup")
	plan, err := fleet.NewPlan(planViewRows(), s.Rules(), fleet.PlanOptions{Mode: "backup"})
	if err != nil {
		t.Fatal(err)
	}
	assertPlanViewGolden(t, "plan_view_backup.json", root, workflow.NewPlanView(plan))
}

func TestTFTPPlanViewGolden(t *testing.T) {
	s, root := planViewServices(t, "tftp")
	settings, err := s.WorkflowTFTPSettings()
	if err != nil {
		t.Fatal(err)
	}
	r := &workflow.Runner{}
	result, err := r.TFTP(t.Context(), s.WorkflowRuntime(), settings, planViewRows(), workflow.TFTPOptions{BackupOptions: workflow.BackupOptions{Policy: "verified"}, Plan: true})
	if err != nil {
		t.Fatal(err)
	}
	assertPlanViewGolden(t, "plan_view_tftp.json", root, result.Preview.View)
}
