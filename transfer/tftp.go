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
	err = d.WithOperation(ctx, func(ctx context.Context, op *connector.Operation) error {
		if command := policy.SourceFileCommand(); command != "" && options.SourceFile == "" {
			r, executeErr := op.ExecuteCommand(ctx, connector.Command{Text: command}, connector.ExecuteOptions{})
			result.Steps = append(result.Steps, r.Steps()...)
			if executeErr != nil {
				return executeErr
			}
			output, outputErr := r.JoinedOutput()
			if outputErr != nil {
				return outputErr
			}
			options.SourceFile, outputErr = policy.ResolveSourceFile(output)
			if outputErr != nil || ValidateSource(options.SourceFile) != nil {
				return newError("startup_config_missing", errors.Join(ErrUnconfirmed, outputErr))
			}
		}
		if !explicitPath {
			if options.SourceFile != "" {
				options.Path = path.Base(options.SourceFile)
			} else {
				var filenameErr error
				options.Path, filenameErr = Filename(registry, vendor, d.Host(), "")
				if filenameErr != nil {
					return filenameErr
				}
			}
		}
		if ValidatePath(options.Path) != nil {
			return ErrInvalid
		}
		command := policy.Command(d.Host(), options, explicitPath)
		r, executeErr := op.ExecuteCommand(ctx, command, connector.ExecuteOptions{SkipPrivilege: true})
		result.Steps = append(result.Steps, r.Steps()...)
		lines, failureDetected, evidenceErr := inspectTransferEvidence(r.Steps(), failed)
		if evidenceErr != nil {
			if errors.Is(evidenceErr, errEvidenceLimit) {
				return newError("evidence_limit_exceeded", errors.Join(evidenceErr, executeErr))
			}
			return errors.Join(evidenceErr, executeErr)
		}
		if failureDetected {
			return newError("transfer_failed", errors.Join(connector.ErrDevice, executeErr))
		}
		actualPath, complete, completionErr := policy.Completed(lines, options)
		if completionErr != nil {
			return newError("transfer_path_unconfirmed", errors.Join(ErrUnconfirmed, completionErr, executeErr))
		}
		if !complete {
			if executeErr != nil {
				return executeErr
			}
			return newError("transfer_unconfirmed", ErrUnconfirmed)
		}
		if actualPath == "" {
			actualPath = options.Path
		}
		if ValidatePath(actualPath) != nil {
			return newError("transfer_path_unconfirmed", errors.Join(ErrInvalid, executeErr))
		}
		metadata := policy.Receipt(options, explicitSource, explicitPath)
		result.Receipt = Receipt{ConfigurationKind: metadata.ConfigurationKind, Format: metadata.Format, Server: options.Server, Verification: VerificationDeviceReported, SourceFile: metadata.SourceFile, Path: actualPath, CompletedAt: time.Now().UTC()}
		if metadata.IncludeRequestedPath {
			requested := options.Path
			result.Receipt.RequestedPath = &requested
		}
		if explicitPath && actualPath != options.Path {
			return newError("transfer_path_mismatch", ErrUnconfirmed)
		}
		if executeErr != nil {
			return newError("transfer_finalize_failed", executeErr)
		}
		return nil
	})
	return result, err
}
