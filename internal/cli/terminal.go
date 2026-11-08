package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/gatework/go-net-connector/config"
	"github.com/gatework/go-net-connector/internal/bootstrap"
	"github.com/gatework/go-net-connector/transfer"
	"golang.org/x/term"
)

var errSetupDeclined = errors.New("未创建必需的配置文件")

func missingSetupFile(path string) (bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil || !info.Mode().IsRegular() {
		return false, config.ErrInvalid
	}
	return false, nil
}

func (r Runtime) setupConsent(snapshot config.Snapshot, reader *bufio.Reader) (bool, bool, error) {
	configPath, envPath := snapshot.InitPaths()
	configMissing, err := missingSetupFile(configPath)
	if err != nil {
		return false, false, err
	}
	envMissing, err := missingSetupFile(envPath)
	if err != nil {
		return false, false, err
	}
	if !configMissing && !envMissing {
		return false, false, nil
	}
	if _, ok := r.terminal(); !ok {
		return false, false, config.ErrInvalid
	}
	approve := func(path string) (bool, error) {
		for {
			if _, err := fmt.Fprintf(r.Error, "未找到文件：%s；是否创建？[y/N]：", path); err != nil {
				return false, err
			}
			line, err := readLine(reader, 64)
			if err != nil {
				return false, err
			}
			switch strings.ToLower(strings.TrimSpace(line)) {
			case "y", "yes":
				return true, nil
			case "", "n", "no":
				return false, nil
			}
			if _, err := fmt.Fprintln(r.Error, "请输入 y 或 n。"); err != nil {
				return false, err
			}
		}
	}
	createConfig, createEnv := false, false
	if configMissing {
		createConfig, err = approve(configPath)
		if err != nil {
			return false, false, err
		}
		if !createConfig {
			return false, false, errSetupDeclined
		}
	}
	if envMissing {
		createEnv, err = approve(envPath)
	}
	return createConfig, createEnv, err
}

func (r Runtime) terminal() (*os.File, bool) {
	input, ok := r.Input.(*os.File)
	if !ok || !term.IsTerminal(int(input.Fd())) {
		return nil, false
	}
	output, ok := r.Error.(*os.File)
	return input, ok && term.IsTerminal(int(output.Fd()))
}
func (r Runtime) passwords(ctx context.Context, o *options) error {
	for _, p := range []struct{ flag, key, label string }{{"ask-password", "NC_DEVICE_PASSWORD", "设备密码"}, {"ask-netdisco-password", "ND_PASSWORD", "Netdisco 密码"}} {
		ask, _ := o.flags.GetBool(p.flag)
		if !ask {
			continue
		}
		input, ok := r.terminal()
		if !ok {
			return config.ErrInvalid
		}
		fmt.Fprint(r.Error, p.label+": ")
		password, e := readTerminalSecret(ctx, input)
		fmt.Fprintln(r.Error)
		if e != nil {
			return e
		}
		if len(password) == 0 || strings.ContainsAny(string(password), "\r\n\x00") {
			return config.ErrInvalid
		}
		o.secrets[p.key] = string(password)
	}
	return nil
}
func (r Runtime) configure(ctx context.Context, snapshot config.Snapshot, o options, name string, reader *bufio.Reader) (config.Snapshot, error) {
	if _, ok := r.terminal(); !ok {
		return snapshot, config.ErrInvalid
	}
	path, _ := snapshot.InitPaths()
	if _, e := os.Lstat(path); e == nil {
		return snapshot, nil
	}
	answer := func(label, defaultValue string) (string, error) {
		if _, e := fmt.Fprintf(r.Error, "%s [%s]: ", label, defaultValue); e != nil {
			return "", e
		}
		line, e := readLine(reader, 64<<10)
		if e != nil {
			return "", e
		}
		line = strings.TrimSpace(line)
		if line == "" {
			line = defaultValue
		}
		return line, nil
	}
	start, e := answer("是否开始交互式配置？y/N", "n")
	if e != nil || !strings.EqualFold(start, "y") {
		return snapshot, e
	}
	values := map[string]string{}
	for key, value := range o.public {
		values[key] = value
	}
	ask := func(key, label, defaultValue string, valid func(string) bool) error {
		for {
			value, err := answer(label, defaultValue)
			if err != nil {
				return err
			}
			if !valid(value) {
				if _, err = fmt.Fprintln(r.Error, "值无效，请重新输入。"); err != nil {
					return err
				}
				continue
			}
			values[key] = value
			return nil
		}
	}
	source, _ := snapshot.Value("netdisco.source").(string)
	if source == "" {
		source = "http"
	}
	urlValue, _ := snapshot.Value("netdisco.url").(string)
	query, _ := snapshot.Value("netdisco.query").(string)
	fields, _ := snapshot.Sources()["fields"].(map[string]string)
	if source == "http" && urlValue == "" && query == "" && fields["/netdisco/source"] == "default" {
		if e = ask("ND_SOURCE", "清单来源 http/postgres", source, func(v string) bool { return v == "http" || v == "postgres" }); e != nil {
			return snapshot, e
		}
		source = values["ND_SOURCE"]
	}
	if source == "http" && urlValue == "" {
		if e = ask("ND_URL", "清单服务地址", "", func(v string) bool {
			u, err := url.Parse(v)
			return err == nil && u.Host != "" && u.User == nil && (u.Scheme == "https" || u.Scheme == "http")
		}); e != nil {
			return snapshot, e
		}
	}
	if source == "postgres" && query == "" {
		if e = ask("ND_QUERY", "PostgreSQL 只读清单查询", "", func(v string) bool { return strings.TrimSpace(v) != "" }); e != nil {
			return snapshot, e
		}
	}
	if o.mode == "tftp" {
		server, _ := snapshot.Value("tftp.server").(string)
		servers, _ := snapshot.Value("tftp.servers").(map[string]any)
		if server == "" && len(servers) == 0 {
			if e = ask("NC_TFTP_HOST", "TFTP 服务器地址", "", func(v string) bool { return transfer.ValidateServer(v) == nil }); e != nil {
				return snapshot, e
			}
			server = values["NC_TFTP_HOST"]
		}
		root, _ := snapshot.Value("tftp.verification_root").(string)
		roots, _ := snapshot.Value("tftp.verification_roots").(map[string]any)
		if server != "" && root == "" && roots[server] == nil {
			if e = ask("NC_TFTP_ROOT", "TFTP 归档根目录（本地路径或 ssh://用户@服务器/目录）", "", func(v string) bool { _, err := transfer.VerificationRoot(v); return err == nil }); e != nil {
				return snapshot, e
			}
		}
	}

	// Keep the originally selected config/env paths and environment precedence.
	overrides, e := config.FromEnvironment(values)
	if e != nil {
		return snapshot, e
	}
	result, e := config.LoadContext(ctx, config.Options{Name: name, CWD: r.CWD, Home: r.Home, Program: r.Program, Directories: r.Directories, ConfigFile: o.config, EnvFile: o.envFile, Defaults: snapshot.Public(), Overrides: overrides, Environment: r.Environment, AllowMissing: true})
	if e == nil {
		_, e = bootstrap.New(result, nil)
	}
	return result, e
}
