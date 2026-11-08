package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/gatework/go-net-connector/fleet"
	"github.com/gatework/go-net-connector/storage"
)

// batchJournal records completed outcomes in the same order as progress output.
// fleet serializes OnOutcome calls, so a journal needs no separate lock.
type batchJournal struct {
	*os.File
	observe func(fleet.Outcome) error
}

func openBatchJournal(directory string, observe func(fleet.Outcome) error) (*batchJournal, error) {
	file, err := storage.OpenAppend(filepath.Join(directory, "events.jsonl"))
	if err != nil {
		return nil, err
	}
	return &batchJournal{File: file, observe: observe}, nil
}

func (j *batchJournal) record(_ context.Context, out fleet.Outcome) error {
	var progressErr error
	if j.observe != nil {
		progressErr = j.observe(out)
	}
	eventErr := json.NewEncoder(j.File).Encode(map[string]any{"host": out.Device.Host, "vendor": out.Device.Vendor, "artifact": artifactView(out.Artifact), "status": out.Status, "duration_ms": out.Duration.Milliseconds(), "error_code": nullableString(out.ErrorCode), "error_type": nullableString(out.ErrorType)})
	return errors.Join(progressErr, eventErr)
}
