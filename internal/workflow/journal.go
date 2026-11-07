package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/gatework/go-net-connector/fleet"
	"github.com/gatework/go-net-connector/internal/progress"
	"github.com/gatework/go-net-connector/storage"
)

// batchJournal records completed outcomes in the same order as progress output.
// fleet serializes OnOutcome calls, so a journal needs no separate lock.
type batchJournal struct {
	*os.File
	progress io.Writer
	reporter *progress.Reporter
}

func openBatchJournal(directory string, progress io.Writer) (*batchJournal, error) {
	file, err := storage.OpenAppend(filepath.Join(directory, "events.jsonl"))
	if err != nil {
		return nil, err
	}
	return &batchJournal{File: file, progress: progress}, nil
}

func (j *batchJournal) record(_ context.Context, out fleet.Outcome) error {
	var progressErr error
	if j.reporter != nil {
		progressErr = j.reporter.Outcome(out.Device.Host, out.Status, out.ErrorCode, out.Duration)
		if a := out.Artifact.Backup; a != nil && a.TextWarning != nil && a.TextWarning.Count > 0 {
			progressErr = errors.Join(progressErr, j.reporter.Warning(fmt.Sprintf("[%s] %s", out.Device.Host, BackupProgress(out))))
		}
	}
	if j.progress != nil {
		_, progressErr = fmt.Fprintf(j.progress, "%s: %s\n", out.Device.Host, BackupProgress(out))
	}
	eventErr := json.NewEncoder(j.File).Encode(map[string]any{"host": out.Device.Host, "vendor": out.Device.Vendor, "artifact": artifactView(out.Artifact), "status": out.Status, "duration_ms": out.Duration.Milliseconds(), "error_code": nullableString(out.ErrorCode), "error_type": nullableString(out.ErrorType)})
	return errors.Join(progressErr, eventErr)
}
