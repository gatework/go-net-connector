package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/gatework/go-net-connector/fleet"
	"github.com/gatework/go-net-connector/internal/display"
	"github.com/gatework/go-net-connector/internal/topologyreport"
	"golang.org/x/term"

	"github.com/gatework/go-net-connector/internal/workflow"
)

func operationLabel(o options) string {
	if o.action == "inventory" {
		return "设备清单"
	}
	switch o.mode {
	case "tftp":
		return "TFTP 备份"
	case "topology":
		return "邻居接口描述"
	default:
		return "配置备份"
	}
}

func (r Runtime) displayTopology(result *workflow.TopologyResult, o options) error {
	if o.json {
		return writeReportOutput(r.Output, result.Summary())
	}
	report := result.Report
	var b strings.Builder
	statistics := report.Statistics
	failed := statistics.Failed
	reviewInterfaces := statistics.ReviewInterfaces
	if failed > 0 || reviewInterfaces > 0 || report.ReportError != "" || report.ProgressError != "" || report.OutputError != "" || report.Interrupted {
		b.WriteString(display.Section(r.Output, "异常与警告"))
		if failed > 0 {
			fmt.Fprintf(&b, "任务异常：%d 台设备执行失败；逐台诊断见报告。\n", failed)
		}
		if reviewInterfaces > 0 {
			fmt.Fprintf(&b, "待核实接口：%d 个，涉及 %d 台设备。\n", reviewInterfaces, statistics.ReviewDevices)
		}
		if report.Interrupted {
			b.WriteString("批次已中断；未完成设备未执行。\n")
		}
		if report.ReportError != "" || report.ProgressError != "" || report.OutputError != "" {
			b.WriteString("报告或进度输出存在异常；请核对下方报告文件。\n")
		}
	}
	b.WriteString(display.Section(r.Output, "执行结果"))
	b.WriteString(topologyreport.ResultLines(report.Presentation()))
	if directory := report.Directory; directory != "" {
		b.WriteString(display.Section(r.Output, "文件与报告"))
		scripts := report.ScriptCount()
		if scripts > 0 {
			fmt.Fprintf(&b, "命令文件：%s（%d 台设备存在变更）\n", display.Text(filepath.Join(directory, "scripts")), scripts)
		}
		fmt.Fprintf(&b, "设备报告：%s\n计划：%s\n报告：%s\n文本报告：%s\n", display.Text(filepath.Join(directory, "devices")), display.Text(filepath.Join(directory, "plan.json")), display.Text(filepath.Join(directory, "summary.json")), display.Text(filepath.Join(directory, "summary.txt")))
	}
	text := strings.Map(func(c rune) rune {
		if c < ' ' && c != '\n' && c != '\t' || c == 127 {
			return ' '
		}
		return c
	}, b.String())
	_, e := fmt.Fprint(r.Output, text)
	return e
}

func writeReportOutput(w io.Writer, v any) error {
	e := json.NewEncoder(w)
	e.SetIndent("", "  ")
	return e.Encode(v)
}
func (r Runtime) renderResult(result workflow.Result, o options, interrupted bool) error {
	if result.Topology != nil {
		if !o.json && result.Topology.Inventory != nil {
			return r.inventoryTable(result.Topology.Inventory)
		}
		return r.displayTopology(result.Topology, o)
	}
	if result.Preview != nil {
		if o.json {
			return writeReportOutput(r.Output, map[string]any{"plan": result.Preview.View})
		}
		return r.inventoryTable(result.Preview.Plan.Entries())
	}
	if result.Batch != nil {
		return r.renderBatch(result, o, interrupted)
	}
	return nil
}

func (r Runtime) inventoryTable(entries []fleet.PlanEntry) error {
	rows := [][5]string{}
	normal := [][5]string{}
	selected := 0
	for _, e := range entries {
		d := e.Device
		if d.Issue == "filtered" {
			continue
		}
		ip := d.Host
		if ip == "" {
			ip = d.SourceIP
		}
		vendor := d.Vendor
		if vendor == "" {
			vendor = d.SourceVendor
		}
		row := [5]string{ip, d.Name, vendor, d.Model, display.Status(e.Status)}
		if e.Status == "ready" {
			selected++
		}
		if e.Status != "ready" && e.Status != "sample_limit" {
			rows = append(rows, row)
		} else {
			normal = append(normal, row)
		}
	}
	rows = append(rows, normal...)
	width := 120
	if f, ok := r.Output.(*os.File); ok {
		if n, _, err := term.GetSize(int(f.Fd())); err == nil {
			width = n
		}
	}
	if err := display.Table(r.Output, rows, width); err != nil {
		return err
	}
	_, err := fmt.Fprintf(r.Output, "清单 %d 台；选中 %d 台\n", len(rows), selected)
	return err
}
