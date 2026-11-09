// Package cli hosts the unified command tree without process-global state.
package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/gatework/go-net-connector/config"
	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/fleet"
	"github.com/gatework/go-net-connector/internal/bootstrap"
	"github.com/gatework/go-net-connector/internal/display"
	"github.com/gatework/go-net-connector/internal/progress"
	"github.com/gatework/go-net-connector/internal/workflow"
	"github.com/gatework/go-net-connector/inventory"
	"github.com/gatework/go-net-connector/netdisco"
)

type Runtime struct {
	reporter           *progress.Reporter
	Args               []string
	Environment        map[string]string
	Input              io.Reader
	Output, Error      io.Writer
	CWD, Home, Program string
	Directories        []string
	Source             netdisco.Source
	DeviceFactory      bootstrap.DeviceFactory
}

func integerString(n int) string { return strconv.Itoa(n) }
func (r Runtime) Run(ctx context.Context) int {
	if r.Input == nil {
		r.Input = os.Stdin
	}
	if r.Output == nil {
		r.Output = os.Stdout
	}
	if r.Error == nil {
		r.Error = os.Stderr
	}

	return r.runCommands(ctx)
}
func (r Runtime) executeSelected(ctx context.Context, o options, request commandRequest) int {
	const program = "netman"
	if request.kind == requestDefaults {
		return r.renderDefaults(o.mode, o.json)
	}
	if r.Environment == nil {
		r.Environment = map[string]string{}
		for _, entry := range os.Environ() {
			k, v, _ := strings.Cut(entry, "=")
			r.Environment[k] = v
		}
	}
	var reporter *progress.Reporter
	var stopUpdates func()
	resumeSetup := func() {}
	if !o.quiet && request.startsProgress() {
		reporter = progress.New(r.Error, o.verbose)
		r.reporter = reporter
		_ = reporter.Section(operationLabel(o))
		_ = reporter.Phase("正在加载配置")
		resumeSetup = reporter.Pause()
		defer resumeSetup()
		stopUpdates = reporter.RunUpdates(ctx)
		defer stopUpdates()
	}
	if request.kind == requestExport {
		return r.exportBatch(ctx, o.export, o.batch, o.output, o.json)
	}
	var pushScripts workflow.PushScripts
	if request.kind == requestPushPlan || request.kind == requestPushRun {
		var e error
		if pushScripts, e = r.loadPushScripts(o); e != nil {
			return r.fail(program, e)
		}
	}
	policy, e := bootstrap.LoadPolicy(ctx, bootstrap.PolicyOptions{
		Mode: o.mode, CWD: r.CWD, Home: r.Home, Program: r.Program,
		Directories: r.Directories, ConfigFile: o.config, EnvFile: o.envFile,
		Environment: r.Environment, PublicOverrides: o.public,
		AllowMissing: request.kind == requestInitialize || o.interactive,
	})
	if e != nil {
		return r.fail(program, e)
	}
	backup, e := bootstrap.BackupOptions(policy, o.mode, request.kind == requestInitialize || request.kind == requestShow)
	if e != nil {
		return r.fail(program, e)
	}
	name := "backup"
	if o.mode == "topology" {
		name = "topology"
	}
	if request.kind == requestInitialize || o.interactive {
		createConfig, createEnv := true, true
		reader := bufio.NewReader(contextInput(ctx, r.Input))
		if request.kind == requestInitialize {
			configPath, envPath := policy.InitPaths()
			createConfig, e = missingSetupFile(configPath)
			if e != nil {
				return r.fail(program, e)
			}
			createEnv, e = missingSetupFile(envPath)
			if e != nil {
				return r.fail(program, e)
			}
			if o.interactive && (createConfig || createEnv) {
				if _, ok := r.terminal(); !ok {
					return r.fail(program, config.ErrInvalid)
				}
			}
		} else if o.interactive {
			createConfig, createEnv, e = r.setupConsent(policy, reader)
			if errors.Is(e, errSetupDeclined) {
				return 1
			}
			if e != nil {
				return r.fail(program, e)
			}
		}
		if request.kind == requestInitialize || createConfig || createEnv {
			if o.interactive {
				if createConfig {
					policy, e = r.configure(ctx, policy, o, name, reader)
				}
				if e != nil {
					return r.fail(program, e)
				}
			}
			created, e := config.InitializeSelected(policy, name, createConfig, createEnv)
			return r.renderInitialization(created, o.mode, e, o.json)
		}
	}
	if o.mode == "tftp" && (request.kind == requestCheck || request.kind == requestTFTPRun) {
		if e := bootstrap.ValidateTFTP(policy, backup.Policy); e != nil {
			return r.fail(program, e)
		}
	}
	if request.kind == requestCheck {
		if o.mode == "topology" {
			mode, _ := policy.Value("topology.mode").(string)
			if e := bootstrap.ValidateTopology(policy, mode); e != nil {
				return r.fail(program, e)
			}
		}
		_, e := bootstrap.New(policy, nil)
		if e != nil {
			return r.fail(program, e)
		}
		return r.renderConfiguration(map[string]any{"valid": true, "scope": o.mode}, o.mode, true, o.json)
	}
	if request.kind == requestShow {
		data := policy.Public()
		data["sources"] = policy.Sources()
		return r.renderConfiguration(data, o.mode, false, o.json)
	}
	if o.mode == "topology" {
		if e := bootstrap.ValidateTopology(policy, request.topologyMode()); e != nil {
			return r.fail(program, e)
		}
	}
	resume := reporter.Pause()
	passwordErr := r.passwords(ctx, &o)
	resume()
	if e := passwordErr; e != nil {
		return r.fail(program, e)
	}
	env := policy.Environment()
	for key, value := range o.secrets {
		suffix := ""
		switch key {
		case "NC_DEVICE_USERNAME":
			suffix = "USERNAME"
		case "NC_DEVICE_PASSWORD":
			suffix = "PASSWORD"
		case "NC_ENABLE_PASSWORD":
			suffix = "ENABLE_PASSWORD"
		}
		if suffix != "" {
			for name := range env {
				if strings.HasPrefix(name, "NC_") && strings.HasSuffix(name, "_"+suffix) && (suffix != "PASSWORD" || !strings.HasSuffix(name, "_ENABLE_PASSWORD")) {
					delete(env, name)
				}
			}
		}
		if key == "ND_USERNAME" || key == "ND_PASSWORD" {
			delete(env, "ND_API_KEY")
		}
		env[key] = value
	}
	reader := bufio.NewReader(contextInput(ctx, r.Input))
	if o.stdin {
		if reporter != nil {
			_ = reporter.Phase("等待标准输入凭据：请输入一行 JSON")
		}
		line, e := readLine(reader, 64<<10)
		if e != nil {
			return r.fail(program, e)
		}
		var creds map[string]string
		if json.Unmarshal([]byte(line), &creds) != nil {
			return r.fail(program, config.ErrInvalid)
		}
		for key, target := range map[string]string{"netdisco_username": "ND_USERNAME", "netdisco_password": "ND_PASSWORD", "device_username": "NC_DEVICE_USERNAME", "device_password": "NC_DEVICE_PASSWORD"} {
			if strings.TrimSpace(creds[key]) == "" {
				return r.fail(program, config.ErrInvalid)
			}
			if target == "NC_DEVICE_USERNAME" || target == "NC_DEVICE_PASSWORD" {
				suffix := strings.TrimPrefix(target, "NC_DEVICE_")
				for name := range env {
					if strings.HasPrefix(name, "NC_") && strings.HasSuffix(name, "_"+suffix) && !strings.HasSuffix(name, "_ENABLE_PASSWORD") {
						delete(env, name)
					}
				}
			}
			env[target] = creds[key]
		}
		delete(env, "ND_API_KEY")
	}
	services, e := bootstrap.New(policy, func(key string) string { return env[key] })
	if e != nil {
		return r.fail(program, e)
	}
	if r.DeviceFactory != nil {
		services = services.WithDeviceFactory(r.DeviceFactory)
	}
	resumeSetup()
	if reporter != nil {
		_ = reporter.Phase("正在读取设备清单")
	}
	var rows []inventory.Device
	if r.Source != nil {
		rows, e = r.Source.Devices(ctx)
	} else {
		var result netdisco.CacheResult
		result, e = services.Inventory(ctx, bootstrap.InventoryMode(policy, o.mode, o.refresh, o.flags.Changed("cache-only") || o.flags.Changed("cache-mode")))
		rows = result.Rows
		if result.RefreshErrorCode != "" {
			reporter.Warning("设备清单刷新失败，已使用校验通过的缓存")
		}
		if result.WriteErrorType != "" {
			reporter.Warning("清单缓存写入失败，继续使用本次获取的清单")
		}
	}
	if e != nil {
		if ctx.Err() != nil {
			return 130
		}
		return r.fail(program, e)
	}
	if reporter != nil {
		_ = reporter.Phase(fmt.Sprintf("设备清单读取完成：%d 台；正在生成计划", len(rows)))
	}
	if o.stdin && request.needsRunConfirmation() {
		if reporter != nil {
			_ = reporter.Phase("等待执行确认：请输入 RUN")
		}
		resumeConfirm := reporter.Pause()
		_ = reporter.Notice("等待执行确认：请输入 RUN")
		line, e := readLine(reader, 64)
		resumeConfirm()
		if e != nil || strings.TrimSpace(line) != "RUN" {
			return r.fail(program, config.ErrInvalid)
		}
	}
	w := &workflow.Runner{Observe: workflowObserver(reporter)}
	workflowRun := services.WorkflowRuntime()
	var result workflow.Result
	if o.mode == "topology" {
		var settings workflow.TopologySettings
		settings, e = services.WorkflowTopologySettings(request.topologyMode())
		if e != nil {
			return r.fail(program, e)
		}
		result, e = w.Topology(ctx, workflowRun, settings, rows, request.topology)
	} else if request.kind == requestPushPlan || request.kind == requestPushRun {
		var plan fleet.Plan
		plan, e = fleet.NewPlan(rows, services.Rules(), fleet.PlanOptions{Mode: "backup"})
		if e != nil {
			return r.fail(program, e)
		}
		result, e = w.Push(ctx, workflowRun, plan, workflow.PushOptions{
			Scripts: pushScripts, Save: o.pushSave, StopAfterFailures: o.pushStopAfter,
			Plan: request.kind == requestPushPlan, Directory: r.pushDirectory(o),
		})
	} else if request.kind == requestTFTPRun || request.kind == requestTFTPPlan {
		var settings workflow.TFTPSettings
		settings, e = services.WorkflowTFTPSettings()
		if e != nil {
			return r.fail(program, e)
		}
		result, e = w.TFTP(ctx, workflowRun, settings, rows, workflow.TFTPOptions{BackupOptions: backup, Plan: request.kind == requestTFTPPlan, All: o.all})
	} else {
		limit := services.Limit()
		if o.all {
			limit = 0
		}
		plan, err := fleet.NewPlan(rows, services.Rules(), fleet.PlanOptions{Mode: "backup", LimitPerVendor: limit})
		if err != nil {
			return r.fail(program, err)
		}
		if request.inventoryPreview() {
			result = workflow.Preview(plan)
		} else {
			result, e = w.Backup(ctx, workflowRun, plan, backup)
		}
	}
	if stopUpdates != nil {
		stopUpdates()
	}
	var progressErr error
	if reporter != nil {
		progressErr = reporter.Err()
		workflow.RecordProgressFailure(ctx, &result, progressErr)
	}
	outputErr := r.renderResult(result, o, ctx.Err() != nil)
	workflow.RecordOutputFailure(ctx, result, outputErr)
	if ctx.Err() != nil {
		return 130
	}
	if outputErr != nil {
		return r.fail(program, outputErr)
	}

	if e != nil {
		return r.fail(program, e)
	}
	if result.Success {
		return 0
	}
	return 1

}
func readLine(reader *bufio.Reader, limit int) (string, error) {
	var b strings.Builder
	for {
		part, prefix, e := reader.ReadLine()
		if e != nil {
			return "", e
		}
		if b.Len()+len(part) > limit {
			return "", config.ErrInvalid
		}
		b.Write(part)
		if !prefix {
			return b.String(), nil
		}
	}
}
func (r Runtime) print(v any) int {
	enc := json.NewEncoder(r.Output)
	enc.SetIndent("", "  ")
	if e := enc.Encode(v); e != nil {
		return r.fail("netman", e)
	}
	return 0
}
func (r Runtime) fail(program string, e error) int {
	if errors.Is(e, context.Canceled) {
		return 130
	}
	message := "操作失败（输入无效、数据源不可用或 I/O 错误）"
	if usage, ok := errors.AsType[*commandUsage](e); ok {
		message = usage.message
	}
	if classified, ok := errors.AsType[*connector.Error](e); ok {
		message = display.Error(classified.Code)
	}
	var inventoryError *netdisco.InventoryError
	if errors.As(e, &inventoryError) && inventoryError.Code == "cache_namespace_required" {
		message = display.Error(inventoryError.Code)
	}
	if errors.Is(e, netdisco.ErrCacheRefreshRequired) {
		message = "离线清单缓存不可用，请先使用 prefer 或 refresh 模式刷新对应来源（cache_unavailable）"
	}
	if errors.Is(e, config.ErrInvalid) {
		message = "配置或参数无效，请使用 --help 查看用法并检查配置文件"
	}
	if field, ok := errors.AsType[*config.FieldError](e); ok {
		switch field.Problem {
		case "removed":
			message = "配置 " + display.Text(field.Field) + " 已移除；请改用 " + display.Text(field.Replacement)
		case "required":
			message = "配置 " + display.Text(field.Field) + " 未设置；请通过配置文件、环境变量或对应选项设置"
		default:
			message = "配置 " + display.Text(field.Field) + " 无效；请通过配置文件、环境变量或对应选项设置"
		}
	}
	if errors.Is(e, workflow.ErrTFTPArchivePreflight) {
		message = "TFTP 归档预检失败：请检查归档服务器 SSH 认证、远端 tftp-archive 是否已安装且协议兼容，以及归档目录权限；本次尚未开始设备上传"
	}
	if r.reporter != nil {
		_ = r.reporter.Warning(program + "：" + message)
	} else {
		fmt.Fprintln(r.Error, program+"："+message)
	}
	return 2
}
