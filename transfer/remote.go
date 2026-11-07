package transfer

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/vendors/dialect"
)

// RemoteOptions describes a remote configuration destination. Password is used
// only by the platform's scoped interaction and is never included in receipts.
type RemoteOptions struct{ Server, Path, Username, Protocol, Password string }

func (o RemoteOptions) dialectOptions() dialect.RemoteBackupOptions {
	return dialect.RemoteBackupOptions{Server: o.Server, Path: o.Path, Username: o.Username, Protocol: o.Protocol}
}

func ValidateRemote(o RemoteOptions) error {
	if o.Protocol != "" && o.Protocol != "ftp" && o.Protocol != "scp" || ValidateServer(o.Server) != nil || ValidatePath(o.Path) != nil || !vrfPattern.MatchString(o.Username) || strings.ContainsAny(o.Password, "\r\n\x00") {
		return ErrInvalid
	}
	return nil
}

// RemoteBackup runs a platform-provided remote export capability. The receipt
// establishes device-reported completion only.
func RemoteBackup(ctx context.Context, registry dialect.Registry, d *connector.Device, o RemoteOptions) (result Result, err error) {
	if registry == nil || len(registry.Names()) == 0 || d == nil {
		return result, ErrInvalid
	}
	declaration, err := registry.Lookup(d.ProfileName())
	if err != nil {
		return result, ErrUnsupported
	}
	policy, ok := declaration.(RemoteBackupPolicy)
	if !ok {
		return result, ErrUnsupported
	}
	if err = ValidateRemote(o); err != nil {
		return result, err
	}
	if o.Protocol == "" {
		o.Protocol = "scp"
	}
	options := o.dialectOptions()
	destination := policy.Destination(options)
	command := policy.Command(options, o.Password)
	err = d.WithOperation(ctx, func(ctx context.Context, op *connector.Operation) error {
		r, e := op.ExecuteCommand(ctx, command, connector.ExecuteOptions{SkipPrivilege: true})
		result.Steps = r.Steps()
		lines, failureDetected, evidenceErr := inspectTransferEvidence(result.Steps, remoteRejected)
		if evidenceErr != nil {
			if errors.Is(evidenceErr, errEvidenceLimit) {
				return &Error{Code: "evidence_limit_exceeded", Phase: "remote_backup", cause: errors.Join(evidenceErr, e)}
			}
			return errors.Join(evidenceErr, e)
		}
		if failureDetected {
			return &Error{Code: "transfer_failed", Phase: "remote_backup", cause: errors.Join(connector.ErrDevice, e)}
		}
		if !policy.Completed(lines, destination) {
			if e != nil {
				return e
			}
			return &Error{Code: "transfer_unconfirmed", Phase: "remote_backup", cause: ErrUnconfirmed}
		}
		result.Receipt = Receipt{Server: o.Server, Path: o.Path, Protocol: o.Protocol, Username: o.Username, ConfigurationKind: "running", Verification: "device_reported", CompletedAt: time.Now().UTC()}
		if e != nil {
			return &Error{Code: "transfer_finalize_failed", Phase: "remote_backup", cause: e}
		}
		return nil
	})
	return result, err
}

var pattern = regexp.MustCompile(`(?i)(?:^|[\r\n])[ \t]*(?:error:|(?:ftp|scp|ssh):[ \t]+|permission denied\b|lost connection\b)`)

func remoteRejected(command connector.Command, prompt, output string) bool {
	return matchesEvidencePattern(command, prompt, output, pattern)
}

// MarshalJSON exposes operational metadata only; credentials remain available explicitly on the options.
func (o RemoteOptions) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct{ Server, Path, Username, Protocol string }{o.Server, o.Path, o.Username, o.Protocol})
}
func (o RemoteOptions) String() string   { b, _ := o.MarshalJSON(); return "RemoteOptions" + string(b) }
func (o RemoteOptions) GoString() string { return o.String() }
