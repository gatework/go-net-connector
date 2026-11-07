package fleet

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/inventory"
	"github.com/gatework/go-net-connector/storage"
	"github.com/gatework/go-net-connector/vendors"
)

func TestFrozenRubyReportAndPartialReceipts(t *testing.T) {
	var fixture struct{ Report map[string]any }
	data, e := os.ReadFile("../testdata/ruby-contracts.json")
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(data, &fixture); e != nil {
		t.Fatal(e)
	}
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	rules, e := NewRules(RuleOptions{}, vendors.NewBuiltinRegistry())
	if e != nil {
		t.Fatal(e)
	}
	batch := Batch{mode: "backup", startedAt: now, finishedAt: now.Add(2 * time.Second), callbackErrors: []CallbackError{}}
	for i, status := range []string{"backed_up", "saved_with_error", "filtered", "failed"} {
		record := rules.Record(inventory.Device{IP: fmt.Sprintf("192.0.2.%d", i+1), Name: fmt.Sprintf("edge%d", i+1), Vendor: "cisco", OS: "ios"})
		out := Outcome{Device: record, Status: status}
		if status != "filtered" {
			out.StartedAt = now
			out.FinishedAt = now.Add(time.Second)
			out.Duration = time.Second
		}
		if status == "backed_up" || status == "saved_with_error" {
			out.Artifact.Backup = &connector.BackupReceipt{Path: fmt.Sprintf("/fixtures/%d.txt", i+1), Bytes: 5, SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("hello"))), CollectedAt: now, Change: "created", DeviceCompleted: true, File: storage.Receipt{State: "durable"}}
		}
		if status == "saved_with_error" || status == "failed" {
			out.ErrorCode = "device_error"
			out.ErrorType = "Net::Connector::DeviceError"
		}
		batch.outcomes = append(batch.outcomes, out)
	}
	report, e := NewReport(batch, "selected")
	if e != nil {
		t.Fatal(e)
	}
	b, e := json.Marshal(report)
	if e != nil {
		t.Fatal(e)
	}
	var got map[string]any
	json.Unmarshal(b, &got)
	// New encoding metadata is additive; compare the established Ruby fields unchanged.
	delete(got, "text_warning_devices")
	for _, value := range got["devices"].([]any) {
		row := value.(map[string]any)
		delete(row, "source")
		delete(row, "encoding")
		delete(row, "text_warning")
	}
	if !reflect.DeepEqual(got, fixture.Report) {
		want, _ := json.MarshalIndent(fixture.Report, "", "  ")
		t.Fatalf("report differs\nGo: %s\nRuby: %s", b, want)
	}
}
