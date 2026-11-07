package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

type panicReader struct{}

func (panicReader) Read([]byte) (int, error) { panic("stdin read") }
func TestHelpDoesNotReadConfigOrStdin(t *testing.T) {
	var out, errout bytes.Buffer
	r := Runtime{Args: []string{"--help"}, Environment: map[string]string{"NC_CONFIG": "/does/not/exist"}, Input: panicReader{}, Output: &out, Error: &errout}
	if code := r.Run(context.Background()); code != 0 || out.Len() == 0 {
		t.Fatal(code, errout.String())
	}
}
func TestInitExitsWithoutInventoryAndNeverCopiesPassword(t *testing.T) {
	root := t.TempDir()
	var out, errout bytes.Buffer
	r := Runtime{Args: []string{"config", "init", "--non-interactive"}, CWD: root, Home: root, Directories: []string{}, Environment: map[string]string{"NC_DEVICE_PASSWORD": "secret-value"}, Input: panicReader{}, Output: &out, Error: &errout}
	if code := r.Run(context.Background()); code != 0 {
		t.Fatal(code, errout.String())
	}
	b, e := os.ReadFile(filepath.Join(root, ".config/net-connector/backup.yml"))
	if e != nil || bytes.Contains(b, []byte("secret-value")) {
		t.Fatal(string(b), e)
	}
}
func TestExportOffline(t *testing.T) {
	root := exportBatchFixture(t)
	var out, errout bytes.Buffer
	r := Runtime{Args: []string{"backup", "export", "192.0.2.1", "--batch", root}, CWD: root, Home: root, Directories: []string{}, Input: panicReader{}, Output: &out, Error: &errout}
	if code := r.Run(context.Background()); code != 0 || out.String() != "中文配置\n" {
		t.Fatal(code, out.String(), errout.String())
	}
}
