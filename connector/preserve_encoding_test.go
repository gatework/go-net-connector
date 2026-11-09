package connector

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestPreserveInvalidEncodingHasWarningAndNoSidecar(t *testing.T) {
	wire := "show config\r\ndescription \xe2\x80\x94\xe2\r\nsw#"
	d, _ := fixtureDevice(t, fixtureDriver(), []string{wire})
	path := filepath.Join(t.TempDir(), "device.txt")
	r, e := d.Backup(t.Context(), BackupOptions{Path: path, TextOnly: true, PreserveInvalidEncoding: true})
	if e != nil || !r.Complete() || r.Encoding != "original-bytes" || r.TextWarning == nil || r.TextWarning.Count != 1 {
		t.Fatalf("encoding=%s warning=%v error=%v", r.Encoding, r.TextWarning, e)
	}
	b, e := os.ReadFile(path)
	if e != nil || !bytes.Contains(b, []byte("\xe2\x80\x94\xe2")) || bytes.Contains(b, []byte("\\xE2")) {
		t.Fatal("source bytes changed", e)
	}
	files, _ := filepath.Glob(path + ".*.raw")
	if len(files) > 0 {
		t.Fatal(files)
	}
}
