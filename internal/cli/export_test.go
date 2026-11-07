package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func exportBatchFixture(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	body := []byte("中文配置\n")
	name := "核心-192.0.2.1.txt"
	sum := sha256.Sum256(body)
	if err := os.WriteFile(filepath.Join(d, name), body, 0600); err != nil {
		t.Fatal(err)
	}
	report := map[string]any{"schema_version": 2, "mode": "backup", "devices": []any{map[string]any{"host": "192.0.2.1", "path": filepath.Join(d, name), "bytes": len(body), "sha256": hex.EncodeToString(sum[:]), "status": "failed"}}}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(d, "summary.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestBatchExportOutputHasOnlySavedBytes(t *testing.T) {
	d := exportBatchFixture(t)
	var out, diagnostic bytes.Buffer
	r := Runtime{Input: panicReader{}, Output: &out, Error: &diagnostic}
	if code := r.exportBatch(t.Context(), "192.0.2.1", d, "", false); code != 0 || out.String() != "中文配置\n" || diagnostic.Len() != 0 {
		t.Fatal(code, out.String(), diagnostic.String())
	}
}

func TestBatchExportFileMetadataRetainsOriginalStatus(t *testing.T) {
	d := exportBatchFixture(t)
	var out, diagnostic bytes.Buffer
	r := Runtime{Input: panicReader{}, Output: &out, Error: &diagnostic}
	destination := filepath.Join(t.TempDir(), "saved.txt")
	if code := r.exportBatch(t.Context(), "192.0.2.1", d, destination, true); code != 0 {
		t.Fatal(code, diagnostic.String())
	}
	var result map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil || result["source_status"] != "failed" || result["output"] != destination {
		t.Fatal(out.String(), err)
	}
}

type exportShortWriter struct{}

func (exportShortWriter) Write(b []byte) (int, error) { return max(0, len(b)-1), nil }
func TestBatchExportShortOutputAndCancellation(t *testing.T) {
	d := exportBatchFixture(t)
	var diagnostic bytes.Buffer
	r := Runtime{Input: panicReader{}, Output: exportShortWriter{}, Error: &diagnostic}
	if code := r.exportBatch(t.Context(), "192.0.2.1", d, "", false); code != 2 {
		t.Fatal(code)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	r.Output = io.Discard
	if code := r.exportBatch(ctx, "192.0.2.1", d, "", false); code != 130 {
		t.Fatal(code)
	}
}
