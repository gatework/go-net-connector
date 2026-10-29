package fleet

import (
	"encoding/json"
	"errors"
	"strings"
	"syscall"
	"testing"

	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/storage"
	"github.com/gatework/go-net-connector/transfer"
)

func TestDiagnosticInternalCodesUseCompatibleVocabulary(t *testing.T) {
	for _, tc := range []struct {
		code, phase, wantCode, wantPhase, wantKind string
	}{
		{"cleanup_failed", "cleanup", "transport_error", "close", "TransportError"},
		{"prepare_failed", "script", "script_error", "script", "ScriptError"},
		{"after_command_failed", "script", "script_error", "script", "ScriptError"},
		{"receipt_callback_failed", "script", "script_error", "script", "ScriptError"},
		{"step_callback_failed", "script", "script_error", "script", "ScriptError"},
		{"after_login_failed", "login", "connection_error", "login", "ConnectionError"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			err := errors.Join(&connector.Error{Code: tc.code, Phase: tc.phase}, errors.New("PRIVATE_CLEANUP"))
			got := diagnose(err, Artifact{})
			if got.ErrorCode != tc.wantCode || got.Phase != tc.wantPhase || got.ErrorType != "Net::Connector::"+tc.wantKind {
				t.Fatalf("diagnostic = %#v", got)
			}
			report, reportErr := NewReport(Batch{mode: "backup", outcomes: []Outcome{{Status: "failed", Diagnostic: got}}}, "strict")
			if reportErr != nil {
				t.Fatal(reportErr)
			}
			data, reportErr := json.Marshal(report)
			if reportErr != nil || strings.Contains(string(data), "PRIVATE_CLEANUP") || !strings.Contains(string(data), tc.wantCode) {
				t.Fatalf("report lost safe classification: %s (%v)", data, reportErr)
			}
		})
	}
}

func TestDiagnosticSecondaryCleanupPreservesPrimaryClassification(t *testing.T) {
	primary := &connector.Error{Code: "command_timeout", Phase: "command"}
	cleanup := &connector.Error{Code: "cleanup_failed", Phase: "cleanup"}
	err := errors.Join(primary, cleanup)
	got := diagnose(err, Artifact{})
	if got.ErrorCode != "command_timeout" || got.Phase != "command" || got.ErrorType != "Net::Connector::CommandTimeout" || !errors.Is(err, primary) || !errors.Is(err, cleanup) {
		t.Fatalf("primary classification or error chain lost: %#v", got)
	}
}

func TestDiagnosticCleanupDoesNotReclassifyStorageFailure(t *testing.T) {
	primary := &storage.WriteError{Cause: syscall.ENOSPC, Receipt: storage.Receipt{State: "not_committed", Phase: "write"}}
	secondary := &connector.Error{Code: "backup_finalize_failed", Phase: "backup"}
	err := errors.Join(primary, secondary)
	diagnostic := diagnose(err, Artifact{})
	if diagnostic.ErrorCode != "file_write_failed" || diagnostic.ErrorType != "Net::Connector::Storage::AtomicFile::WriteError" || diagnostic.ArtifactState != "not_committed" || diagnostic.UnderlyingType != "Errno::ENOSPC" {
		t.Fatalf("cleanup replaced primary storage failure: %#v", diagnostic)
	}
	if !errors.Is(err, syscall.ENOSPC) || !errors.Is(err, secondary) {
		t.Fatal("lost error causes")
	}
}

type diagnosticEnvelope struct{ cause error }

func (diagnosticEnvelope) Error() string   { return "safe envelope" }
func (e diagnosticEnvelope) Unwrap() error { return e.cause }

func TestDiagnosticPreservesPrimaryAcrossMetadataTypes(t *testing.T) {
	command := &connector.Error{Code: "command_timeout", Phase: "command"}
	upload := &transfer.Error{Code: "transfer_unconfirmed", Phase: "tftp_backup"}
	for _, tc := range []struct {
		primary, secondary error
		code, phase        string
	}{
		{command, upload, "command_timeout", "command"},
		{upload, command, "transfer_unconfirmed", "tftp_backup"},
	} {
		err := diagnosticEnvelope{errors.Join(tc.primary, tc.secondary)}
		got := diagnose(err, Artifact{})
		if got.ErrorCode != tc.code || got.Phase != tc.phase {
			t.Fatalf("secondary changed primary: %#v", got)
		}
	}
}
