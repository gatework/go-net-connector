package transfer

import (
	"context"
	"errors"
	"fmt"
	"path"
	"time"

	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/vendors/dialect"
)

var ErrInvalid = errors.New("transfer: invalid options")
var ErrUnsupported = errors.New("transfer: unsupported operation")
var ErrUnconfirmed = errors.New("transfer: completion unconfirmed")
var ErrSourceFileRequired = errors.New("transfer: source file required")

const DefaultUploadTimeout = 180 * time.Second

type Error struct {
	Code, Phase string
	cause       error
}

func (e *Error) Error() string    { return "transfer: " + e.Code + " (" + e.Phase + ")" }
func (e *Error) Unwrap() error    { return e.cause }
func (e *Error) GoString() string { return e.Error() }
func newError(code string, cause error) error {
	return &Error{Code: code, Phase: "tftp_backup", cause: cause}
}

type Result struct {
	Receipt Receipt
	Steps   []connector.StepResult
}

func (r Result) String() string {
	return fmt.Sprintf("transfer.Result{steps:%d,reported:%t}", len(r.Steps), r.Receipt.Reported())
}
func (r Result) GoString() string { return r.String() }

// TFTP runs the common transfer workflow through the resolved platform's
// command, interaction and completion policy.
func TFTP(ctx context.Context, registry dialect.Registry, d *connector.Device, options TFTPOptions) (result Result, err error) {
	if d == nil || registry == nil {
		return result, ErrInvalid
	}
	vendor := d.PlatformName()
	platform, err := registry.Lookup(vendor)
	if err != nil {
		return result, ErrUnsupported
	}
	policy, ok := platform.(TFTPPolicy)
	if !ok {
		return result, ErrUnsupported
	}
	if err = Validate(registry, vendor, d.Host(), options); err != nil {
		return result, err
	}
	explicitPath := options.Path != ""
	explicitSource := options.SourceFile != ""
	if options.Timeout == 0 {
		options.Timeout = DefaultUploadTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, options.Timeout)
	defer cancel()
	t := &tftpRun{registry: registry, policy: policy, device: d, vendor: vendor, options: options, explicitPath: explicitPath, explicitSource: explicitSource, result: &result}
	err = d.WithOperation(ctx, t.run)
	return result, err
}

// tftpRun is one TFTP transfer inside a device operation.
type tftpRun struct {
	registry       dialect.Registry
	policy         TFTPPolicy
	device         *connector.Device
	vendor         string
	options        TFTPOptions
	explicitPath   bool
	explicitSource bool
	result         *Result
}

// run resolves the source file and destination path, runs the transfer
// command and confirms its completion from the device evidence.
func (t *tftpRun) run(ctx context.Context, op *connector.Operation) error {
	if e := t.resolveSource(ctx, op); e != nil {
		return e
	}
	if e := t.resolvePath(); e != nil {
		return e
	}
	command := t.policy.Command(t.device.Host(), t.options, t.explicitPath)
	r, executeErr := op.ExecuteCommand(ctx, command, connector.ExecuteOptions{SkipPrivilege: true})
	t.result.Steps = append(t.result.Steps, r.Steps()...)
	actualPath, e := t.confirm(r.Steps(), executeErr)
	if e != nil {
		return e
	}
	metadata := t.policy.Receipt(t.options, t.explicitSource, t.explicitPath)
	t.result.Receipt = Receipt{ConfigurationKind: metadata.ConfigurationKind, Format: metadata.Format, Server: t.options.Server, Verification: VerificationDeviceReported, SourceFile: metadata.SourceFile, Path: actualPath, CompletedAt: time.Now().UTC()}
	if metadata.IncludeRequestedPath {
		requested := t.options.Path
		t.result.Receipt.RequestedPath = &requested
	}
	if t.explicitPath && actualPath != t.options.Path {
		return newError("transfer_path_mismatch", ErrUnconfirmed)
	}
	if executeErr != nil {
		return newError("transfer_finalize_failed", executeErr)
	}
	return nil
}

// resolveSource asks the device for its startup file when the platform
// needs one and none was given.
func (t *tftpRun) resolveSource(ctx context.Context, op *connector.Operation) error {
	command := t.policy.SourceFileCommand()
	if command == "" || t.options.SourceFile != "" {
		return nil
	}
	r, executeErr := op.ExecuteCommand(ctx, connector.Command{Text: command}, connector.ExecuteOptions{})
	t.result.Steps = append(t.result.Steps, r.Steps()...)
	if executeErr != nil {
		return executeErr
	}
	output, outputErr := r.JoinedOutput()
	if outputErr != nil {
		return outputErr
	}
	t.options.SourceFile, outputErr = t.policy.ResolveSourceFile(output)
	if outputErr != nil || ValidateSource(t.options.SourceFile) != nil {
		return newError("startup_config_missing", errors.Join(ErrUnconfirmed, outputErr))
	}
	return nil
}

// resolvePath derives the remote file name from the source file or the
// platform's naming rule unless one was given.
func (t *tftpRun) resolvePath() error {
	if !t.explicitPath {
		if t.options.SourceFile != "" {
			t.options.Path = path.Base(t.options.SourceFile)
		} else {
			var e error
			if t.options.Path, e = Filename(t.registry, t.vendor, t.device.Host(), ""); e != nil {
				return e
			}
		}
	}
	if ValidatePath(t.options.Path) != nil {
		return ErrInvalid
	}
	return nil
}

// confirm inspects the transfer output for failure and completion and
// returns the path the device reports.
func (t *tftpRun) confirm(steps []connector.StepResult, executeErr error) (string, error) {
	lines, failureDetected, evidenceErr := inspectTransferEvidence(steps, failed)
	if evidenceErr != nil {
		if errors.Is(evidenceErr, errEvidenceLimit) {
			return "", newError("evidence_limit_exceeded", errors.Join(evidenceErr, executeErr))
		}
		return "", errors.Join(evidenceErr, executeErr)
	}
	if failureDetected {
		return "", newError("transfer_failed", errors.Join(connector.ErrDevice, executeErr))
	}
	actualPath, complete, completionErr := t.policy.Completed(lines, t.options)
	if completionErr != nil {
		return "", newError("transfer_path_unconfirmed", errors.Join(ErrUnconfirmed, completionErr, executeErr))
	}
	if !complete {
		if executeErr != nil {
			return "", executeErr
		}
		return "", newError("transfer_unconfirmed", ErrUnconfirmed)
	}
	if actualPath == "" {
		actualPath = t.options.Path
	}
	if ValidatePath(actualPath) != nil {
		return "", newError("transfer_path_unconfirmed", errors.Join(ErrInvalid, executeErr))
	}
	return actualPath, nil
}
