package workflow

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/fleet"
	"github.com/gatework/go-net-connector/internal/topologyreport"
	"github.com/gatework/go-net-connector/topology"
)

func TestTopologyTextAndJSONRepair(t *testing.T) {
	dir := t.TempDir()
	current, expected := "old", "To_peer_Gi1/0/1"
	out := topologyOutcome{device: fleet.Record{Host: "192.0.2.1", Name: "edge", Vendor: "cisco_ios"},
		state: topologyPlanned, audited: true, scriptFile: filepath.Join(dir, "scripts", "edge.cfg"), scriptSHA256: strings.Repeat("a", 64),
		audit: topology.Audit{Comparisons: []topology.Comparison{{Interface: "Gi1/0/1", Current: &current, Expected: &expected, Status: "content_mismatch"}},
			Issues: []topology.Issue{{Interface: "Gi1/0/2", Code: "unknown_neighbor_name"}}},
	}
	if err := writeTopologyDeviceReport(t.Context(), dir, out); err != nil {
		t.Fatal(err)
	}
	name, _ := out.device.BackupFilename("hostname_ip")
	initial, err := os.ReadFile(filepath.Join(dir, "devices", name))
	if err != nil || !bytes.Contains(initial, []byte("状态：计划已生成")) {
		t.Fatal(err, string(initial))
	}
	out.state, out.applied = topologyApplied, true
	out.result.Receipts = make([]connector.StepResult, 3)
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
	summary := &TopologyReport{Mode: "apply", Success: true, outcomes: []topologyOutcome{out}, counts: map[topologyState]int{topologyApplied: 1}}
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

func TestTopologyTextExplainsPendingPlan(t *testing.T) {
	dir := t.TempDir()
	report := &TopologyReport{Mode: "plan", Directory: dir, outcomes: []topologyOutcome{{
		state: topologyPartialPlanned, audited: true, scriptFile: filepath.Join(dir, "scripts", "edge.cfg"),
		audit: topology.Audit{Comparisons: []topology.Comparison{{Status: "content_mismatch"}}, Issues: []topology.Issue{{Interface: "Gi1/0/2", Code: "unknown_neighbor_name"}}},
	}}}
	report.summarize(false)
	text := topologyreport.SummaryText(report.Presentation())
	for _, want := range []string{"结果：计划已生成，仍需修正或核验；模式：规划", "未核验原因：缺少邻居名称 1 个接口", "命令文件：" + filepath.Join(dir, "scripts", "edge.cfg")} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in %s", want, text)
		}
	}
	if report.ScriptCount() != 1 {
		t.Fatal("script evidence lost")
	}
}

func TestTopologyProjectionCannotChangeExecutionOrEvidence(t *testing.T) {
	current := "current"
	out := topologyOutcome{device: fleet.Record{Host: "192.0.2.1"}, state: topologyPartialPlanned, audited: true,
		audit: topology.Audit{Comparisons: []topology.Comparison{{Current: &current, Status: "content_mismatch"}}, Issues: []topology.Issue{{Code: "unknown_neighbor_name"}}},
		plan:  &topology.Plan{Commands: []string{"description peer"}},
	}
	report := &TopologyReport{Mode: "plan", outcomes: []topologyOutcome{out}}
	report.summarize(false)
	before := topologyreport.SummaryText(report.Presentation())
	view := report.view()
	row := view["devices"].([]map[string]any)[0]
	row["status"] = "applied"
	row["commands"].([]string)[0] = "unrelated write"
	row["issues"].([]topology.Issue)[0].Code = "changed"
	*row["neighbors"].([]topology.Comparison)[0].Current = "changed"
	view["success"] = true
	view["statistics"].(map[string]int)["changes"] = 0
	presentation := report.Presentation()
	presentation.Devices[0].Commands[0] = "unrelated write"
	presentation.Devices[0].Issues[0].Code = "changed"
	*presentation.Devices[0].Comparisons[0].Current = "changed"
	*presentation.Devices[0].Changes = 99
	presentation.Statistics.Changes = 99
	public := makeTopologyOutcomes(report.outcomes)
	public[0].Plan.Commands[0] = "unrelated write"
	public[0].Audit.Issues[0].Code = "changed"
	*public[0].Audit.Comparisons[0].Current = "changed"
	if !report.outcomes[0].state.permitsApply() || report.Success || topologyreport.SummaryText(report.Presentation()) != before {
		t.Fatal("presentation map changed execution state or retained evidence")
	}
}
