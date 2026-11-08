package cli

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/gatework/go-net-connector/internal/display"
	"github.com/gatework/go-net-connector/internal/workflow"
)

// renderBatch is the shared presentation boundary for CLI and TFTP backups.
func (r Runtime) renderBatch(result workflow.Result, o options, interrupted bool) error {
	b := result.Batch
	if !o.json {
		stats := b.Report.Statistics()
		failed, _ := stats["failed"].(int)
		partial, _ := stats["partial"].(int)
		unverified := 0
		if verification, ok := stats["verification"].(map[string]int); ok {
			unverified = verification["unverified"]
		}
		textWarnings, _ := stats["text_warning_devices"].(int)
		reportError, _ := stats["report_error"].(string)
		var text strings.Builder
		if failed > 0 || partial > 0 || unverified > 0 || textWarnings > 0 || reportError != "" || interrupted {
			text.WriteString(display.Section(r.Output, "异常与警告"))
			if failed > 0 || partial > 0 {
				fmt.Fprintf(&text, "任务异常：失败 %d 台；部分完成 %d 台；逐台诊断见报告。\n", failed, partial)
			}
			if interrupted {
				text.WriteString("批次已中断；未完成设备未执行。\n")
			}
			if unverified > 0 {
				fmt.Fprintf(&text, "服务器文件待核验：%d 台设备仅报告上传完成。\n", unverified)
			}
			if textWarnings > 0 {
				fmt.Fprintf(&text, "编码警告：%d 台；请核对设备配置中的异常字符。\n", textWarnings)
			}
			if reportError != "" {
				fmt.Fprintf(&text, "报告写入异常：%s\n", display.Error(reportError))
			}
		}
		text.WriteString(display.Section(r.Output, "执行结果"))
		state := "完成"
		if !b.Report.Success() {
			state = "未完成"
		}
		fmt.Fprintf(&text, "批次%s：成功 %v；部分完成 %v；失败 %v；跳过 %v\n", state, stats["succeeded"], stats["partial"], stats["failed"], stats["skipped"])
		mode := "CLI 配置采集"
		if o.mode == "tftp" {
			mode = "TFTP 原生文件备份"
		}
		fmt.Fprintf(&text, "备份方式：%s\n", mode)
		if verification, ok := stats["verification"].(map[string]int); ok {
			fmt.Fprintf(&text, "服务器核验：已验证 %d；未验证 %d\n", verification["verified"], verification["unverified"])
		}
		text.WriteString(display.Section(r.Output, "文件与报告"))
		for _, path := range b.ServerArchiveDirectories {
			fmt.Fprintf(&text, "服务器归档目录：%s\n", display.Text(path))
		}
		fmt.Fprintf(&text, "备份目录：%s\n文本报告：%s\nJSON 报告：%s\n", display.Text(b.Directory), display.Text(filepath.Join(b.Directory, "summary.txt")), display.Text(filepath.Join(b.Directory, "summary.json")))
		n, e := fmt.Fprint(r.Output, text.String())
		if e == nil && n != text.Len() {
			return io.ErrShortWrite
		}
		return e
	}

	summary := b.Report.Statistics()
	exit := 1
	if result.Success {
		exit = 0
	}
	if interrupted {
		exit = 130
	}
	return writeReportOutput(r.Output, map[string]any{"directory": b.Directory, "counts": summary["counts"], "text_warning_devices": summary["text_warning_devices"], "tasks_succeeded": b.Report.Success(), "success": b.Report.Success(), "policy": summary["policy"], "policy_success": b.Report.Success(), "exit_status": exit, "report_location": summary["report_location"], "report_error": summary["report_error"]})
}
