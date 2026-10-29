package fleet

import (
	"context"
	"errors"
	"io"
	"strings"
	"syscall"

	"github.com/gatework/go-net-connector/archive"
	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/storage"
	"github.com/gatework/go-net-connector/transfer"
)

var knownTypes = words(`Net::Connector::Error Net::Connector::ConnectionError Net::Connector::AuthenticationError Net::Connector::LoginTimeout
Net::Connector::CommandTimeout Net::Connector::WriteTimeout Net::Connector::PromptError Net::Connector::ConnectionClosed
Net::Connector::TransportError Net::Connector::DeviceError Net::Connector::ConfigurationError Net::Connector::ScriptError
Net::Connector::InternalError Net::Connector::OutputLimitExceeded Net::Connector::ScriptOutputLimitExceeded Net::Connector::SessionBusy
Net::Connector::UnsupportedOperation Net::Connector::LogError Net::Connector::ParsingError Net::Connector::BackupBusy
Net::Connector::BackupPersistenceError Net::Connector::BackupFinalizeError Net::Connector::TftpCompletionError StandardError
RuntimeError ArgumentError TypeError IOError
EOFError SystemCallError ThreadError RangeError
KeyError IndexError NoMethodError NameError
FrozenError NotImplementedError Timeout::Error IO::TimeoutError
Errno::EIO Errno::EACCES Errno::EPERM Errno::ENOSPC
Errno::EROFS Errno::ENOENT Errno::ENOTDIR Errno::EISDIR
Errno::ELOOP Errno::EINVAL Errno::ENOSYS Errno::EMFILE
Errno::ENFILE Errno::ENOMEM Errno::EAGAIN Errno::ENOTSUP
Errno::EOPNOTSUPP Errno::ETIMEDOUT Errno::ECONNREFUSED Errno::ECONNRESET
Net::Connector::RemoteBackup::CompletionError Net::Connector::Storage::AtomicFile::WriteError Net::Connector::Storage::AtomicFile::PersistenceError Net::Connector::Storage::AtomicFile::DirectorySyncUnsupported
Net::Connector::Netdisco::Tftp::Archive::Unavailable Net::Connector::Netdisco::Tftp::Archive::Failure`)
var knownCodes = words(`error connection_error authentication_error login_timeout command_timeout write_timeout prompt_error connection_closed transport_error device_error configuration_error script_error internal_error output_limit_exceeded script_output_limit_exceeded session_busy unsupported_operation log_error parsing_error backup_busy backup_persistence_error backup_finalize_error tftp_completion_error authentication_failed connection_failed connection_refused no_route connection_reset connection_timeout host_key_changed host_key_untrusted known_hosts_busy known_hosts_unsafe known_hosts_inherited rsa_too_small unsupported_cipher ambiguous_description ambiguous_neighbor confirmation_required description_stages_unsupported description_unconfirmed description_unsupported incomplete_configuration interface_missing neighbor_discovery_unsupported parse_failed invalid_output_encoding persistence_unconfirmed stale_plan startup_config_missing template_invalid template_missing template_unreadable transfer_failed transfer_finalize_failed transfer_path_mismatch transfer_path_unconfirmed transfer_unconfirmed uncommitted_configuration unrecognized_output unsupported_configuration_format verification_plan_changed candidate_isolation_unavailable backup_durability_unsupported backup_finalize_failed backup_persistence_unconfirmed file_finalize_failed file_persistence_unconfirmed file_write_failed directory_sync_unsupported tftp_history_unavailable tftp_archive_failed`)
var knownPhases = words(`connect login enable command script read write close logging backup collect discover parse plan apply verify persist save interact tftp_backup remote_backup report callback`)

func words(s string) map[string]bool {
	m := map[string]bool{}
	for _, v := range strings.Fields(s) {
		m[v] = true
	}
	return m
}
func allowed(m map[string]bool, s string) string {
	if m[s] {
		return s
	}
	return ""
}

type Diagnostic struct{ ErrorCode, ErrorType, Phase, UnderlyingType, ArtifactState, ArtifactPhase, Verification string }

func (d Diagnostic) safe() Diagnostic {
	d.ErrorCode = allowed(knownCodes, d.ErrorCode)
	if d.ErrorType != "" && !knownTypes[d.ErrorType] {
		d.ErrorType = "StandardError"
	}
	d.Phase = allowed(knownPhases, d.Phase)
	if d.UnderlyingType != "" && !knownTypes[d.UnderlyingType] {
		d.UnderlyingType = "StandardError"
	}
	d.ArtifactState = allowed(words("not_committed committed durable reported_uploaded"), d.ArtifactState)
	d.ArtifactPhase = allowed(words("resolve_parent temporary_file write file_sync rename directory_open directory_sync cleanup complete path finalize"), d.ArtifactPhase)
	d.Verification = allowed(words("device_reported"), d.Verification)
	return d
}
func diagnose(e error, a Artifact) Diagnostic {
	if e == nil {
		return Diagnostic{}
	}
	e = primaryDiagnosticError(e)
	d := Diagnostic{ErrorType: trustedCauseType(e)}
	var ce *connector.Error
	if errors.As(e, &ce) {
		d.ErrorCode = ce.Code
		d.Phase = ce.Phase
		// Internal lifecycle names map to the established report vocabulary.
		// Keep the underlying error untouched for errors.Is/As callers.
		switch ce.Code {
		case "cleanup_failed":
			d.ErrorCode, d.Phase = "transport_error", "close"
		case "prepare_failed", "after_command_failed", "receipt_callback_failed", "step_callback_failed":
			d.ErrorCode, d.Phase = "script_error", "script"
		case "after_login_failed":
			d.ErrorCode, d.Phase = "connection_error", "login"
		}
		d.ErrorType = "Net::Connector::Error"
		if ce.Unwrap() != nil {
			d.UnderlyingType = trustedCauseType(ce.Unwrap())
		}
	}
	if ce != nil {
		if kind := connectorKinds[d.ErrorCode]; kind != "" {
			d.ErrorType = "Net::Connector::" + kind
		}
	} else {
		switch {
		case errors.Is(e, connector.ErrAuthentication):
			d.ErrorCode, d.ErrorType = "authentication_error", "Net::Connector::AuthenticationError"
		case errors.Is(e, connector.ErrDevice):
			d.ErrorCode, d.ErrorType = "device_error", "Net::Connector::DeviceError"
		case errors.Is(e, connector.ErrConnection):
			d.ErrorCode, d.ErrorType = "connection_error", "Net::Connector::ConnectionError"
		case errors.Is(e, connector.ErrBusy):
			d.ErrorCode, d.ErrorType = "session_busy", "Net::Connector::SessionBusy"
		case errors.Is(e, connector.ErrOutputLimit):
			d.ErrorCode, d.ErrorType = "output_limit_exceeded", "Net::Connector::OutputLimitExceeded"
		case errors.Is(e, connector.ErrScriptLimit):
			d.ErrorCode, d.ErrorType = "script_output_limit_exceeded", "Net::Connector::ScriptOutputLimitExceeded"
		case errors.Is(e, connector.ErrUnsupported):
			d.ErrorCode, d.ErrorType = "unsupported_operation", "Net::Connector::UnsupportedOperation"
		case errors.Is(e, connector.ErrIncomplete):
			d.ErrorCode, d.ErrorType, d.Phase = "incomplete_configuration", "Net::Connector::DeviceError", "collect"
		case errors.Is(e, connector.ErrInvalid):
			d.ErrorType = "ArgumentError"
		}
	}
	var se *storage.WriteError
	if errors.As(e, &se) {
		if ce == nil {
			d.ErrorCode, d.ErrorType = "file_write_failed", "Net::Connector::Storage::AtomicFile::WriteError"
			if se.Receipt.Committed() {
				d.ErrorCode, d.ErrorType = "file_persistence_unconfirmed", "Net::Connector::Storage::AtomicFile::PersistenceError"
			}
			if se.Receipt.Durable() {
				d.ErrorCode = "file_finalize_failed"
			}
			if se.Receipt.Phase == "directory_sync" && (errors.Is(se.Cause, syscall.EINVAL) || errors.Is(se.Cause, syscall.ENOSYS) || errors.Is(se.Cause, syscall.ENOTSUP) || errors.Is(se.Cause, syscall.EOPNOTSUPP)) {
				d.ErrorCode, d.ErrorType = "directory_sync_unsupported", "Net::Connector::Storage::AtomicFile::DirectorySyncUnsupported"
			}
		}
		d.ArtifactState = string(se.Receipt.State)
		d.ArtifactPhase = se.Receipt.Phase
		d.UnderlyingType = trustedCauseType(se.Cause)
	}
	var persistence *connector.BackupPersistenceError
	if errors.As(e, &persistence) {
		d.ErrorType = "Net::Connector::BackupPersistenceError"
	}
	var te *transfer.Error
	if errors.As(e, &te) {
		d.ErrorCode, d.Phase, d.ErrorType = te.Code, te.Phase, "Net::Connector::DeviceError"
		if te.Unwrap() != nil {
			d.UnderlyingType = trustedCauseType(te.Unwrap())
		}
		if a.Transfer != nil && a.Transfer.Reported() {
			d.ErrorType = "Net::Connector::TftpCompletionError"
			if te.Phase == "remote_backup" {
				d.ErrorType = "Net::Connector::RemoteBackup::CompletionError"
			}
		}
	} else if errors.Is(e, archive.ErrArchive) && ce == nil {
		d.ErrorCode, d.Phase, d.ErrorType = "tftp_archive_failed", "tftp_backup", "Net::Connector::Netdisco::Tftp::Archive::Failure"
	} else if errors.Is(e, transfer.ErrInvalid) {
		d.ErrorType = "ArgumentError"
	} else if errors.Is(e, transfer.ErrUnsupported) {
		d.ErrorCode, d.ErrorType = "unsupported_operation", "Net::Connector::UnsupportedOperation"
	}
	if a.Transfer != nil && a.Transfer.Reported() {
		d.ArtifactState = "reported_uploaded"
		d.Verification = "device_reported"
	}
	return d.safe()
}

// A later cleanup error must not replace the operation's classification just
// because it carries a different metadata type. Retain the first metadata
// carrier along the primary branch; callers still receive the full error tree.
func primaryDiagnosticError(err error) error {
	for err != nil {
		switch err.(type) {
		case *connector.Error, *connector.BackupPersistenceError, *storage.WriteError, *transfer.Error:
			return err
		}
		switch wrapped := err.(type) {
		case interface{ Unwrap() []error }:
			causes := wrapped.Unwrap()
			if len(causes) == 0 {
				return err
			}
			err = causes[0]
		case interface{ Unwrap() error }:
			cause := wrapped.Unwrap()
			if cause == nil {
				return err
			}
			err = cause
		default:
			return err
		}
	}
	return nil
}

// Only connector-owned codes select business types; arbitrary cause text never does.
var connectorKinds = map[string]string{
	"script_error":      "ScriptError",
	"connection_failed": "ConnectionError", "connection_error": "ConnectionError", "authentication_error": "AuthenticationError", "device_error": "DeviceError",
	"login_timeout": "LoginTimeout", "command_timeout": "CommandTimeout", "write_timeout": "WriteTimeout", "prompt_error": "PromptError", "connection_closed": "ConnectionClosed", "transport_error": "TransportError",
	"log_error": "LogError", "output_limit_exceeded": "OutputLimitExceeded", "script_output_limit_exceeded": "ScriptOutputLimitExceeded", "incomplete_configuration": "DeviceError", "configuration_error": "ConfigurationError", "invalid_output_encoding": "ParsingError",
	"backup_persistence_unconfirmed": "BackupPersistenceError", "backup_durability_unsupported": "BackupPersistenceError", "backup_finalize_failed": "BackupFinalizeError",
}

// trustedCauseType maps concrete Go causes to the frozen metadata vocabulary.
// It never reads error messages or accepts a callback's self-reported type name.
func trustedCauseType(err error) string {
	err = primaryDiagnosticError(err)
	var errno syscall.Errno
	if errors.As(err, &errno) {
		for _, entry := range []struct {
			code syscall.Errno
			name string
		}{
			{syscall.EIO, "Errno::EIO"},
			{syscall.EACCES, "Errno::EACCES"},
			{syscall.EPERM, "Errno::EPERM"},
			{syscall.ENOSPC, "Errno::ENOSPC"},
			{syscall.EROFS, "Errno::EROFS"},
			{syscall.ENOENT, "Errno::ENOENT"},
			{syscall.ENOTDIR, "Errno::ENOTDIR"},
			{syscall.EISDIR, "Errno::EISDIR"},
			{syscall.ELOOP, "Errno::ELOOP"},
			{syscall.EINVAL, "Errno::EINVAL"},
			{syscall.ENOSYS, "Errno::ENOSYS"},
			{syscall.EMFILE, "Errno::EMFILE"},
			{syscall.ENFILE, "Errno::ENFILE"},
			{syscall.ENOMEM, "Errno::ENOMEM"},
			{syscall.EAGAIN, "Errno::EAGAIN"},
			{syscall.ENOTSUP, "Errno::ENOTSUP"},
			{syscall.EOPNOTSUPP, "Errno::EOPNOTSUPP"},
			{syscall.ETIMEDOUT, "Errno::ETIMEDOUT"},
			{syscall.ECONNREFUSED, "Errno::ECONNREFUSED"},
			{syscall.ECONNRESET, "Errno::ECONNRESET"},
		} {
			if errno == entry.code {
				return entry.name
			}
		}
		return "SystemCallError"
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "Timeout::Error"
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return "EOFError"
	case errors.Is(err, io.ErrShortWrite), errors.Is(err, io.ErrClosedPipe), errors.Is(err, io.ErrNoProgress):
		return "IOError"
	default:
		return "StandardError"
	}
}
func (d Diagnostic) summary() map[string]any {
	d = d.safe()
	return map[string]any{"phase": optional(d.Phase), "underlying_type": optional(d.UnderlyingType), "artifact_state": optional(d.ArtifactState), "artifact_phase": optional(d.ArtifactPhase), "verification": optional(d.Verification)}
}
