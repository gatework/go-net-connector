package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func batchFixture(t *testing.T, mode string) (string, map[string]any) {
	t.Helper()
	directory := t.TempDir()
	body := []byte("设备配置\n\xff")
	name := "核心交换机-192.0.2.1.txt"
	if err := os.WriteFile(filepath.Join(directory, name), body, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	row := map[string]any{"host": "192.0.2.1", "name": "核心交换机", "path": "/old/location/" + name, "bytes": len(body), "sha256": hex.EncodeToString(sum[:]), "status": "failed"}
	if mode == "tftp" {
		row["local_file"] = row["path"]
		row["path"] = "remote-upload.cfg"
		row["verification"] = "server_verified"
	}
	writeBatchFixture(t, directory, mode, []any{row})
	return directory, row
}
func writeBatchFixture(t *testing.T, directory, mode string, rows []any) {
	t.Helper()
	b, e := json.Marshal(map[string]any{"schema_version": 2, "mode": mode, "success": false, "devices": rows})
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(directory, "summary.json"), b, 0600); e != nil {
		t.Fatal(e)
	}
}
func TestReadBatchSavedKeepsBytesAndLateFailureEvidence(t *testing.T) {
	for _, mode := range []string{"backup", "tftp"} {
		t.Run(mode, func(t *testing.T) {
			d, _ := batchFixture(t, mode)
			b, a, e := ReadBatchSaved(t.Context(), d, "192.0.2.1")
			if e != nil || string(b) != "设备配置\n\xff" || a.Status != "failed" || a.Host != "192.0.2.1" {
				t.Fatalf("%q %#v %v", b, a, e)
			}
			destination := filepath.Join(t.TempDir(), "export.cfg")
			_, receipt, e := ExportBatchSaved(t.Context(), d, "192.0.2.1", destination)
			if e != nil || !receipt.Committed() {
				t.Fatal(receipt, e)
			}
			got, e := os.ReadFile(destination)
			if e != nil || string(got) != string(b) {
				t.Fatal(string(got), e)
			}
		})
	}
}
func TestReadBatchSavedRejectsInvalidEvidence(t *testing.T) {
	for _, kind := range []string{"missing", "duplicate", "hash", "size", "traversal", "symlink", "schema", "unverified"} {
		t.Run(kind, func(t *testing.T) {
			d, row := batchFixture(t, "backup")
			rows := []any{row}
			mode := "backup"
			switch kind {
			case "missing":
				row["host"] = "192.0.2.2"
			case "duplicate":
				rows = append(rows, row)
			case "hash":
				row["sha256"] = string(make([]byte, 64))
			case "size":
				row["bytes"] = 100
			case "traversal":
				row["path"] = "../核心交换机-192.0.2.1.txt"
			case "symlink":
				p := filepath.Join(d, "核心交换机-192.0.2.1.txt")
				os.Remove(p)
				os.Symlink(filepath.Join(t.TempDir(), "outside"), p)
			case "unverified":
				mode = "tftp"
				row["local_file"] = row["path"]
			}
			writeBatchFixture(t, d, mode, rows)
			if kind == "schema" {
				os.WriteFile(filepath.Join(d, "summary.json"), []byte(`{"schema_version":1}`), 0600)
			}
			if b, _, e := ReadBatchSaved(t.Context(), d, "192.0.2.1"); e == nil || len(b) != 0 {
				t.Fatal("accepted invalid evidence", kind, e)
			}
		})
	}
}
func TestReadBatchSavedRequiresExplicitBatchAndContext(t *testing.T) {
	if _, _, e := ReadBatchSaved(t.Context(), "", "192.0.2.1"); e == nil {
		t.Fatal("accepted implicit batch")
	}
	d, _ := batchFixture(t, "backup")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, e := ReadBatchSaved(ctx, d, "192.0.2.1"); e == nil {
		t.Fatal("ignored cancellation")
	}
}

func TestReadBatchSavedDoesNotReadReportAbsoluteLocation(t *testing.T) {
	d, row := batchFixture(t, "backup")
	outside := filepath.Join(t.TempDir(), "outside.txt")
	body := []byte("external secret")
	if err := os.WriteFile(outside, body, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	row["path"] = outside
	row["bytes"] = len(body)
	row["sha256"] = hex.EncodeToString(sum[:])
	writeBatchFixture(t, d, "backup", []any{row})
	if b, _, err := ReadBatchSaved(t.Context(), d, "192.0.2.1"); err == nil || len(b) != 0 {
		t.Fatalf("read report external location: %q %v", b, err)
	}
}

func TestReadBatchSavedRejectsSummarySymlink(t *testing.T) {
	d, _ := batchFixture(t, "backup")
	path := filepath.Join(d, "summary.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "summary.json")
	if err = os.WriteFile(outside, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	if _, _, err = ReadBatchSaved(t.Context(), d, "192.0.2.1"); err == nil {
		t.Fatal("followed report symlink")
	}
}
