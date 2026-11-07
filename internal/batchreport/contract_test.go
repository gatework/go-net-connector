package batchreport

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestBackupEvidenceHasTypedReportShape(t *testing.T) {
	source := &BackupSource{
		Path: "router.cfg.raw", Bytes: 5, SHA256: strings.Repeat("c", 64),
		File:            FileReceipt{Path: "router.cfg.raw", State: "durable", Phase: "complete"},
		NoWriteVerified: true,
		Segments:        []SourceSegment{{Step: 1, Offset: 0, Bytes: 5}},
	}
	warning := &TextWarning{Count: 1, Positions: []TextPosition{{Offset: 3, Line: 1, Column: 4}}}

	data, err := json.Marshal(Device{Mode: "backup", HasBackup: true, Host: "192.0.2.1", Source: source, TextWarning: warning})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"source": map[string]any{
			"path": "router.cfg.raw", "bytes": float64(5), "sha256": strings.Repeat("c", 64),
			"file":              map[string]any{"path": "router.cfg.raw", "state": "durable", "phase": "complete"},
			"no_write_verified": true,
			"segments":          []any{map[string]any{"step": float64(1), "offset": float64(0), "bytes": float64(5)}},
		},
		"text_warning": map[string]any{
			"count":     float64(1),
			"positions": []any{map[string]any{"offset": float64(3), "line": float64(1), "column": float64(4)}},
		},
	}
	if got["source"] == nil || !reflect.DeepEqual(got["source"], want["source"]) || !reflect.DeepEqual(got["text_warning"], want["text_warning"]) {
		t.Fatalf("backup evidence shape changed: %s", data)
	}
	var decoded Device
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded.Source, source) || !reflect.DeepEqual(decoded.TextWarning, warning) {
		t.Fatalf("typed evidence round trip changed: %#v", decoded)
	}
}

func TestSummarySharesReportAndExportContract(t *testing.T) {
	path := "/backups/router.cfg"
	checksum := strings.Repeat("a", 64)
	bytes := int64(42)
	summary := Summary{
		SchemaVersion: SchemaVersion, Mode: "backup", Status: "succeeded",
		Devices: []Device{{Mode: "backup", Host: "192.0.2.1", Status: "backed_up", Path: &path, Bytes: &bytes, SHA256: &checksum}},
	}
	data, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	var row map[string]any
	if err = json.Unmarshal(data, &row); err != nil {
		t.Fatal(err)
	}
	devices := row["devices"].([]any)
	device := devices[0].(map[string]any)
	if _, exists := device["remote_path"]; exists {
		t.Fatalf("backup row contains TFTP fields: %s", data)
	}
	artifact, err := SelectArtifact(data, "192.0.2.1")
	if err != nil || artifact.Path != path || artifact.Bytes != bytes || artifact.SHA256 != checksum {
		t.Fatalf("artifact=%+v error=%v", artifact, err)
	}
}

func TestTFTPExportRequiresServerVerification(t *testing.T) {
	localPath := "/backups/router.cfg"
	remotePath := "router.cfg"
	checksum := strings.Repeat("b", 64)
	bytes := int64(12)
	makeSummary := func(verification string) []byte {
		data, err := json.Marshal(Summary{
			SchemaVersion: SchemaVersion, Mode: "tftp", Status: "succeeded",
			Devices: []Device{{Mode: "tftp", Host: "192.0.2.2", Status: "reported_uploaded", Path: &remotePath, RemotePath: &remotePath, LocalFile: &localPath, Bytes: &bytes, SHA256: &checksum, ServerSHA256: &checksum, Verification: &verification, ServerFileVerified: verification == "server_verified"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	if _, err := SelectArtifact(makeSummary("device_reported"), "192.0.2.2"); err == nil {
		t.Fatal("accepted device-reported upload as verified server artifact")
	}
	artifact, err := SelectArtifact(makeSummary("server_verified"), "192.0.2.2")
	if err != nil || artifact.Path != localPath || artifact.Bytes != bytes {
		t.Fatalf("artifact=%+v error=%v", artifact, err)
	}
}
