package netdisco

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/inventory"
	"github.com/gatework/go-net-connector/storage"
	"github.com/gatework/go-net-connector/vendors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
)

func TestDiagnosticFrozenMetadataVocabulary(t *testing.T) {
	// Frozen engine/error/metadata.rb TYPES: connector classes are fully qualified.
	want := words(`Net::Connector::Error Net::Connector::ConnectionError Net::Connector::AuthenticationError Net::Connector::LoginTimeout Net::Connector::CommandTimeout Net::Connector::WriteTimeout Net::Connector::PromptError Net::Connector::ConnectionClosed Net::Connector::TransportError Net::Connector::DeviceError Net::Connector::ConfigurationError Net::Connector::ScriptError Net::Connector::InternalError Net::Connector::OutputLimitExceeded Net::Connector::ScriptOutputLimitExceeded Net::Connector::SessionBusy Net::Connector::UnsupportedOperation Net::Connector::LogError Net::Connector::ParsingError Net::Connector::BackupBusy Net::Connector::BackupPersistenceError Net::Connector::BackupFinalizeError Net::Connector::TftpCompletionError StandardError RuntimeError ArgumentError TypeError IOError EOFError SystemCallError ThreadError RangeError KeyError IndexError NoMethodError NameError FrozenError NotImplementedError Timeout::Error IO::TimeoutError Errno::EIO Errno::EACCES Errno::EPERM Errno::ENOSPC Errno::EROFS Errno::ENOENT Errno::ENOTDIR Errno::EISDIR Errno::ELOOP Errno::EINVAL Errno::ENOSYS Errno::EMFILE Errno::ENFILE Errno::ENOMEM Errno::EAGAIN Errno::ENOTSUP Errno::EOPNOTSUPP Errno::ETIMEDOUT Errno::ECONNREFUSED Errno::ECONNRESET Net::Connector::RemoteBackup::CompletionError Net::Connector::Storage::AtomicFile::WriteError Net::Connector::Storage::AtomicFile::PersistenceError Net::Connector::Storage::AtomicFile::DirectorySyncUnsupported Net::Connector::Netdisco::Tftp::Archive::Unavailable Net::Connector::Netdisco::Tftp::Archive::Failure`)
	if !reflect.DeepEqual(knownTypes, want) {
		t.Errorf("type vocabulary differs from frozen Ruby: got %d types, want %d", len(knownTypes), len(want))
	}
	for name := range want {
		t.Run(name, func(t *testing.T) {
			d := (Diagnostic{ErrorType: name, UnderlyingType: name}).safe()
			if d.ErrorType != name || d.UnderlyingType != name {
				t.Fatalf("safe type lost: %#v", d)
			}
		})
	}
	d := (Diagnostic{ErrorType: "private-marker", UnderlyingType: "private-marker", ErrorCode: "private-marker", Phase: "private-marker"}).safe()
	if d.ErrorType != "StandardError" || d.UnderlyingType != "StandardError" || d.ErrorCode != "" || d.Phase != "" {
		t.Fatalf("unsafe values: %#v", d)
	}
	if d := (Diagnostic{ErrorType: "AuthenticationError"}).safe(); d.ErrorType != "StandardError" {
		t.Fatalf("unqualified connector type accepted: %#v", d)
	}
}
func TestDiagnosticTrustedOperationAndCallbackCauses(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{{"invalid", connector.ErrInvalid, "ArgumentError"}, {"deadline", context.DeadlineExceeded, "Timeout::Error"}, {"eof", io.EOF, "EOFError"}, {"io", io.ErrShortWrite, "IOError"}, {"system", &os.PathError{Op: "private-marker", Path: "private-marker", Err: syscall.EACCES}, "Errno::EACCES"}, {"unknown", errors.New("private-marker"), "StandardError"}} {
		t.Run(tc.name, func(t *testing.T) {
			rules, e := NewRules(RuleOptions{}, vendors.Profile)
			if e != nil {
				t.Fatal(e)
			}
			p, e := NewPlan([]inventory.Device{{IP: "192.0.2.1", Vendor: "h3c"}}, rules, PlanOptions{Mode: "backup"})
			if e != nil {
				t.Fatal(e)
			}
			b, e := Run(t.Context(), p, FleetOptions{Run: func(context.Context, Task) (Artifact, error) { return Artifact{}, tc.err }, OnOutcome: func(context.Context, Outcome) error { return tc.err }})
			if e != nil {
				t.Fatal(e)
			}
			if b.Outcomes()[0].ErrorType != tc.want || b.CallbackErrors()[0].ErrorType != tc.want {
				t.Fatalf("classification lost: %#v %#v", b.Outcomes(), b.CallbackErrors())
			}
			r, e := NewReport(b, "strict")
			if e != nil {
				t.Fatal(e)
			}
			data, e := json.Marshal(r)
			if e != nil {
				t.Fatal(e)
			}
			if strings.Contains(string(data), "private-marker") {
				t.Fatal("cause text leaked")
			}
		})
	}
}
func TestDiagnosticStorageFailureRetainsSafeCause(t *testing.T) {
	_, err := storage.Write(t.Context(), filepath.Join(t.TempDir(), "missing", "report.json"), nil)
	var we *storage.WriteError
	if !errors.As(err, &we) {
		t.Fatalf("expected actual storage failure: %v", err)
	}
	d := diagnose(err, Artifact{})
	if d.ErrorType != "Net::Connector::Storage::AtomicFile::WriteError" || d.UnderlyingType != "Errno::ENOENT" || d.ArtifactState != "not_committed" || d.ArtifactPhase != "resolve_parent" {
		t.Fatalf("storage diagnostic lost: %#v", d)
	}
}
