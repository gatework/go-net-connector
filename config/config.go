package config

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/gatework/go-net-connector/internal/batchreport"
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

func (s Snapshot) Public() map[string]any { return cloneValue(s.data).(map[string]any) }
func (s Snapshot) Value(path string) any {
	var v any = s.data
	for key := range strings.SplitSeq(path, ".") {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[key]
	}
	return cloneValue(v)
}
func (s Snapshot) Secret(key string) string { return s.environment[key] }
func (s Snapshot) Environment() map[string]string {
	m := map[string]string{}
	maps.Copy(m, s.environment)
	return m
}
func (s Snapshot) Sources() map[string]any {
	fields := map[string]string{}
	maps.Copy(fields, s.fields)
	return map[string]any{"configuration_files": append([]string{}, s.files...), "environment_files": append([]string{}, s.envFiles...), "fields": fields}
}
func (s Snapshot) Missing() bool                { return !s.applicable }
func (s Snapshot) InitPaths() (string, string)  { return s.configPath, s.envPath }
func (s Snapshot) String() string               { return "config.Snapshot" }
func (s Snapshot) GoString() string             { return s.String() }
func (s Snapshot) MarshalJSON() ([]byte, error) { return json.Marshal(s.data) }

var pathFields = map[string]bool{"netdisco.cache_directory": true, "netdisco.db_sslrootcert": true, "backup.directory": true, "topology.directory": true, "ssh.known_hosts": true, "ssh.log_directory": true, "tftp.verification_root": true}

func absolutePath(path, base, home string) string {
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
	return absolutePath(v, base, home), nil
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
func resolveEnvironmentPaths(m map[string]string, base, home string) error {
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
func uniquePaths(paths []string, last string) []string {
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

// Load is the compatibility entry point for callers without a context.
func Load(o Options) (Snapshot, error) {
	return LoadContext(context.Background(), o)
}

// LoadContext loads and freezes the configured layers, checking cancellation
// between discovery, file reads and layer decoding. Filesystem calls and bounded
// parsers remain synchronous; cancellation cannot interrupt a blocked syscall.
func LoadContext(ctx context.Context, o Options) (snapshot Snapshot, err error) {
	s := Snapshot{data: map[string]any{}, environment: map[string]string{}, fields: map[string]string{}}
	if ctx == nil {
		return s, ErrInvalid
	}
	defer func() {
		if canceled := ctx.Err(); canceled != nil && !errors.Is(err, canceled) {
			err = errors.Join(err, canceled)
		}
	}()
	if err := ctx.Err(); err != nil {
		return s, err
	}
	if o.Name != "backup" && o.Name != "topology" {
		return s, ErrInvalid
	}
	// Validate and own caller data before walking or merging any containers.
	for _, layer := range []*map[string]any{&o.Defaults, &o.Overrides} {
		if err := ctx.Err(); err != nil {
			return s, err
		}
		owned, e := copyPlain(*layer)
		if e != nil {
			return s, e
		}
		*layer = owned.(map[string]any)
		if e := rejectRemovedFields(*layer); e != nil {
			return s, e
		}
	}
	if err := ctx.Err(); err != nil {
		return s, err
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
	o.Home = absolutePath(o.Home, actualCWD, o.Home)
	if !filepath.IsAbs(o.Home) {
		return s, ErrInvalid
	}
	if o.CWD == "" {
		o.CWD = actualCWD
	}
	o.CWD = absolutePath(o.CWD, actualCWD, o.Home)
	process := map[string]string{}
	maps.Copy(process, o.Environment)
	if e := resolveEnvironmentPaths(process, o.CWD, o.Home); e != nil {
		return s, e
	}
	user := process["XDG_CONFIG_HOME"]
	if user == "" {
		user = filepath.Join(o.Home, ".config")
	}
	user = filepath.Join(absolutePath(user, o.CWD, o.Home), "net-connector")
	dirs := append([]string{}, o.Directories...)
	if o.Directories == nil {
		program := o.Program
		if program == "" {
			program = os.Args[0]
		}
		dirs = []string{"/etc/net-connector", "/usr/local/etc/net-connector", user, filepath.Dir(absolutePath(program, o.CWD, o.Home)), o.CWD}
	}
	for i, d := range dirs {
		if err := ctx.Err(); err != nil {
			return s, err
		}
		dirs[i] = absolutePath(d, o.CWD, o.Home)
	}
	dirs = uniquePaths(dirs, "")
	envFile := ""
	if o.EnvFile != "" {
		envFile = absolutePath(o.EnvFile, o.CWD, o.Home)
	}
	envPathsList := []string{}
	envOrigins := map[string]string{}
	for _, dir := range dirs {
		envPathsList = append(envPathsList, filepath.Join(dir, ".env"))
	}
	for _, path := range uniquePaths(envPathsList, envFile) {
		missingOK := path != envFile || o.AllowMissing
		b, ok, e := ReadFile(ctx, path, missingOK)
		if e != nil {
			return s, e
		}
		if !ok {
			continue
		}
		m, e := ParseEnvironment(string(b), s.environment, process)
		if err := ctx.Err(); err != nil {
			return s, err
		}
		if e != nil {
			return s, e
		}
		if e = resolveEnvironmentPaths(m, filepath.Dir(path), o.Home); e != nil {
			return s, e
		}
		for k, v := range m {
			s.environment[k] = v
			envOrigins[k] = path
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
		selected = absolutePath(selected, o.CWD, o.Home)
	}
	paths := []string{}
	for _, dir := range dirs {
		paths = append(paths, filepath.Join(dir, "net-connector.yml"))
	}
	apply := func(m map[string]any, source string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if e := rejectRemovedFields(m); e != nil {
			return e
		}
		if e := resolvePaths(m, filepath.Dir(source), o.Home, ""); e != nil {
			return e
		}
		recordFields(s.fields, s.data, m, source, "")
		mergeValues(s.data, m)
		return ctx.Err()
	}
	for _, path := range uniquePaths(paths, selected) {
		missingOK := path != selected || o.AllowMissing
		b, ok, e := ReadFile(ctx, path, missingOK)
		if e != nil {
			return s, e
		}
		if !ok {
			continue
		}
		m, e := ParseYAML(b)
		if err := ctx.Err(); err != nil {
			return s, err
		}
		if e != nil {
			return s, e
		}
		if e = apply(m, path); e != nil {
			return s, e
		}
		s.applicable = true
		s.files = append(s.files, path)
	}
	yamlData := s.data
	processPresence := map[string]any{}
	for path, f := range schema {
		if _, ok := process[f.Env]; ok {
			setPath(processPresence, path, true)
		}
	}
	s.data = map[string]any{}
	mergeValues(s.data, o.Defaults)
	if e := resolvePaths(s.data, o.CWD, o.Home, ""); e != nil {
		return s, e
	}
	defaultFields := map[string]string{}
	recordFields(defaultFields, map[string]any{}, s.data, "default", "")
	for key, source := range defaultFields {
		if _, exists := s.fields[key]; !exists {
			s.fields[key] = source
		}
	}
	// Higher layers mask invalid lower values before environment decoding.
	for _, layer := range []struct {
		env    map[string]string
		higher map[string]any
		source string
	}{{s.environment, mergeMaps(mergeMaps(yamlData, processPresence), o.Overrides), ".env"}, {process, o.Overrides, "environment"}} {
		if err := ctx.Err(); err != nil {
			return s, err
		}
		m, e := environmentValues(layer.env, layer.higher, nil)
		if err := ctx.Err(); err != nil {
			return s, err
		}
		if e != nil {
			return s, e
		}
		if layer.source == ".env" {
			for path, f := range schema {
				if origin := envOrigins[f.Env]; origin != "" && hasPath(m, path) {
					part := map[string]any{}
					setPath(part, path, valueAtPath(m, path))
					recordFields(s.fields, s.data, part, origin, "")
				}
			}
			mergeValues(s.data, m)
			mergeValues(s.data, yamlData)
		} else {
			recordFields(s.fields, s.data, m, layer.source, "")
			mergeValues(s.data, m)
		}
	}
	if err := ctx.Err(); err != nil {
		return s, err
	}
	overrides := cloneValue(o.Overrides)
	if overrides != nil {
		m := overrides.(map[string]any)
		if e := resolvePaths(m, o.CWD, o.Home, ""); e != nil {
			return s, e
		}
		recordFields(s.fields, s.data, m, "cli", "")
		mergeValues(s.data, m)
	}
	// Layer merging can exceed a budget even when each layer fits independently.
	if _, e := copyPlain(s.data); e != nil {
		return s, e
	}
	if e := validateValues(s.data); e != nil {
		return s, e
	}
	maps.Copy(s.environment, process)
	if _, e := environmentSize(s.environment); e != nil {
		return s, e
	}
	s.configPath = selected
	if s.configPath == "" {
		s.configPath = filepath.Join(user, "net-connector.yml")
	}
	s.envPath = envFile
	if s.envPath == "" {
		s.envPath = filepath.Join(user, ".env")
	}
	return s, ctx.Err()
}

func hasPath(values map[string]any, path string) bool {
	parts := strings.SplitN(path, ".", 2)
	v, ok := values[parts[0]]
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
	return hasPath(child, parts[1])
}
func setPath(values map[string]any, path string, value any) {
	parts := strings.SplitN(path, ".", 2)
	if len(parts) == 1 {
		values[path] = value
		return
	}
	child, ok := values[parts[0]].(map[string]any)
	if !ok {
		child = map[string]any{}
		values[parts[0]] = child
	}
	setPath(child, parts[1], value)
}
func mergeMaps(base, overrides map[string]any) map[string]any {
	m := map[string]any{}
	mergeValues(m, base)
	mergeValues(m, overrides)
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
func validateValues(values map[string]any) error {
	for section, v := range values {
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
			case KindString, KindOptionalString:
				s, ok := v.(string)
				valid = ok && (f.Kind == KindOptionalString || strings.TrimSpace(s) != "")
				if section == "backup" && key == "success_policy" && !batchreport.SuccessPolicy(s).Valid() {
					return &FieldError{Field: "backup.success_policy", Problem: "invalid"}
				}
			case KindInteger, KindOptionalInteger:
				n, ok := integerValue(v)
				valid = ok && (n > 0 || section == "backup" && key == "limit_per_vendor" && n == 0) || v == nil && f.Kind == KindOptionalInteger
			case KindNumber:
				n, ok := v.(float64)
				if i, iok := integerValue(v); iok {
					n = float64(i)
					ok = true
				}
				valid = ok && n > 0 && !math.IsInf(n, 0) && !math.IsNaN(n)
			case KindBoolean:
				_, valid = v.(bool)
			case KindList:
				a, ok := v.([]any)
				valid = ok
				for _, item := range a {
					s, ok := item.(string)
					valid = valid && ok && strings.TrimSpace(s) != "" && !strings.Contains(s, ",")
				}
			case KindJSON, KindJSONArray:
				if f.Kind == KindJSONArray || key == "device_rules" {
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
