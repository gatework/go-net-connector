// Package topologyreport renders detached topology presentation values.
// Execution eligibility and retained evidence belong to workflow and topology.
package topologyreport

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gatework/go-net-connector/internal/display"
	"github.com/gatework/go-net-connector/topology"
)

type Statistics struct {
	Collected, Failed, Unchanged, ChangeDevices, Changes, ReviewDevices, ReviewInterfaces int
}

func (s Statistics) Values() map[string]int {
	return map[string]int{"collected": s.Collected, "failed": s.Failed, "unchanged": s.Unchanged,
		"change_devices": s.ChangeDevices, "changes": s.Changes, "review_devices": s.ReviewDevices, "review_interfaces": s.ReviewInterfaces}
}

type Diagnostic struct{ Code string }
type Device struct {
	Named                                  bool
	Name, Host, Vendor, Status             string
	Changes, Steps                         *int
	ScriptFile, ScriptSHA256, DeviceReport string
	Diagnostic                             *Diagnostic
	Comparisons                            []topology.Comparison
	Issues                                 []topology.Issue
	Commands                               []string
}
type Summary struct {
	Mode                                                        string
	Success, Interrupted                                        bool
	ReportError, ProgressError, OutputError, OrchestrationError string
	Statistics                                                  Statistics
	Devices                                                     []Device
}

func countValue(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}
func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func DeviceText(out Device) string {
	var body strings.Builder
	fmt.Fprintf(&body, "设备：%s（%s）\n厂商：%s\n状态：%v；候选变更：%v\n已完成步骤：%v\n", safeTopologyText(out.Name), out.Host, out.Vendor, display.Status(out.Status), countValue(out.Changes), countValue(out.Steps))
	if out.ScriptFile != "" {
		fmt.Fprintf(&body, "命令文件：%s\n命令 SHA-256：%v\n", out.ScriptFile, out.ScriptSHA256)
	} else {
		body.WriteString("命令文件：未生成（无已核验的配置变更）\n")
	}
	if out.Diagnostic != nil {
		fmt.Fprintf(&body, "诊断：%v\n", display.Error(fmt.Sprint(nullableString(out.Diagnostic.Code))))
	}
	body.WriteString("配置命令来自本计划；实际执行结果请核对设备状态与回执。\n")
	for _, row := range out.Comparisons {
		fmt.Fprintf(&body, "%s %s 当前=%s 目标=%s\n", safeTopologyText(row.Interface), display.Status(row.Status), topologyQuoted(row.Current), topologyQuoted(row.Expected))
	}
	for _, issue := range out.Issues {
		fmt.Fprintf(&body, "未核验：%s %s\n", safeTopologyText(issue.Interface), topologyReason(issue.Code))
	}
	return body.String()
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

func SummaryText(summary Summary) string {
	var b strings.Builder
	b.WriteString(ResultLines(summary))
	for _, out := range summary.Devices {
		script := out.ScriptFile
		if script == "" {
			script = "未生成"
		}
		name := ""
		if out.Named {
			name = out.Name
		}
		fmt.Fprintf(&b, "%v %s %v 变更=%v 步骤=%v\n  设备报告：%v\n  命令文件：%s\n", out.Host, safeTopologyText(name), display.Status(out.Status), countValue(out.Changes), countValue(out.Steps), nullableString(out.DeviceReport), script)
		if out.ScriptSHA256 != "" {
			fmt.Fprintf(&b, "  命令 SHA-256：%s\n", out.ScriptSHA256)
		}
		if out.Diagnostic != nil {
			fmt.Fprintf(&b, "  诊断：%v\n", display.Error(fmt.Sprint(nullableString(out.Diagnostic.Code))))
		}
		for _, item := range out.Comparisons {
			fmt.Fprintf(&b, "  %s %s 当前=%s 预期=%s\n", safeTopologyText(item.Interface), display.Status(item.Status), topologyQuoted(item.Current), topologyQuoted(item.Expected))
		}
		for _, item := range out.Issues {
			fmt.Fprintf(&b, "  %s %s\n", safeTopologyText(item.Interface), topologyReason(item.Code))
		}
		for _, command := range out.Commands {
			fmt.Fprintf(&b, "  命令=%q\n", command)
		}
	}
	return b.String()
}

func ResultLines(summary Summary) string {
	var b strings.Builder
	stats := summary.Statistics
	result := "仍有待处理项"
	switch {
	case summary.OrchestrationError != "" || summary.Interrupted || summary.ProgressError != "":
		result = "未完成"
	case summary.Success:
		result = "完成"
	case stats.Failed > 0:
		result = "执行存在失败"
	default:
		if len(summary.Devices) == 0 {
			result = "没有符合条件的设备"
		} else {
			switch summary.Mode {
			case "plan":
				result = "计划已生成，仍需修正或核验"
			case "discover":
				result = "发现已完成，仍有未核验接口"
			case "apply":
				result = "执行已完成，仍有待处理项"
			}
		}
	}
	fmt.Fprintf(&b, "结果：%s；模式：%v\n设备：已采集 %d；无需变更 %d；有候选变更 %d；失败 %d\n接口：候选变更 %d；未核验 %d（涉及 %d 台设备）\n", result, display.Status(summary.Mode), stats.Collected, stats.Unchanged, stats.ChangeDevices, stats.Failed, stats.Changes, stats.ReviewInterfaces, stats.ReviewDevices)
	reasons := map[string]int{}
	for _, out := range summary.Devices {
		for _, issue := range out.Issues {
			reasons[issue.Code]++
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
