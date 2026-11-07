package workflow

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gatework/go-net-connector/config"
	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/internal/bootstrap"
	"github.com/gatework/go-net-connector/inventory"
)

func TestTFTPPreflightFailureNeverCreatesDevice(t *testing.T) {
	root := t.TempDir()
	blocked := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(blocked, []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	defaults := config.Defaults("tftp")
	defaults["backup"].(map[string]any)["directory"] = filepath.Join(root, "backups")
	defaults["tftp"].(map[string]any)["server"] = "192.0.2.10"
	defaults["tftp"].(map[string]any)["verification_root"] = blocked
	p, err := config.Load(config.Options{Name: "backup", CWD: root, Home: root, Directories: []string{}, Environment: map[string]string{}, Defaults: defaults, AllowMissing: true})
	if err != nil {
		t.Fatal(err)
	}
	s, err := bootstrap.New(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	s = s.WithDeviceFactory(func(connector.Profile, connector.ConnectionConfig) (*connector.Device, error) {
		t.Fatal("device created before preflight succeeded")
		return nil, nil
	})
	r := &Runner{}
	_, err = r.TFTP(t.Context(), runtimeForTest(s), tftpSettingsForTest(s), []inventory.Device{{IP: "192.0.2.1", Vendor: "h3c", Name: "edge"}}, TFTPOptions{BackupOptions: BackupOptions{Policy: "selected"}})
	if !errors.Is(err, ErrTFTPArchivePreflight) {
		t.Fatalf("unclassified preflight failure: %v", err)
	}
}
