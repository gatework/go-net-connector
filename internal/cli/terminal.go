package cli

import (
	"bufio"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/gatework/go-net-connector/config"
	"github.com/gatework/go-net-connector/internal/bootstrap"
	"golang.org/x/term"
)

var errSetupDeclined = errors.New("required configuration was not created")

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
			if _, err := fmt.Fprintf(r.Error, "Not found: %s; create it? [y/N]: ", path); err != nil {
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
			if _, err := fmt.Fprintln(r.Error, "Enter y or n."); err != nil {
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
func (r Runtime) passwords(o *options) error {
	for _, p := range []struct{ flag, key, label string }{{"ask-password", "NC_DEVICE_PASSWORD", "Device password"}, {"ask-netdisco-password", "ND_PASSWORD", "Netdisco password"}} {
		ask, _ := o.flags.GetBool(p.flag)
		if !ask {
			continue
		}
		input, ok := r.terminal()
		if !ok {
			return config.ErrInvalid
		}
		fmt.Fprint(r.Error, p.label+": ")
		password, e := term.ReadPassword(int(input.Fd()))
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
func (r Runtime) configure(snapshot config.Snapshot, o options, name string, reader *bufio.Reader) (config.Snapshot, error) {
	if _, ok := r.terminal(); !ok {
		return snapshot, config.ErrInvalid
	}
	path, _ := snapshot.InitPaths()
	if _, e := os.Lstat(path); e == nil {
		return snapshot, nil
	}
	answer := func(label, defaultValue string) (string, error) {
		fmt.Fprintf(r.Error, "%s [%s]: ", label, defaultValue)
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
	start, e := answer("Start interactive configuration? y/N", "n")
	if e != nil || !strings.EqualFold(start, "y") {
		return snapshot, e
	}
	values := map[string]string{}
	fields := []struct{ key, label, defaultValue string }{{"ND_SOURCE", "Inventory source http/postgres", "http"}, {"ND_URL", "Inventory URL", "https://netdisco.example.invalid"}, {"NC_INCLUDE_HOSTS", "Include hosts", ""}, {"NC_EXCLUDE_HOSTS", "Exclude hosts", ""}, {"NC_INCLUDE_NAMES", "Include names", ""}, {"NC_EXCLUDE_NAMES", "Exclude names", ""}, {"NC_INCLUDE_MODELS", "Include models", ""}, {"NC_EXCLUDE_MODELS", "Exclude models", ""}, {"NC_NAME_MATCH", "Name matching contains/prefix/glob", "contains"}, {"NC_MODEL_MATCH", "Model matching contains/prefix/glob", "contains"}, {"NC_INCLUDE_VENDORS", "Include vendors", ""}, {"NC_EXCLUDE_VENDORS", "Exclude vendors", ""}}
	if name == "topology" {
		fields = append(fields, struct{ key, label, defaultValue string }{"NC_TOPOLOGY_PARALLEL", "Topology concurrency 1..500", "4"}, struct{ key, label, defaultValue string }{"NC_TOPOLOGY_DIRECTORY", "Topology report directory", "./topologys"})
	} else {
		fields = append(fields, struct{ key, label, defaultValue string }{"NC_PARALLEL", "Concurrency 1..500", "4"}, struct{ key, label, defaultValue string }{"NC_BACKUP_DIRECTORY", "Backup directory", "./backups"})
		if o.mode == "tftp" {
			fields = append(fields, struct{ key, label, defaultValue string }{"NC_TFTP_HOST", "TFTP server", ""})
		}
	}
	result := snapshot
	for _, field := range fields {
		if field.key == "ND_URL" && values["ND_SOURCE"] == "postgres" {
			continue
		}
		for {
			v, e := answer(field.label, field.defaultValue)
			if e != nil {
				return snapshot, e
			}
			values[field.key] = v
			overrides, e := config.FromEnvironment(values)
			if e == nil {
				result, e = config.Load(config.Options{Name: name, CWD: r.CWD, Home: r.Home, Program: r.Program, Directories: []string{}, Defaults: snapshot.Public(), Overrides: overrides, Environment: map[string]string{}, AllowMissing: true})
			}
			if e == nil {
				var services *bootstrap.Services
				services, e = bootstrap.New(result, func(string) string { return "" })
				if services != nil {
					services.Close()
				}
			}
			if e == nil && field.key == "ND_SOURCE" && v != "http" && v != "postgres" {
				e = config.ErrInvalid
			}
			if e == nil && field.key == "ND_URL" {
				u, err := url.Parse(v)
				if err != nil || u.Host == "" || u.Scheme != "https" && u.Scheme != "http" {
					e = config.ErrInvalid
				}
			}
			if e == nil {
				break
			}
			fmt.Fprintln(r.Error, "Invalid value; enter it again.")
		}
	}
	// Keep the originally selected config/env paths and environment precedence.
	overrides, e := config.FromEnvironment(values)
	if e != nil {
		return snapshot, e
	}
	return config.Load(config.Options{Name: name, CWD: r.CWD, Home: r.Home, Program: r.Program, Directories: r.Directories, ConfigFile: o.config, EnvFile: o.envFile, Defaults: snapshot.Public(), Overrides: overrides, Environment: r.Environment, AllowMissing: true})
}
