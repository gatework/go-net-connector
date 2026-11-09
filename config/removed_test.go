package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRemovedSourceFileKeysAreRejected(t *testing.T) {
	for _, key := range []string{"h3c_source_file", "h3c_wireless_source_file", "huawei_source_file"} {
		t.Run("yaml "+key, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "net-connector.yml")
			if err := os.WriteFile(path, []byte("tftp:\n  "+key+": flash:/a.cfg\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := Load(Options{Name: "backup", CWD: dir, Home: dir, Directories: []string{dir}, Environment: map[string]string{}, Defaults: Defaults("tftp")})
			assertRemoved(t, err, "tftp."+key)
		})
		t.Run("override "+key, func(t *testing.T) {
			dir := t.TempDir()
			_, err := Load(Options{Name: "backup", CWD: dir, Home: dir, Directories: []string{}, Environment: map[string]string{}, Defaults: Defaults("tftp"), Overrides: map[string]any{"tftp": map[string]any{key: "flash:/a.cfg"}}, AllowMissing: true})
			assertRemoved(t, err, "tftp."+key)
		})
	}
}

func TestRemovedVariableOutsideProjectionIsIgnored(t *testing.T) {
	got, err := InventoryEnvironment(map[string]string{"NC_H3C_TFTP_SOURCE_FILE": "flash:/a.cfg", "ND_URL": "https://inventory.example/api"}, nil)
	if err != nil || got["netdisco"] == nil {
		t.Fatalf("inventory projection rejected an unrelated removed variable: %v %#v", err, got)
	}
}

func assertRemoved(t *testing.T, err error, field string) {
	t.Helper()
	removed, ok := errors.AsType[*FieldError](err)
	if !ok || removed.Problem != "removed" || removed.Field != field || removed.Replacement == "" || !errors.Is(err, ErrInvalid) {
		t.Fatalf("%s not reported as removed: %#v", field, err)
	}
}
