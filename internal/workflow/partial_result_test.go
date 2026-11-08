package workflow_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gatework/go-net-connector/config"
	"github.com/gatework/go-net-connector/internal/bootstrap"
	"github.com/gatework/go-net-connector/internal/workflow"
	"github.com/gatework/go-net-connector/inventory"
)

type sabotagePlanWriter struct{ root string }

func (w sabotagePlanWriter) Write(b []byte) (int, error) {
	dirs, e := os.ReadDir(w.root)
	if e != nil {
		return 0, e
	}
	for _, d := range dirs {
		if d.IsDir() {
			if e = os.Mkdir(filepath.Join(w.root, d.Name(), "plan.json"), 0700); e != nil && !os.IsExist(e) {
				return 0, e
			}
		}
	}
	return len(b), nil
}
func TestPlanSaveFailurePreservesCollectedOutcomes(t *testing.T) {
	root := t.TempDir()
	defaults := config.Defaults("topology")
	defaults["topology"].(map[string]any)["directory"] = root
	p, e := config.Load(config.Options{Name: "topology", CWD: t.TempDir(), Home: t.TempDir(), Directories: []string{}, Environment: map[string]string{}, Defaults: defaults, AllowMissing: true})
	if e != nil {
		t.Fatal(e)
	}
	s, e := bootstrap.New(p, nil)
	if e != nil {
		t.Fatal(e)
	}
	r := &workflow.Runner{Observe: func(workflow.Event) error { _, err := (sabotagePlanWriter{root}).Write(nil); return err }}
	settings, e := s.WorkflowTopologySettings("plan")
	if e != nil {
		t.Fatal(e)
	}
	result, e := r.Topology(context.Background(), s.WorkflowRuntime(), settings, []inventory.Device{{IP: "192.0.2.7", Name: "edge", Vendor: "topsec"}}, workflow.TopologyOptions{})
	if e == nil {
		t.Fatal("plan persistence should fail")
	}
	if result.Topology == nil {
		t.Fatal("collected result lost on plan persistence failure")
	}
	rows, ok := result.Topology.Summary()["devices"].([]map[string]any)
	if !ok || len(rows) != 1 || rows[0]["host"] != "192.0.2.7" {
		t.Fatalf("collected identity lost: %#v", result.Topology.Summary())
	}
	if result.Topology.Summary()["report_error"] == nil {
		t.Fatal("missing persistence diagnostic")
	}
}
