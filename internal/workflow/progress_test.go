package workflow

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/fleet"
)

func TestFinishObserverFailureDowngradesPersistedResult(t *testing.T) {
	directory := t.TempDir()
	report := &TopologyReport{Mode: "plan", Directory: directory, outcomes: []topologyOutcome{{
		device: fleet.Record{Host: "192.0.2.1"}, state: topologyUnchanged, audited: true,
	}}}
	report.summarize(false)
	if err := persistTopologySummary(t.Context(), directory, report); err != nil {
		t.Fatal(err)
	}
	cause := errors.New("terminal output failed")
	runner := Runner{Observe: func(event Event) error {
		if _, finished := event.(FinishedEvent); finished {
			return cause
		}
		return nil
	}}
	runner.result = Result{Success: true, Topology: &TopologyResult{Directory: directory, Report: report}}
	runner.finishBatch(t.Context())
	if runner.result.Success || !errors.Is(runner.progressErr, cause) {
		t.Fatal("final delivery failure was lost")
	}
	data, err := os.ReadFile(filepath.Join(directory, "summary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]any
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved["success"] != false || saved["progress_error"] != "IOError" {
		t.Fatalf("complete report survived failed delivery: %s", data)
	}
	if len(report.outcomes) != 1 || report.outcomes[0].state != topologyUnchanged {
		t.Fatal("presentation failure changed acquired evidence")
	}
}

func TestOutcomeEventRetainsUnconfirmedSourceState(t *testing.T) {
	var received OutcomeEvent
	runner := Runner{Observe: func(event Event) error {
		received = event.(OutcomeEvent)
		return nil
	}}
	out := fleet.Outcome{Status: "failed", Artifact: fleet.Artifact{
		Backup: &connector.BackupReceipt{TextWarning: &connector.TextWarning{Count: 1}},
	}}
	if err := runner.outcome(out); err != nil {
		t.Fatal(err)
	}
	if received.TextWarningBytes != 1 || received.SourceCommitted || received.SourceVerified {
		t.Fatal("progress promoted an unconfirmed source")
	}
}
