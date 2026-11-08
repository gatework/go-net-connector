package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/gatework/go-net-connector/fleet"
)

func TestJournalPersistsEventWhenProgressFails(t *testing.T) {
	cause := errors.New("progress unavailable")
	j, err := openBatchJournal(t.TempDir(), func(fleet.Outcome) error { return cause })
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	err = j.record(context.Background(), fleet.Outcome{Device: fleet.Record{Host: "192.0.2.1", Vendor: "h3c"}, Status: "failed"})
	if !errors.Is(err, cause) {
		t.Fatal(err)
	}
	if err = j.Sync(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(j.Name())
	if err != nil {
		t.Fatal(err)
	}
	var row map[string]any
	if err = json.Unmarshal(data, &row); err != nil || row["host"] != "192.0.2.1" {
		t.Fatal(string(data), err)
	}
}
