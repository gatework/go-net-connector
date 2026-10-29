package transfer

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/gatework/go-net-connector/connector"
)

// RemoteOptions describes a Junos FTP/SCP destination. Password is used only for
// its scoped interaction and is never included in the command or receipt.
type RemoteOptions struct{ Server, Path, Username, Protocol, Password string }

func ValidateRemote(o RemoteOptions) error {
	if o.Protocol != "" && o.Protocol != "ftp" && o.Protocol != "scp" || ValidateServer(o.Server) != nil || ValidatePath(o.Path) != nil || !vrfPattern.MatchString(o.Username) || strings.ContainsAny(o.Password, "\r\n\x00") {
		return ErrInvalid
	}
	return nil
}

// RemoteBackup exports committed Junos configuration without changing candidate
// configuration. The receipt establishes device-reported completion only.
func RemoteBackup(ctx context.Context, d *connector.Device, o RemoteOptions) (result Result, err error) {
	if d == nil {
		return result, ErrInvalid
	}
	if d.ProfileName() != "juniper" {
		return result, ErrUnsupported
	}
	if err = ValidateRemote(o); err != nil {
		return result, err
	}
	if o.Protocol == "" {
		o.Protocol = "scp"
	}
	destination := o.Protocol + "://" + o.Username + "@" + o.Server + "/" + o.Path
	timeout := 180 * time.Second
	command := connector.Command{Text: "show configuration | save " + destination, Timeout: &timeout, OutputSensitive: true}
	if o.Password != "" {
		command.Interactions = []connector.Interaction{{Pattern: regexp.MustCompile(`(?i)(?:password(?: for [^\r\n:]*)?):\s*$`), Reply: o.Password + "\n", Sensitive: true, Limit: 1}}
	}
	err = d.WithOperation(ctx, func(ctx context.Context, op *connector.Operation) error {
		r, e := op.ExecuteCommand(ctx, command, connector.ExecuteOptions{SkipPrivilege: true})
		result.Steps = r.Steps()
		lines, outputs, parseErr := evidence(result.Steps)
		if parseErr != nil {
			return parseErr
		}
		rejected := regexp.MustCompile(`(?mi)^[ \t]*(?:error:|(?:ftp|scp|ssh):\s|permission denied\b|lost connection\b)`)
		for _, output := range outputs {
			if rejected.MatchString(output) {
				return &Error{Code: "transfer_failed", Phase: "remote_backup", cause: connector.ErrDevice}
			}
		}
		success := regexp.MustCompile(`^Wrote [1-9][0-9]* lines of output to '` + regexp.QuoteMeta(destination) + `'$`)
		confirmed := false
		for _, line := range lines {
			confirmed = confirmed || success.MatchString(strings.TrimSpace(line))
		}
		if !confirmed {
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

// MarshalJSON exposes operational metadata only; credentials remain available explicitly on the options.
func (o RemoteOptions) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct{ Server, Path, Username, Protocol string }{o.Server, o.Path, o.Username, o.Protocol})
}
func (o RemoteOptions) String() string   { b, _ := o.MarshalJSON(); return "RemoteOptions" + string(b) }
func (o RemoteOptions) GoString() string { return o.String() }
