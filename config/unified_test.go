package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestUnifiedConfigurationAppliesToEveryCommand(t *testing.T) {
	root := t.TempDir()
	body := []byte("netdisco:\n  url: https://inventory.example.test\nbackup:\n  parallel: 7\ntftp:\n  server: 192.0.2.50\n  upload_timeout: 600\ntopology:\n  parallel: 3\n  description_format: To_%{neighbor}_%{port}\n")
	if err := os.WriteFile(filepath.Join(root, "net-connector.yml"), body, 0600); err != nil {
		t.Fatal(err)
	}
	// Old scope files are no longer discovered, even when malformed.
	for _, name := range []string{"backup.yml", "topology.yml"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("broken: ["), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, mode := range []string{"backup", "tftp", "topology"} {
		name := mode
		if name == "tftp" {
			name = "backup"
		}
		s, err := Load(context.Background(), Options{Name: name, CWD: root, Home: root, Directories: []string{root}, Environment: map[string]string{}, Defaults: Defaults(mode)})
		if err != nil {
			t.Fatal(mode, err)
		}
		if s.Value("netdisco.url") != "https://inventory.example.test" || s.Value("backup.parallel") != 7 || s.Value("topology.parallel") != 3 || s.Value("tftp.upload_timeout") != 600 {
			t.Fatal(mode, s.Public())
		}
		files := s.Sources()["configuration_files"].([]string)
		if len(files) != 1 || filepath.Base(files[0]) != "net-connector.yml" {
			t.Fatal(files)
		}
	}
}

func TestScopeFilesAloneDoNotCreateApplicableConfiguration(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"backup.yml", "topology.yml"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("backup:\n  parallel: 99\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	s, err := Load(context.Background(), Options{Name: "backup", CWD: root, Home: root, Directories: []string{root}, Environment: map[string]string{}, Defaults: Defaults("backup")})
	if err != nil || len(s.Sources()["configuration_files"].([]string)) != 0 || s.Value("backup.parallel") != 4 {
		t.Fatal(s.Public(), err)
	}
}

func TestUnifiedInitializerRoundTripAndExistingFile(t *testing.T) {
	root := t.TempDir()
	options := Options{Name: "backup", CWD: root, Home: root, Directories: []string{}, Environment: map[string]string{}, Defaults: Defaults("backup"), Overrides: map[string]any{"netdisco": map[string]any{"url": "https://inventory.example.test"}, "tftp": map[string]any{"server": "192.0.2.50"}}}
	s, err := Load(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	created, err := InitializeSelected(s, "backup", true, true)
	if err != nil || len(created) != 2 {
		t.Fatal(created, err)
	}
	path, _ := s.InitPaths()
	if filepath.Base(path) != "net-connector.yml" {
		t.Fatal(path)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := ParseYAML(body)
	if err != nil {
		t.Fatal(err)
	}
	if doc["netdisco"].(map[string]any)["url"] != "https://inventory.example.test" || doc["tftp"].(map[string]any)["server"] != "192.0.2.50" {
		t.Fatal(doc)
	}
	options.ConfigFile = path
	options.Overrides = nil
	options.Name = "topology"
	options.Defaults = Defaults("topology")
	loaded, err := Load(context.Background(), options)
	if err != nil || loaded.Value("tftp.server") != "192.0.2.50" {
		t.Fatal(err)
	}
	created, err = InitializeSelected(loaded, "topology", true, true)
	if err != nil || len(created) != 0 {
		t.Fatal(created, err)
	}
	after, _ := os.ReadFile(path)
	if string(body) != string(after) {
		t.Fatal("existing unified configuration changed")
	}
}

func TestNestedLegacyDocumentRequiresMigration(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "net-connector.yml")
	for _, body := range []string{"common:\n  ssh:\n    protocol: ssh\n", "backup:\n  backup:\n    parallel: 4\n"} {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(context.Background(), Options{Name: "backup", CWD: root, Home: root, Directories: []string{root}, Defaults: Defaults("backup")}); err == nil {
			t.Fatal("legacy nesting accepted")
		}
	}
}
