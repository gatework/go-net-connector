package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestStrictYAML(t *testing.T) {
	for _, s := range []string{"a: 1\na: 2", "a: &a [1]\nb: *a", "1: yes", "a: 1\n---\nb: 2", "a: !!python/object {}"} {
		if _, e := ParseYAML([]byte(s)); e == nil {
			t.Fatalf("accepted %q", s)
		}
	}
}
func TestLayerPrecedenceAndPaths(t *testing.T) {
	root := t.TempDir()
	low := filepath.Join(root, "low")
	high := filepath.Join(root, "high")
	os.Mkdir(low, 0700)
	os.Mkdir(high, 0700)
	os.WriteFile(filepath.Join(low, "net-connector.yml"), []byte("ssh:\n  protocol: ssh\nbackup:\n  directory: ./saved\n  parallel: 2\n"), 0600)
	os.WriteFile(filepath.Join(high, "net-connector.yml"), []byte("backup:\n  parallel: 3\n"), 0600)
	os.WriteFile(filepath.Join(high, ".env"), []byte("NC_PARALLEL=1\nNC_DEVICE_PASSWORD='a#$b'\n"), 0600)
	s, e := Load(Options{Name: "backup", CWD: root, Home: root, Directories: []string{low, high}, Environment: map[string]string{"NC_PARALLEL": "4"}, Overrides: map[string]any{"backup": map[string]any{"parallel": 5}}})
	if e != nil {
		t.Fatal(e)
	}
	if s.Value("backup.parallel") != 5 || s.Value("backup.directory") != filepath.Join(low, "saved") {
		t.Fatal(s.Public())
	}
	if s.Secret("NC_DEVICE_PASSWORD") != "a#$b" {
		t.Fatal("dotenv quote")
	}
}
func TestDotenvSnapshot(t *testing.T) {
	m, e := ParseEnvironment("X=local\nY=\"$X-${Z}\"\nL='$(literal)'\nQ=foo # comment\n", map[string]string{"Z": "old"}, map[string]string{"X": "process"})
	if e != nil || m["Y"] != "process-old" || m["L"] != "$(literal)" || m["Q"] != "foo" {
		t.Fatal(m, e)
	}
	if _, e = ParseEnvironment("X=$(date)", nil, nil); e == nil {
		t.Fatal("command substitution")
	}
}

func TestProcessEnvironmentMasksInvalidDotenv(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".env")
	if e := os.WriteFile(path, []byte("NC_PARALLEL=invalid\nND_CACHE_ENABLED=invalid\nNC_HOST_OVERRIDES=invalid\n"), 0600); e != nil {
		t.Fatal(e)
	}
	process := map[string]string{"NC_PARALLEL": "4", "ND_CACHE_ENABLED": "true", "NC_HOST_OVERRIDES": `{"192.0.2.1":"cisco_ios"}`}
	s, e := Load(Options{Name: "backup", CWD: root, Home: root, Directories: []string{}, EnvFile: path, Environment: process})
	if e != nil || s.Value("backup.parallel") != 4 || s.Value("netdisco.cache_enabled") != true || !reflect.DeepEqual(s.Value("inventory.host_overrides"), map[string]any{"192.0.2.1": "cisco_ios"}) {
		t.Fatalf("winning process layer: %v", e)
	}
	for _, key := range []string{"NC_PARALLEL", "ND_CACHE_ENABLED", "NC_HOST_OVERRIDES"} {
		t.Run("invalid_process_"+key, func(t *testing.T) {
			original := process[key]
			process[key] = "bad"
			defer func() { process[key] = original }()
			if _, e := Load(Options{Name: "backup", CWD: root, Home: root, Directories: []string{}, EnvFile: path, Environment: process}); !errors.Is(e, ErrInvalid) {
				t.Fatalf("invalid winning process value accepted: %v", e)
			}
		})
	}
}

func TestSnapshotPlainDataOwnership(t *testing.T) {
	root := t.TempDir()
	options := func(values map[string]any) Options {
		return Options{Name: "backup", CWD: root, Home: root, Directories: []string{}, Overrides: values}
	}
	input := map[string]any{"inventory": map[string]any{"host_overrides": map[string]any{"192.0.2.1": map[string]any{"vendor": "cisco_ios"}}}}
	s, e := Load(options(input))
	if e != nil {
		t.Fatal(e)
	}
	input["inventory"].(map[string]any)["host_overrides"].(map[string]any)["192.0.2.1"].(map[string]any)["vendor"] = "huawei"
	for _, value := range []any{s.Value("inventory.host_overrides"), s.Public()["inventory"].(map[string]any)["host_overrides"]} {
		value.(map[string]any)["192.0.2.1"].(map[string]any)["vendor"] = "fortinet"
	}
	if got := s.Value("inventory.host_overrides").(map[string]any)["192.0.2.1"].(map[string]any)["vendor"]; got != "cisco_ios" {
		t.Fatalf("snapshot aliased caller: %v", got)
	}
	typed := map[string]any{"inventory": map[string]any{"host_overrides": map[string]any{"192.0.2.1": map[string]string{"vendor": "cisco_ios"}}}}
	if _, e = Load(options(typed)); !errors.Is(e, ErrInvalid) {
		t.Fatalf("unsupported mutable typed map accepted: %v", e)
	}
}

func TestSnapshotRejectsCyclicAndOverBudgetOptions(t *testing.T) {
	root := t.TempDir()
	cycle := map[string]any{}
	cycle["cycle"] = cycle
	slice := make([]any, 1)
	slice[0] = slice
	deep := map[string]any{}
	for range 130 {
		deep = map[string]any{"child": deep}
	}
	for _, tc := range []struct {
		name  string
		value any
	}{
		{"map_cycle", cycle}, {"slice_cycle", slice}, {"depth", deep}, {"nodes", make([]any, 100001)}, {"bytes", strings.Repeat("x", MaxBytes+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, defaults := range []bool{false, true} {
				o := Options{Name: "backup", CWD: root, Home: root, Directories: []string{}}
				values := map[string]any{"inventory": map[string]any{"host_overrides": map[string]any{"value": tc.value}}}
				if defaults {
					o.Defaults = values
				} else {
					o.Overrides = values
				}
				if _, e := Load(o); !errors.Is(e, ErrInvalid) {
					t.Fatalf("defaults=%v invalid plain data accepted: %v", defaults, e)
				}
			}
		})
	}
}

func TestRelativeSourceContextsAreAbsolute(t *testing.T) {
	base, e := os.Getwd()
	if e != nil {
		t.Fatal(e)
	}
	root, e := os.MkdirTemp(base, "config-context-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	rel, e := filepath.Rel(base, root)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(root, "net-connector.yml"), []byte("backup:\n  parallel: 7\n"), 0600); e != nil {
		t.Fatal(e)
	}
	for _, cwd := range []string{".", rel} {
		t.Run(cwd, func(t *testing.T) {
			o := Options{Name: "backup", CWD: cwd, Home: filepath.Join(rel, "home"), Directories: []string{}, Defaults: Defaults("backup")}
			if cwd == rel {
				o.Directories = []string{"."}
			}
			s, e := Load(o)
			if e != nil {
				t.Fatal(e)
			}
			wantBase := base
			if cwd == rel {
				wantBase = root
				if s.Value("backup.parallel") != 7 {
					t.Fatal("relative discovery missed file")
				}
			}
			if got := s.Value("netdisco.cache_directory"); got != filepath.Join(wantBase, ".cache/net-connector/inventory") {
				t.Fatalf("cache=%v", got)
			}
			cfg, env := s.InitPaths()
			if cfg != filepath.Join(root, "home/.config/net-connector/net-connector.yml") || !filepath.IsAbs(env) {
				t.Fatalf("init paths=%s %s", cfg, env)
			}
		})
	}
}

func TestConfigurationSymlinkDiscoveryAndExplicitFiles(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		t.Run(fmt.Sprintf("explicit_%v", explicit), func(t *testing.T) {
			root := t.TempDir()
			targets := t.TempDir()
			for name, content := range map[string]string{"net-connector.yml": "backup:\n  parallel: 6\n  directory: ./saved\n", ".env": "NC_DEVICE_PASSWORD='fixture'\n"} {
				path := filepath.Join(targets, name)
				if e := os.WriteFile(path, []byte(content), 0644); e != nil {
					t.Fatal(e)
				}
				if e := os.Symlink(path, filepath.Join(root, name)); e != nil {
					t.Fatal(e)
				}
			}
			o := Options{Name: "backup", CWD: root, Home: root, Directories: []string{root}}
			if explicit {
				o.Directories = []string{}
				o.ConfigFile = "net-connector.yml"
				o.EnvFile = ".env"
			}
			s, e := Load(o)
			if e != nil || s.Value("backup.parallel") != 6 || s.Secret("NC_DEVICE_PASSWORD") != "fixture" || s.Value("backup.directory") != filepath.Join(root, "saved") {
				t.Fatalf("symlink config: %v", e)
			}
		})
	}
}

func TestConfigurationRejectsNonregularAndOversizedFiles(t *testing.T) {
	for _, env := range []bool{false, true} {
		for _, kind := range []string{"directory", "fifo", "oversized"} {
			t.Run(fmt.Sprintf("env_%v/%s", env, kind), func(t *testing.T) {
				root := t.TempDir()
				path := filepath.Join(root, "input")
				switch kind {
				case "directory":
					if e := os.Mkdir(path, 0700); e != nil {
						t.Fatal(e)
					}
				case "fifo":
					if e := unix.Mkfifo(path, 0600); e != nil {
						t.Fatal(e)
					}
				case "oversized":
					f, e := os.Create(path)
					if e != nil {
						t.Fatal(e)
					}
					if e = f.Truncate(MaxBytes + 1); e != nil {
						t.Fatal(e)
					}
					if e = f.Close(); e != nil {
						t.Fatal(e)
					}
				}
				o := Options{Name: "backup", CWD: root, Home: root, Directories: []string{}}
				if env {
					o.EnvFile = path
				} else {
					o.ConfigFile = path
				}
				if _, e := Load(o); !errors.Is(e, ErrInvalid) {
					t.Fatalf("accepted %s: %v", kind, e)
				}
			})
		}
	}
}

func TestExplicitEmptyDirectoriesDisableDiscovery(t *testing.T) {
	root := t.TempDir()
	if e := os.WriteFile(filepath.Join(root, "net-connector.yml"), []byte("invalid: true"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(root, ".env"), []byte("X=$(no)"), 0600); e != nil {
		t.Fatal(e)
	}
	o := Options{Name: "backup", CWD: root, Home: root, Program: filepath.Join(root, "tool"), Directories: []string{}}
	if _, e := Load(o); e != nil {
		t.Fatalf("disabled discovery read file: %v", e)
	}
	o.Directories = nil
	if _, e := Load(o); !errors.Is(e, ErrInvalid) {
		t.Fatalf("nil directories should discover invalid input: %v", e)
	}
}

func TestIntegerSchemaRejectsFloatingPointScalars(t *testing.T) {
	for _, field := range []string{"backup.parallel", "netdisco.db_port", "topology.limit", "netdisco.max_devices"} {
		for _, value := range []string{"1.0", "1e0", "1"} {
			t.Run(field+"/"+value, func(t *testing.T) {
				root := t.TempDir()
				parts := strings.SplitN(field, ".", 2)
				path := filepath.Join(root, "net-connector.yml")
				if e := os.WriteFile(path, []byte(parts[0]+":\n  "+parts[1]+": "+value+"\n"), 0600); e != nil {
					t.Fatal(e)
				}
				_, e := Load(Options{Name: "backup", CWD: root, Home: root, Directories: []string{}, ConfigFile: path})
				if value == "1" {
					if e != nil {
						t.Fatal(e)
					}
				} else if !errors.Is(e, ErrInvalid) {
					t.Fatalf("float integer accepted: %v", e)
				}
			})
		}
	}
	root := t.TempDir()
	for _, value := range []any{1, 1.0} {
		if _, e := Load(Options{Name: "backup", CWD: root, Home: root, Directories: []string{}, Overrides: map[string]any{"netdisco": map[string]any{"inventory_timeout": value}}}); e != nil {
			t.Fatalf("number field rejected %T: %v", value, e)
		}
	}
}
