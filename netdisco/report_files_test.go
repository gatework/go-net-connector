package netdisco

import (
	"encoding/json"
	"github.com/gatework/go-net-connector/connector"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestReportFilesPreservesFirstReportError(t *testing.T) {
	for _, tc := range []struct {
		name   string
		repair bool
	}{{"all writes fail", false}, {"json repair commits", true}} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := NewReport(Batch{mode: "backup", outcomes: []Outcome{{Status: "backed_up"}}}, "strict")
			if err != nil {
				t.Fatal(err)
			}
			r = r.WithReportError(connector.ErrAuthentication, "earlier.json")
			dir := filepath.Join(t.TempDir(), "missing")
			if tc.repair {
				dir = t.TempDir()
				if err := os.Mkdir(filepath.Join(dir, "summary.txt"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			got := WriteReport(t.Context(), r, dir, Plan{}, 1)
			if got.batch.reportError != r.batch.reportError || !reflect.DeepEqual(got.diagnostic, r.diagnostic) {
				t.Fatalf("primary report failure replaced: %#v -> %#v", r, got)
			}
			wantLocation := "earlier.json"
			if tc.repair {
				wantLocation = filepath.Join(dir, "summary.json")
			}
			if got.batch.reportLocation != wantLocation {
				t.Fatalf("location=%q want=%q", got.batch.reportLocation, wantLocation)
			}
			if tc.repair {
				data, err := os.ReadFile(wantLocation)
				if err != nil {
					t.Fatal(err)
				}
				var summary map[string]any
				if err := json.Unmarshal(data, &summary); err != nil {
					t.Fatal(err)
				}
				if summary["report_error"] != "Net::Connector::AuthenticationError" {
					t.Fatalf("saved primary error replaced: %s", data)
				}
			}
		})
	}
}
func TestReportFilesFailureKeepsSafeStorageDiagnostic(t *testing.T) {
	r, err := NewReport(Batch{mode: "backup", outcomes: []Outcome{{Status: "backed_up"}}}, "strict")
	if err != nil {
		t.Fatal(err)
	}
	got := WriteReport(t.Context(), r, filepath.Join(t.TempDir(), "private-marker"), Plan{}, 1)
	if got.batch.reportError != "Net::Connector::Storage::AtomicFile::WriteError" || got.diagnostic.UnderlyingType != "Errno::ENOENT" || got.diagnostic.Phase != "report" {
		t.Fatalf("report write classification lost: %#v", got)
	}
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "private-marker") {
		t.Fatal("report diagnostics leaked path")
	}
}
