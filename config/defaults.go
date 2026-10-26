package config

import (
	"go.yaml.in/yaml/v3"
	"io"
	"os"
	"path/filepath"
)

func Defaults(mode string) map[string]any {
	m := map[string]any{"netdisco": map[string]any{"source": "http", "cache_enabled": true, "cache_mode": "prefer", "cache_ttl": 259200, "cache_directory": "./.cache/net-connector/inventory", "inventory_timeout": 300, "page_size": 500, "allow_insecure_http": false}, "inventory": map[string]any{"include_hosts": []any{}, "exclude_hosts": []any{}, "include_names": []any{}, "exclude_names": []any{}, "include_vendors": []any{}, "exclude_vendors": []any{}, "include_models": []any{}, "exclude_models": []any{}, "name_match": "contains", "model_match": "contains"}, "ssh": map[string]any{"protocol": "ssh", "host_key_policy": "accept_new", "log_level": "info"}, "backup": map[string]any{"directory": "./backups", "parallel": 4}}
	if mode == "topology" {
		m["topology"] = map[string]any{"mode": "plan", "cache_mode": "prefer", "parallel": 4, "directory": "./topologys", "description_format": "To_%{neighbor}_%{port}", "abbreviate_ports": true, "lowercase_ports": false, "neighbor_name_prefixes": []any{}}
	}
	if mode == "tftp" {
		m["tftp"] = map[string]any{"ssh_worker_path": "/usr/local/bin/net-connector-archive"}
	}
	return m
}

const EnvironmentTemplate = `# CLI > process environment > YAML > .env > defaults.
# Credentials are never copied into YAML. Fill in only what you use.
# ND_URL=
# ND_USERNAME=
# ND_PASSWORD=
# ND_API_KEY=
# NC_DEVICE_USERNAME=
# NC_DEVICE_PASSWORD=
# NC_ENABLE_PASSWORD=
# ND_DB_HOST=
# ND_DB_NAME=
# ND_DB_USER=
# ND_DB_PASS=
# NC_TFTP_SSH_PASSWORD=
# Single quotes preserve # and $ literally.
`

// Initialize publishes missing files exclusively; it never replaces an existing entry.
func Initialize(s Snapshot, name string) ([]string, error) {
	return InitializeSelected(s, name, true, true)
}

// InitializeSelected publishes only the files whose creation was approved.
func InitializeSelected(s Snapshot, name string, createConfig, createEnv bool) ([]string, error) {
	doc := s.Public()
	if name == "topology" {
		delete(doc, "backup")
		set(doc, "topology.mode", "plan")
	}
	path, env := s.InitPaths()
	if filepath.Base(path) == "net-connector.yml" {
		doc = map[string]any{name: doc}
	}
	data, e := yaml.Marshal(doc)
	if e != nil {
		return nil, ErrInvalid
	}
	created := []string{}
	for _, item := range []struct {
		path   string
		data   []byte
		create bool
	}{{path, data, createConfig}, {env, []byte(EnvironmentTemplate), createEnv}} {
		if !item.create {
			continue
		}
		if info, e := os.Lstat(item.path); e == nil {
			if !info.Mode().IsRegular() {
				return created, ErrInvalid
			}
			continue
		} else if !os.IsNotExist(e) {
			return created, ErrInvalid
		}
		if e = os.MkdirAll(filepath.Dir(item.path), 0700); e != nil {
			return created, e
		}
		f, e := os.OpenFile(item.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return created, e
		}
		n, e := f.Write(item.data)
		if e == nil && n != len(item.data) {
			e = io.ErrShortWrite
		}
		if e == nil {
			e = f.Sync()
		}
		ce := f.Close()
		if e == nil {
			e = ce
		}
		if e != nil {
			_ = os.Remove(item.path)
			return created, e
		}
		created = append(created, item.path)
	}
	return created, nil
}
func FromEnvironment(values map[string]string) (map[string]any, error) {
	m := map[string]any{}
	for path, f := range schema {
		if value, ok := values[f.Env]; ok {
			v, e := decodeEnv(value, f.Kind)
			if e != nil {
				return nil, ErrInvalid
			}
			set(m, path, v)
		}
	}
	return m, nil
}
