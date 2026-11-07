package workflow

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gatework/go-net-connector/fleet"
	"github.com/gatework/go-net-connector/topology"
)

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
	if err != nil || !bytes.Contains(initial, []byte("状态：计划已生成")) {
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
	for _, value := range []string{"状态：执行完成", "命令 SHA-256：", "Gi1/0/1 内容不一致", "未核验：Gi1/0/2 缺少邻居名称"} {
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
