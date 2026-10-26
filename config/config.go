package config

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

type Options struct {
	Name, CWD, Home, Program, ConfigFile, EnvFile string
	Directories                                   []string
	Environment                                   map[string]string
	Overrides, Defaults                           map[string]any
	AllowMissing                                  bool
}
type Snapshot struct {
	data                map[string]any
	environment         map[string]string
	files, envFiles     []string
	fields              map[string]string
	configPath, envPath string
	applicable          bool
}

func (s Snapshot) Public() map[string]any { return clone(s.data).(map[string]any) }
func (s Snapshot) Value(path string) any {
	var v any = s.data
	for _, key := range strings.Split(path, ".") {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[key]
	}
	return clone(v)
}
func (s Snapshot) Secret(key string) string { return s.environment[key] }
func (s Snapshot) Environment() map[string]string {
	m := map[string]string{}
	for k, v := range s.environment {
		m[k] = v
	}
	return m
}
func (s Snapshot) Sources() map[string]any {
	fields := map[string]string{}
	for k, v := range s.fields {
		fields[k] = v
	}
	return map[string]any{"configuration_files": append([]string{}, s.files...), "environment_files": append([]string{}, s.envFiles...), "fields": fields}
}
func (s Snapshot) Missing() bool                { return !s.applicable }
func (s Snapshot) InitPaths() (string, string)  { return s.configPath, s.envPath }
func (s Snapshot) String() string               { return "config.Snapshot" }
func (s Snapshot) GoString() string             { return s.String() }
func (s Snapshot) MarshalJSON() ([]byte, error) { return json.Marshal(s.data) }

var pathFields = map[string]bool{"netdisco.cache_directory": true, "netdisco.db_sslrootcert": true, "backup.directory": true, "topology.directory": true, "ssh.known_hosts": true, "ssh.log_directory": true, "tftp.verification_root": true}

func absolute(path, base, home string) string {
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(base, path)
}
func pathValue(v, base, home string) (string, error) {
	if strings.ContainsRune(v, 0) {
		return "", ErrInvalid
	}
	if strings.Contains(v, "://") {
		return v, nil
	}
	return absolute(v, base, home), nil
}
func resolvePaths(m map[string]any, base, home, prefix string) error {
	for k, v := range m {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		if child, ok := v.(map[string]any); ok {
			if e := resolvePaths(child, base, home, key); e != nil {
				return e
			}
		} else if pathFields[key] || strings.HasPrefix(key, "tftp.verification_roots.") {
			if s, ok := v.(string); ok && s != "" {
				if (key == "ssh.known_hosts" && s == "inherit") || (key == "netdisco.db_sslrootcert" && s == "system") {
					continue
				}
				p, e := pathValue(s, base, home)
				if e != nil {
					return e
				}
				m[k] = p
			}
		}
	}
	return nil
}
func envPaths(m map[string]string, base, home string) error {
	for path, f := range schema {
		if !pathFields[path] {
			continue
		}
		v, ok := m[f.Env]
		if !ok || v == "" {
			continue
		}
		if (path == "ssh.known_hosts" && v == "inherit") || (path == "netdisco.db_sslrootcert" && v == "system") {
			continue
		}
		p, e := pathValue(v, base, home)
		if e != nil {
			return e
		}
		m[f.Env] = p
	}
	if v := m["NC_CONFIG"]; strings.TrimSpace(v) != "" {
		p, e := pathValue(v, base, home)
		if e != nil {
			return e
		}
		m["NC_CONFIG"] = p
	}
	return nil
}
func unique(paths []string, last string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, p := range paths {
		if p != "" && p != last && !seen[p] {
			out = append(out, p)
			seen[p] = true
		}
	}
	if last != "" {
		out = append(out, last)
	}
	return out
}
func read(path string, required, allowMissing bool) ([]byte, bool, error) {
	// Configuration follows readable symlinks, unlike private backup storage.
	// NONBLOCK lets us inspect and reject FIFOs without waiting for a writer.
	fd, e := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if errors.Is(e, os.ErrNotExist) && (!required || allowMissing) {
		return nil, false, nil
	}
	if e != nil {
		return nil, false, ErrInvalid
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	info, e := file.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Size() > MaxBytes {
		return nil, false, ErrInvalid
	}
	b, e := io.ReadAll(io.LimitReader(file, MaxBytes+1))
	if e != nil || len(b) > MaxBytes {
		return nil, false, ErrInvalid
	}
	return b, true, nil
}
func Load(o Options) (Snapshot, error) {
	s := Snapshot{data: map[string]any{}, environment: map[string]string{}, fields: map[string]string{}}
	if o.Name != "backup" && o.Name != "topology" {
		return s, ErrInvalid
	}
	// Validate and own caller data before walking or merging any containers.
	for _, layer := range []*map[string]any{&o.Defaults, &o.Overrides} {
		owned, e := copyPlain(*layer)
		if e != nil {
			return s, e
		}
		*layer = owned.(map[string]any)
	}
	actualCWD, e := os.Getwd()
	if e != nil {
		return s, e
	}
	if o.Home == "" {
		o.Home, e = os.UserHomeDir()
		if e != nil {
			return s, e
		}
	}
	o.Home = absolute(o.Home, actualCWD, o.Home)
	if !filepath.IsAbs(o.Home) {
		return s, ErrInvalid
	}
	if o.CWD == "" {
		o.CWD = actualCWD
	}
	o.CWD = absolute(o.CWD, actualCWD, o.Home)
	process := map[string]string{}
	for k, v := range o.Environment {
		process[k] = v
	}
	if e := envPaths(process, o.CWD, o.Home); e != nil {
		return s, e
	}
	user := process["XDG_CONFIG_HOME"]
	if user == "" {
		user = filepath.Join(o.Home, ".config")
	}
	user = filepath.Join(absolute(user, o.CWD, o.Home), "net-connector")
	dirs := append([]string{}, o.Directories...)
	if o.Directories == nil {
		program := o.Program
		if program == "" {
			program = os.Args[0]
		}
		dirs = []string{"/etc/net-connector", "/usr/local/etc/net-connector", user, filepath.Dir(absolute(program, o.CWD, o.Home)), o.CWD}
	}
	for i, d := range dirs {
		dirs[i] = absolute(d, o.CWD, o.Home)
	}
	dirs = unique(dirs, "")
	envFile := ""
	if o.EnvFile != "" {
		envFile = absolute(o.EnvFile, o.CWD, o.Home)
	}
	envPathsList := []string{}
	for _, dir := range dirs {
		envPathsList = append(envPathsList, filepath.Join(dir, ".env"))
	}
	for _, path := range unique(envPathsList, envFile) {
		b, ok, e := read(path, path == envFile, o.AllowMissing)
		if e != nil {
			return s, e
		}
		if !ok {
			continue
		}
		m, e := ParseEnvironment(string(b), s.environment, process)
		if e != nil {
			return s, e
		}
		if e = envPaths(m, filepath.Dir(path), o.Home); e != nil {
			return s, e
		}
		for k, v := range m {
			s.environment[k] = v
		}
		s.envFiles = append(s.envFiles, path)
	}
	selected := o.ConfigFile
	if selected == "" {
		selected = process["NC_CONFIG"]
		if strings.TrimSpace(selected) == "" {
			selected = s.environment["NC_CONFIG"]
		}
	}
	if selected != "" {
		selected = absolute(selected, o.CWD, o.Home)
	}
	paths := []string{}
	for _, dir := range dirs {
		paths = append(paths, filepath.Join(dir, "net-connector.yml"), filepath.Join(dir, o.Name+".yml"))
	}
	apply := func(m map[string]any, source string) error {
		if e := resolvePaths(m, filepath.Dir(source), o.Home, ""); e != nil {
			return e
		}
		recordFields(s.fields, s.data, m, source, "")
		merge(s.data, m)
		return nil
	}
	for _, path := range unique(paths, selected) {
		b, ok, e := read(path, path == selected, o.AllowMissing)
		if e != nil {
			return s, e
		}
		if !ok {
			continue
		}
		m, e := ParseYAML(b)
		if e != nil {
			return s, e
		}
		if filepath.Base(path) == "net-connector.yml" {
			for k := range m {
				if k != "common" && k != "backup" && k != "topology" && k != "vxlan" && k != "wireless" {
					return s, ErrInvalid
				}
			}
			if c, ok := m["common"]; ok {
				common, ok := c.(map[string]any)
				if !ok {
					return s, ErrInvalid
				}
				for k := range common {
					if k != "netdisco" && k != "inventory" && k != "ssh" {
						return s, ErrInvalid
					}
				}
				if e = apply(common, path); e != nil {
					return s, e
				}
			}
			if c, ok := m[o.Name]; ok {
				native, ok := c.(map[string]any)
				if !ok {
					return s, ErrInvalid
				}
				if e = apply(native, path); e != nil {
					return s, e
				}
				s.applicable = true
			}
		} else {
			if e = apply(m, path); e != nil {
				return s, e
			}
			s.applicable = true
		}
		s.files = append(s.files, path)
	}
	yamlData := s.data
	processPresence := map[string]any{}
	for path, f := range schema {
		if _, ok := process[f.Env]; ok {
			set(processPresence, path, true)
		}
	}
	s.data = map[string]any{}
	merge(s.data, o.Defaults)
	if e := resolvePaths(s.data, o.CWD, o.Home, ""); e != nil {
		return s, e
	}
	// Higher layers mask invalid lower values before environment decoding.
	for _, layer := range []struct {
		env    map[string]string
		higher map[string]any
		source string
	}{{s.environment, merged(merged(yamlData, processPresence), o.Overrides), ".env"}, {process, o.Overrides, "environment"}} {
		m := map[string]any{}
		for path, f := range schema {
			value, ok := layer.env[f.Env]
			if !ok || has(layer.higher, path) {
				continue
			}
			v, e := decodeEnv(value, f.Kind)
			if e != nil {
				return s, ErrInvalid
			}
			set(m, path, v)
		}
		if layer.source == ".env" {
			merge(s.data, m)
			merge(s.data, yamlData)
		} else {
			recordFields(s.fields, s.data, m, layer.source, "")
			merge(s.data, m)
		}
	}
	overrides := clone(o.Overrides)
	if overrides != nil {
		m := overrides.(map[string]any)
		if e := resolvePaths(m, o.CWD, o.Home, ""); e != nil {
			return s, e
		}
		recordFields(s.fields, s.data, m, "cli", "")
		merge(s.data, m)
	}
	// Layer merging can exceed a budget even when each layer fits independently.
	if _, e := copyPlain(s.data); e != nil {
		return s, e
	}
	if e := validate(s.data); e != nil {
		return s, e
	}
	for k, v := range process {
		s.environment[k] = v
	}
	if _, e := environmentSize(s.environment); e != nil {
		return s, e
	}
	s.configPath = selected
	if s.configPath == "" {
		s.configPath = filepath.Join(user, o.Name+".yml")
	}
	s.envPath = envFile
	if s.envPath == "" {
		s.envPath = filepath.Join(user, ".env")
	}
	return s, nil
}
func has(m map[string]any, path string) bool {
	parts := strings.SplitN(path, ".", 2)
	v, ok := m[parts[0]]
	if !ok {
		return false
	}
	if len(parts) == 1 {
		return true
	}
	child, ok := v.(map[string]any)
	if !ok {
		return true
	}
	return has(child, parts[1])
}
func set(m map[string]any, path string, v any) {
	parts := strings.SplitN(path, ".", 2)
	if len(parts) == 1 {
		m[path] = v
		return
	}
	child, ok := m[parts[0]].(map[string]any)
	if !ok {
		child = map[string]any{}
		m[parts[0]] = child
	}
	set(child, parts[1], v)
}
func merged(a, b map[string]any) map[string]any {
	m := map[string]any{}
	merge(m, a)
	merge(m, b)
	return m
}
func decodeJSON(s string) (any, error) {
	var v any
	d := json.NewDecoder(strings.NewReader(s))
	if d.Decode(&v) != nil {
		return nil, ErrInvalid
	}
	var trailing any
	if d.Decode(&trailing) != io.EOF {
		return nil, ErrInvalid
	}
	return v, nil
}
func validate(m map[string]any) error {
	for section, v := range m {
		values, ok := v.(map[string]any)
		if !ok {
			return ErrInvalid
		}
		known := false
		for path := range schema {
			if strings.HasPrefix(path, section+".") {
				known = true
				break
			}
		}
		if !known {
			return ErrInvalid
		}
		for key, v := range values {
			f, ok := schema[section+"."+key]
			if !ok {
				return ErrInvalid
			}
			valid := false
			switch f.Kind {
			case "string":
				s, ok := v.(string)
				valid = ok && strings.TrimSpace(s) != ""
			case "integer", "optional_integer":
				n, ok := integer(v)
				valid = ok && n > 0 || v == nil && f.Kind == "optional_integer"
			case "number":
				n, ok := v.(float64)
				if i, iok := integer(v); iok {
					n = float64(i)
					ok = true
				}
				valid = ok && n > 0 && !math.IsInf(n, 0) && !math.IsNaN(n)
			case "boolean":
				_, valid = v.(bool)
			case "list":
				a, ok := v.([]any)
				valid = ok
				for _, item := range a {
					s, ok := item.(string)
					valid = valid && ok && strings.TrimSpace(s) != "" && !strings.Contains(s, ",")
				}
			case "json", "json_array":
				if f.Kind == "json_array" || key == "device_rules" {
					_, valid = v.([]any)
				} else {
					_, valid = v.(map[string]any)
				}
			}
			if !valid {
				return ErrInvalid
			}
		}
	}
	return nil
}
func recordFields(fields map[string]string, previous, current map[string]any, source, prefix string) {
	for key, v := range current {
		name := prefix + "/" + strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
		before, _ := previous[key].(map[string]any)
		if child, ok := v.(map[string]any); ok && len(child) > 0 {
			delete(fields, name)
			recordFields(fields, before, child, source, name)
		} else if child, ok := v.(map[string]any); ok && len(child) == 0 && len(before) > 0 {
			continue
		} else {
			for k := range fields {
				if strings.HasPrefix(k, name+"/") {
					delete(fields, k)
				}
			}
			fields[name] = source
		}
	}
}
