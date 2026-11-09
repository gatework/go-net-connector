package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/internal/display"
	"github.com/gatework/go-net-connector/internal/workflow"
	"github.com/gatework/go-net-connector/vendors"
)

// maxScriptBytes bounds one script file before parsing.
const maxScriptBytes = 1 << 20

// loadPushScripts reads and validates every script before configuration,
// credentials, inventory or any device connection is touched.
func (r Runtime) loadPushScripts(o options) (workflow.PushScripts, error) {
	scripts := workflow.PushScripts{ByPlatform: map[string]connector.Script{}}
	if o.pushScript == "" && len(o.pushScriptFor) == 0 {
		return scripts, usageError("必须通过 --script 或 --script-for 指定命令脚本")
	}
	if o.pushStopAfter < 0 {
		return scripts, usageError("--stop-after-failures 不能为负数")
	}
	known := map[string]bool{}
	for _, name := range vendors.NewBuiltinRegistry().Names() {
		known[name] = true
	}
	if o.pushScript != "" {
		script, err := r.readScript(o.pushScript)
		if err != nil {
			return scripts, err
		}
		scripts.Default = &script
	}
	for _, entry := range o.pushScriptFor {
		platform, path, ok := strings.Cut(entry, "=")
		if !ok || platform == "" || path == "" {
			return scripts, usageError("--script-for 格式应为 平台=文件")
		}
		if !known[platform] {
			return scripts, usageError("未知平台 " + display.Text(platform) + "；可用平台：" + strings.Join(vendors.NewBuiltinRegistry().Names(), "、"))
		}
		if _, exists := scripts.ByPlatform[platform]; exists {
			return scripts, usageError("平台 " + platform + " 重复指定了脚本")
		}
		script, err := r.readScript(path)
		if err != nil {
			return scripts, err
		}
		scripts.ByPlatform[platform] = script
	}
	return scripts, nil
}

func (r Runtime) readScript(path string) (connector.Script, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(r.CWD, path)
	}
	name := filepath.Base(path)
	file, err := os.Open(path)
	if err != nil {
		return connector.Script{}, usageError("无法读取脚本 " + display.Text(name))
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxScriptBytes+1))
	if err != nil {
		return connector.Script{}, usageError("无法读取脚本 " + display.Text(name))
	}
	if len(data) > maxScriptBytes {
		return connector.Script{}, usageError("脚本 " + display.Text(name) + " 超过 1 MiB")
	}
	script, err := connector.ParseScript(string(data), name)
	if err != nil {
		if invalid, ok := errors.AsType[*connector.Error](err); ok && invalid.Line > 0 {
			return connector.Script{}, usageError(fmt.Sprintf("脚本 %s 第 %d 行无效（含 NUL 或非 UTF-8 字符）", display.Text(name), invalid.Line))
		}
		return connector.Script{}, usageError("脚本 " + display.Text(name) + " 无效")
	}
	if len(script.Commands) == 0 {
		return connector.Script{}, usageError("脚本 " + display.Text(name) + " 没有命令")
	}
	// A control byte such as Ctrl-A or ESC would be interpreted by the device
	// line editor rather than sent as command text; reject it before connecting.
	for _, command := range script.Commands {
		if strings.ContainsFunc(command.Text, func(c rune) bool { return c != '\t' && unicode.IsControl(c) }) {
			return connector.Script{}, usageError(fmt.Sprintf("脚本 %s 第 %d 行含控制字符", display.Text(name), command.Line))
		}
	}
	return script, nil
}

// pushDirectory resolves the output directory against the launch directory.
func (r Runtime) pushDirectory(o options) string {
	directory := o.directory
	if directory == "" {
		directory = "pushes"
	}
	if !filepath.IsAbs(directory) {
		directory = filepath.Join(r.CWD, directory)
	}
	return directory
}

func (r Runtime) renderPush(result *workflow.PushResult, o options) error {
	if o.json {
		if result.Report != nil {
			return writeReportOutput(r.Output, result.Report)
		}
		return writeReportOutput(r.Output, map[string]any{"plan": result.Plan})
	}
	var b strings.Builder
	rows := [][5]string{}
	if result.Report == nil {
		for _, d := range result.Plan.Selected {
			rows = append(rows, [5]string{d.Host, optionalText(d.Name), d.Platform, fmt.Sprintf("%s（%d 条）", d.Script, d.Commands), display.Status("ready")})
		}
	} else {
		for _, d := range result.Report.Devices {
			status := display.Status(string(d.Status))
			if d.ErrorCode != "" {
				status += "；" + display.Error(d.ErrorCode)
			}
			if d.FailedLine > 0 {
				status += fmt.Sprintf("；第 %d 行", d.FailedLine)
			}
			rows = append(rows, [5]string{d.Host, optionalText(d.Name), d.Platform, fmt.Sprintf("%s（%d/%d 条）", d.Script, d.AcceptedCommands, d.Commands), status})
		}
	}
	if err := display.Table(&b, rows, 120); err != nil {
		return err
	}
	fmt.Fprintf(&b, "清单 %d 台；下发 %d 台", result.Plan.Total, len(result.Plan.Selected))
	if n := result.Plan.Skipped["missing_script"]; n > 0 {
		fmt.Fprintf(&b, "；%d 台缺少对应平台脚本", n)
	}
	if n := result.Plan.Skipped["save_unsupported"]; n > 0 {
		fmt.Fprintf(&b, "；%d 台平台不支持保存，已跳过", n)
	}
	b.WriteString("\n")
	if report := result.Report; report != nil {
		if report.Stopped {
			b.WriteString("失败数量达到上限，其余设备未启动。\n")
		}
		if report.Interrupted {
			b.WriteString("批次已中断；未开始的设备未连接。\n")
		}
		if report.Counts[string(workflow.PushStatusRejected)] > 0 || report.Counts[string(workflow.PushStatusSaveUnconfirmed)] > 0 {
			b.WriteString("注意：被拒绝或保存未确认的设备可能已部分生效，请按报告逐台核对，不要直接重跑整份脚本。\n")
		}
		fmt.Fprintf(&b, "报告：%s\n", display.Text(filepath.Join(report.Directory, "report.json")))
	}
	_, err := io.WriteString(r.Output, b.String())
	return err
}

func optionalText(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
