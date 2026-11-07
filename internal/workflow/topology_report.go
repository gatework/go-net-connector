package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gatework/go-net-connector/internal/display"
	"github.com/gatework/go-net-connector/storage"
	"github.com/gatework/go-net-connector/topology"
	"github.com/gatework/go-net-connector/vendors/dialect"
)

func saveTopologyPlan(ctx context.Context, directory string, outcomes []topologyOutcome, registry dialect.Registry) error {
	plans := []map[string]any{}
	audits := []map[string]any{}
	for _, out := range outcomes {
		name, e := out.device.BackupFilename("hostname_ip")
		if e != nil {
			return e
		}
		stem := strings.TrimSuffix(name, ".txt")
		out.row["name"] = nullableString(out.device.Name)
		reportPath := filepath.Join(directory, "devices", stem+".txt")
		if out.row["status"] == "not_started" {
			delete(out.row, "device_report")
		} else {
			out.row["device_report"] = reportPath
		}
		if out.plan != nil {
			p := out.plan
			if e = topology.ValidatePlan(registry, *p, out.device.Host, out.device.Vendor); e != nil {
				return e
			}
			var scriptPath, digest any
			if len(p.Commands) > 0 {
				path := filepath.Join(directory, "scripts", stem+".cfg")
				body := strings.Join(p.Commands, "\n") + "\n"
				if e = storage.PrepareDirectory(filepath.Dir(path)); e != nil {
					return e
				}
				if _, e = storage.Write(ctx, path, []byte(body)); e != nil {
					return e
				}
				scriptPath = path
				digest = fmt.Sprintf("%x", sha256.Sum256([]byte(body)))
				out.row["script_file"], out.row["script_sha256"] = scriptPath, digest
			}
			plans = append(plans, map[string]any{"host": p.Host, "name": nullableString(out.device.Name), "vendor": p.Vendor, "evidence": p.Evidence, "scope": p.Scope, "commands": p.Commands, "script_file": scriptPath, "script_sha256": digest, "device_report": reportPath, "changes": p.Changes})
		}
		if out.row["status"] != "not_started" {
			if e = storage.PrepareDirectory(filepath.Dir(reportPath)); e != nil {
				return e
			}
			if e = writeTopologyDeviceReport(ctx, directory, out); e != nil {
				return e
			}
		}
		audits = append(audits, out.row)
	}
	b, e := json.MarshalIndent(map[string]any{"devices": plans, "audits": audits}, "", "  ")
	if e != nil {
		return e
	}
	path := filepath.Join(directory, "plan.json")
	if _, e = storage.Write(ctx, path, b); e != nil {
		return e
	}
	_, e = storage.Write(ctx, path+".sha256", []byte(fmt.Sprintf("%x\n", sha256.Sum256(b))))
	return e
}
func saveTopologySummary(ctx context.Context, dir string, summary map[string]any) error {
	if e := saveTopologyJSON(ctx, dir, summary); e != nil {
		return e
	}
	_, e := storage.Write(ctx, filepath.Join(dir, "summary.txt"), []byte(topologySummaryText(summary)))
	return e
}
func persistTopologySummary(ctx context.Context, dir string, summary map[string]any) error {
	err := saveTopologySummary(ctx, dir, summary)
	if err != nil {
		summary["success"] = false
		if summary["report_error"] == nil {
			summary["report_error"] = "IOError"
		}
		// JSON may already be committed when the text report fails.
		_ = saveTopologyJSON(ctx, dir, summary)
	}
	return err
}
func saveTopologyJSON(ctx context.Context, dir string, summary map[string]any) error {
	summary["report_location"] = filepath.Join(dir, "summary.json")
	b, e := json.MarshalIndent(summary, "", "  ")
	if e != nil {
		return e
	}
	_, e = storage.Write(ctx, filepath.Join(dir, "summary.json"), b)
	return e
}
func writeTopologyDeviceReport(ctx context.Context, directory string, out topologyOutcome) error {
	name, e := out.device.BackupFilename("hostname_ip")
	if e != nil {
		return e
	}
	destination := filepath.Join(directory, "devices", name)
	if e = storage.PrepareDirectory(filepath.Dir(destination)); e != nil {
		return e
	}
	var body strings.Builder
	fmt.Fprintf(&body, "设备：%s（%s）\n厂商：%s\n状态：%v；候选变更：%v\n已完成步骤：%v\n", safeTopologyText(out.device.Name), out.device.Host, out.device.Vendor, display.Status(fmt.Sprint(out.row["status"])), out.row["changes"], out.row["steps"])
	if script, ok := out.row["script_file"].(string); ok {
		fmt.Fprintf(&body, "命令文件：%s\n命令 SHA-256：%v\n", script, out.row["script_sha256"])
	} else {
		body.WriteString("命令文件：未生成（无已核验的配置变更）\n")
	}
	if diagnostic, ok := out.row["diagnostic"].(map[string]any); ok {
		fmt.Fprintf(&body, "诊断：%v\n", display.Error(fmt.Sprint(diagnostic["error_code"])))
	}
	body.WriteString("配置命令来自本计划；实际执行结果请核对设备状态与回执。\n")
	if rows, ok := out.row["neighbors"].([]topology.Comparison); ok {
		for _, row := range rows {
			fmt.Fprintf(&body, "%s %s 当前=%s 目标=%s\n", safeTopologyText(row.Interface), display.Status(row.Status), topologyQuoted(row.Current), topologyQuoted(row.Expected))
		}
	}
	if issues, ok := out.row["issues"].([]topology.Issue); ok {
		for _, issue := range issues {
			fmt.Fprintf(&body, "未核验：%s %s\n", safeTopologyText(issue.Interface), topologyReason(issue.Code))
		}
	}
	_, e = storage.Write(ctx, destination, []byte(body.String()))
	return e
}

func safeTopologyText(value string) string {
	return strings.Map(func(c rune) rune {
		if c < ' ' || c == 127 {
			return ' '
		}
		return c
	}, value)
}
func topologyQuoted(value *string) string {
	if value == nil {
		return "未设置"
	}
	return fmt.Sprintf("%q", *value)
}
func topologyReason(code string) string {
	switch code {
	case "unknown_neighbor_name":
		return "缺少邻居名称"
	case "unknown_neighbor_interface":
		return "缺少邻居端口"
	case "ambiguous_neighbor":
		return "同一接口存在多个邻居"
	case "interface_missing":
		return "配置中未找到本地接口"
	case "unsafe_description":
		return "目标描述不满足安全约束"
	default:
		return safeTopologyText(code)
	}
}

func topologySummaryText(summary map[string]any) string {
	var b strings.Builder
	b.WriteString(TopologyResultLines(summary))
	if rows, ok := summary["devices"].([]map[string]any); ok {
		for _, row := range rows {
			name, _ := row["name"].(string)
			script, _ := row["script_file"].(string)
			if script == "" {
				script = "未生成"
			}
			fmt.Fprintf(&b, "%v %s %v 变更=%v 步骤=%v\n  设备报告：%v\n  命令文件：%s\n", row["host"], safeTopologyText(name), display.Status(fmt.Sprint(row["status"])), row["changes"], row["steps"], row["device_report"], script)
			if hash, ok := row["script_sha256"].(string); ok {
				fmt.Fprintf(&b, "  命令 SHA-256：%s\n", hash)
			}
			if diagnostic, ok := row["diagnostic"].(map[string]any); ok {
				fmt.Fprintf(&b, "  诊断：%v\n", display.Error(fmt.Sprint(diagnostic["error_code"])))
			}
			if comparisons, ok := row["neighbors"].([]topology.Comparison); ok {
				for _, item := range comparisons {
					fmt.Fprintf(&b, "  %s %s 当前=%s 预期=%s\n", safeTopologyText(item.Interface), display.Status(item.Status), topologyQuoted(item.Current), topologyQuoted(item.Expected))
				}
			}
			if issues, ok := row["issues"].([]topology.Issue); ok {
				for _, item := range issues {
					fmt.Fprintf(&b, "  %s %s\n", safeTopologyText(item.Interface), topologyReason(item.Code))
				}
			}
			if commands, ok := row["commands"].([]string); ok {
				for _, command := range commands {
					fmt.Fprintf(&b, "  命令=%q\n", command)
				}
			}
		}
	}
	return b.String()
}

func TopologyResultLines(summary map[string]any) string {
	var b strings.Builder
	stats, _ := summary["statistics"].(map[string]int)
	result := "仍有待处理项"
	switch {
	case summary["orchestration_error_type"] != nil || summary["interrupted"] == true || summary["progress_error"] != nil:
		result = "未完成"
	case summary["success"] == true:
		result = "完成"
	case stats["failed"] > 0:
		result = "执行存在失败"
	default:
		rows, _ := summary["devices"].([]map[string]any)
		if len(rows) == 0 {
			result = "没有符合条件的设备"
		} else {
			switch summary["mode"] {
			case "plan":
				result = "计划已生成，仍需修正或核验"
			case "discover":
				result = "发现已完成，仍有未核验接口"
			case "apply":
				result = "执行已完成，仍有待处理项"
			}
		}
	}
	fmt.Fprintf(&b, "结果：%s；模式：%v\n设备：已采集 %d；无需变更 %d；有候选变更 %d；失败 %d\n接口：候选变更 %d；未核验 %d（涉及 %d 台设备）\n", result, display.Status(fmt.Sprint(summary["mode"])), stats["collected"], stats["unchanged"], stats["change_devices"], stats["failed"], stats["changes"], stats["review_interfaces"], stats["review_devices"])
	reasons := map[string]int{}
	if rows, ok := summary["devices"].([]map[string]any); ok {
		for _, row := range rows {
			if issues, ok := row["issues"].([]topology.Issue); ok {
				for _, issue := range issues {
					reasons[issue.Code]++
				}
			}
		}
	}
	keys := make([]string, 0, len(reasons))
	for key := range reasons {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s %d 个接口", topologyReason(key), reasons[key]))
	}
	reasonsText := strings.Join(parts, "；")
	if reasonsText == "" {
		reasonsText = "无"
	}
	fmt.Fprintf(&b, "未核验原因：%s\n", reasonsText)
	return b.String()
}

func makeTopologyOutcomes(outcomes []topologyOutcome) []TopologyOutcome {
	items := make([]TopologyOutcome, 0, len(outcomes))
	for _, out := range outcomes {
		items = append(items, TopologyOutcome{Device: out.device, Audit: out.audit, Result: out.result, Plan: out.plan})
	}
	return items
}
