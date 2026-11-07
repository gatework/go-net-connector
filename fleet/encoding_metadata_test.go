package fleet

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/storage"
	"github.com/gatework/go-net-connector/transfer"
)

func TestBackupMetadataSnapshotsAndWarningCounts(t *testing.T) {
	receipt := &connector.BackupReceipt{Encoding: "escaped", Source: &connector.BackupSource{Path: "raw", File: storage.Receipt{State: "durable"}, Segments: []connector.SourceSegment{{Step: 1, Bytes: 4}}}, TextWarning: &connector.TextWarning{Count: 1, Positions: []connector.TextPosition{{Offset: 3, Line: 1, Column: 4}}}}
	artifact := Artifact{Backup: receipt}
	cloned := artifact.clone()
	cloned.Backup.Source.Segments[0].Bytes = 999
	cloned.Backup.TextWarning.Positions[0].Offset = 999
	if receipt.Source.Segments[0].Bytes != 4 || receipt.TextWarning.Positions[0].Offset != 3 {
		t.Fatal("snapshot aliases metadata")
	}
	b := Batch{mode: modeBackup, outcomes: []Outcome{{Status: "backed_up", Artifact: artifact}}}
	report, err := NewReport(b, "selected")
	if err != nil {
		t.Fatal(err)
	}
	stats := report.Statistics()
	if stats["text_warning_devices"] != 1 || !report.Success() {
		t.Fatalf("warning changed success: %#v", stats)
	}
	row := report.Summary()["devices"].([]map[string]any)[0]
	if row["source"] == nil || row["encoding"] != "escaped" || row["text_warning"] == nil {
		t.Fatalf("metadata missing: %#v", row)
	}
	sourceView, ok := row["source"].(*connector.BackupSource)
	if !ok {
		t.Fatalf("summary source type changed: %T", row["source"])
	}
	warningView, ok := row["text_warning"].(*connector.TextWarning)
	if !ok {
		t.Fatalf("summary warning type changed: %T", row["text_warning"])
	}
	sourceView.Segments[0].Bytes = 999
	warningView.Positions[0].Offset = 999
	row = report.Summary()["devices"].([]map[string]any)[0]
	if row["source"].(*connector.BackupSource).Segments[0].Bytes != 4 || row["text_warning"].(*connector.TextWarning).Positions[0].Offset != 3 {
		t.Fatal("summary metadata aliases report state")
	}
}

func TestReportJSONAndSummaryMapUseTheSameSchema(t *testing.T) {
	for _, runMode := range []string{"backup", "tftp"} {
		t.Run(runMode, func(t *testing.T) {
			outcome := Outcome{Device: Record{Host: "192.0.2.5", Name: "edge", Vendor: "h3c"}, Status: "failed"}
			if runMode == "backup" {
				outcome.Artifact.Backup = &connector.BackupReceipt{
					Path: "/backup/edge.cfg", Bytes: 17, SHA256: "abc", Change: "changed", Encoding: "utf-8",
					Source: &connector.BackupSource{
						Path: "/backup/edge.cfg.raw", Bytes: 17, SHA256: "def",
						File:     storage.Receipt{Path: "/backup/edge.cfg.raw", State: storage.StateDurable, Phase: "complete"},
						Segments: []connector.SourceSegment{{Step: 1, Offset: 0, Bytes: 17}},
					},
					TextWarning: &connector.TextWarning{Count: 1, Positions: []connector.TextPosition{{Offset: 4, Line: 1, Column: 5}}},
				}
			} else {
				outcome.Artifact.Transfer = &transfer.Receipt{Path: "edge.cfg", Server: "192.0.2.6", Verification: transfer.VerificationDeviceReported}
				outcome.Artifact.Backup = &connector.BackupReceipt{
					Source:      &connector.BackupSource{Path: "unexpected.cfg.raw"},
					TextWarning: &connector.TextWarning{Count: 1},
				}
			}
			report, err := NewReport(Batch{mode: mode(runMode), outcomes: []Outcome{outcome}}, "selected")
			if err != nil {
				t.Fatal(err)
			}
			fromMap, err := json.Marshal(report.Summary())
			if err != nil {
				t.Fatal(err)
			}
			fromReport, err := json.Marshal(report)
			if err != nil {
				t.Fatal(err)
			}
			var mapValue, reportValue any
			if err = json.Unmarshal(fromMap, &mapValue); err != nil {
				t.Fatal(err)
			}
			if err = json.Unmarshal(fromReport, &reportValue); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(mapValue, reportValue) {
				t.Fatalf("summary map and report JSON diverged:\n%s\n%s", fromMap, fromReport)
			}
		})
	}
}
