package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gatework/go-net-connector/archive"
	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/fleet"
	"github.com/gatework/go-net-connector/internal/batchreport"
	"github.com/gatework/go-net-connector/storage"
	"github.com/gatework/go-net-connector/transfer"
	"github.com/gatework/go-net-connector/vendors"
	"github.com/gatework/go-net-connector/vendors/dialect"
)

// SaveStage persists the running configuration. Success requires the
// platform's positive save confirmation; a prompt alone is unconfirmed.
type SaveStage struct{}

// Name returns "save".
func (SaveStage) Name() string { return "save" }

// Admit skips platforms that declare no save commands.
func (SaveStage) Admit(_ fleet.Record, platform dialect.Platform) string {
	if len(platform.Terminal().SaveCommands) == 0 {
		return "save_unsupported"
	}
	return ""
}

// Run saves the running configuration and requires the platform's
// confirmation.
func (SaveStage) Run(ctx context.Context, s *Session, e *Evidence) error {
	device, err := s.Device(ctx)
	if err != nil {
		return err
	}
	_, err = device.SaveConfig(ctx, connector.ExecuteOptions{})
	e.Artifacts.Save = &batchreport.Save{Confirmed: err == nil}
	return err
}

// BackupStage collects the running configuration into "<name>-<IP>.txt".
type BackupStage struct{ Directory string }

// Name returns "backup".
func (BackupStage) Name() string { return "backup" }

// Admit skips platforms that declare no configuration commands.
func (BackupStage) Admit(_ fleet.Record, platform dialect.Platform) string {
	if len(platform.Terminal().ConfigCommands) == 0 {
		return "unsupported_operation"
	}
	return ""
}

// Plan records the backup file name in target.
func (b BackupStage) Plan(task fleet.Task, _ dialect.Platform, target *batchreport.Target) {
	target.File, _ = task.Device.BackupFilename("hostname_ip")
}

// Run collects the configuration and writes the backup, recording its receipt
// in e.
func (b BackupStage) Run(ctx context.Context, s *Session, e *Evidence) error {
	name, err := s.Record.BackupFilename("hostname_ip")
	if err != nil {
		return err
	}
	path := filepath.Join(b.Directory, name)
	if info, statErr := os.Lstat(path); statErr == nil && !info.Mode().IsRegular() {
		return storage.ErrUnsafe
	} else if statErr != nil && !os.IsNotExist(statErr) {
		return statErr
	}
	device, err := s.Device(ctx)
	if err != nil {
		return err
	}
	receipt, err := device.Backup(ctx, connector.BackupOptions{TextOnly: true, PreserveInvalidEncoding: true, Path: path, RejectSymlink: true, LockTimeout: 5 * time.Second})
	if receipt.Path != "" {
		e.Artifacts.Backup = reportBackup(receipt)
	}
	if receipt.HasSavedEvidence() {
		e.MarkPartial()
		if err == nil && receipt.HasUnverifiedPair() {
			err = connector.ErrIncomplete
		}
	}
	return err
}

// TFTPStage makes the device upload its configuration file. With a
// verification root the upload runs inside an archive transaction that marks
// the server clock, verifies the uploaded file and retrieves it.
type TFTPStage struct {
	Registry *vendors.Registry
	Pools    map[string]*archive.Pool
}

// Name returns "tftp".
func (TFTPStage) Name() string { return "tftp" }

// Admit skips platforms without a TFTP policy.
func (TFTPStage) Admit(_ fleet.Record, platform dialect.Platform) string {
	if _, ok := platform.(transfer.TFTPPolicy); !ok {
		return "unsupported_operation"
	}
	return ""
}

// Plan records the TFTP server, file and source settings in target.
func (TFTPStage) Plan(task fleet.Task, _ dialect.Platform, target *batchreport.Target) {
	t := task.Target
	target.File, target.Server, target.VerificationRoot = t.Path, t.Server, t.Root
	target.SourceFile, target.SourceInterface, target.VRF = t.SourceFile, t.SourceInterface, t.VRF
}

// Run makes the device upload its configuration, inside an archive
// transaction when a verification root is set.
func (t TFTPStage) Run(ctx context.Context, s *Session, e *Evidence) error {
	target := s.Target
	upload := func(ctx context.Context, name string) (transfer.Receipt, error) {
		device, err := s.Device(ctx)
		if err != nil {
			return transfer.Receipt{}, err
		}
		opts := target.TFTPOptions
		opts.Path = name
		s.Stage("upload")
		result, err := transfer.TFTP(ctx, t.Registry, device, opts)
		if result.Receipt.Reported() {
			s.Stage("archive")
		}
		return result.Receipt, err
	}
	var receipt transfer.Receipt
	var err error
	var archived *archive.Receipt
	if pool := t.Pools[target.Root]; pool != nil {
		name, nameErr := s.Record.BackupFilename("hostname_ip")
		if nameErr != nil {
			return nameErr
		}
		name = strings.TrimSuffix(name, ".txt") + filepath.Ext(target.Path)
		result, uploadErr := pool.Upload(ctx, target.Path, name, upload)
		receipt, err, archived = result.Transfer, uploadErr, &result.Receipt
		e.Artifacts.Archive = reportArchive(result.Receipt)
	} else {
		receipt, err = upload(ctx, target.Path)
	}
	if receipt.Reported() {
		e.Artifacts.Transfer = reportTransfer(receipt)
		e.MarkPartial()
		if archived != nil && !archived.Complete() && err == nil {
			err = connector.ErrIncomplete
		}
	}
	if archived != nil && archived.Verified() {
		e.Verified = true
		if e.Artifacts.Transfer != nil {
			e.Artifacts.Transfer.Verification = "server_verified"
		}
	}
	return err
}

// ExecuteStage sends one validated script per platform inside one operation
// lease. The first rejected command stops the script; nothing is retried.
type ExecuteStage struct{ Scripts DeployScripts }

// Name returns "execute".
func (ExecuteStage) Name() string { return "execute" }

// Admit skips devices whose platform has no script.
func (x ExecuteStage) Admit(record fleet.Record, _ dialect.Platform) string {
	if script, ok := x.Scripts.For(record.Vendor); !ok || len(script.Commands) == 0 {
		return "missing_script"
	}
	return ""
}

// Plan records the script name and command count in target.
func (x ExecuteStage) Plan(task fleet.Task, _ dialect.Platform, target *batchreport.Target) {
	script, _ := x.Scripts.For(task.Device.Vendor)
	target.Script, target.Commands = script.Name, len(script.Commands)
}

// Run sends the platform's script in one operation, stopping at the first
// rejected command.
func (x ExecuteStage) Run(ctx context.Context, s *Session, e *Evidence) error {
	script, _ := x.Scripts.For(s.Record.Vendor)
	device, err := s.Device(ctx)
	if err != nil {
		return err
	}
	evidence := &batchreport.Script{Name: script.Name, Commands: len(script.Commands), Attempts: []batchreport.Attempt{}}
	e.Artifacts.Script = evidence
	return device.WithOperation(ctx, func(ctx context.Context, op *connector.Operation) error {
		result, err := op.ExecuteScript(ctx, script, connector.ExecuteOptions{})
		evidence.Accepted = len(result.Steps())
		for _, attempt := range result.Attempts() {
			failed := attempt.Err() != nil
			evidence.Attempts = append(evidence.Attempts, batchreport.Attempt{Line: attempt.Command().Line, Written: attempt.Written(), Complete: attempt.Complete(), Failed: failed})
			if failed && evidence.FailedLine == 0 {
				evidence.FailedLine = attempt.Command().Line
			}
			if attempt.Written() > 0 {
				// Any written command may have changed the device.
				e.MarkPartial()
			}
		}
		if err != nil && errors.Is(err, connector.ErrDevice) {
			e.Outcome = "rejected"
		}
		return err
	})
}

// DeployScripts selects the script for each canonical platform key. A
// platform without its own script uses Default; with neither it is skipped.
type DeployScripts struct {
	Default    *connector.Script
	ByPlatform map[string]connector.Script
}

// For returns the script that applies to platform.
func (s DeployScripts) For(platform string) (connector.Script, bool) {
	if script, ok := s.ByPlatform[platform]; ok {
		return script, true
	}
	if s.Default != nil {
		return *s.Default, true
	}
	return connector.Script{}, false
}

func reportBackup(receipt connector.BackupReceipt) *batchreport.Backup {
	b := &batchreport.Backup{
		Path: receipt.Path, Bytes: int64(receipt.Bytes), SHA256: receipt.SHA256, Change: receipt.Change,
		PreviousSHA256: receipt.PreviousSHA256, Encoding: receipt.Encoding,
		File:  batchreport.FileReceipt{Path: receipt.File.Path, State: string(receipt.File.State), Phase: receipt.File.Phase},
		Saved: receipt.HasSavedEvidence(), Complete: receipt.Complete(),
	}
	if source := receipt.Source; source != nil {
		b.Source = &batchreport.BackupSource{
			Path: source.Path, Bytes: source.Bytes, SHA256: source.SHA256, NoWriteVerified: source.NoWriteVerified,
			File: batchreport.FileReceipt{Path: source.File.Path, State: string(source.File.State), Phase: source.File.Phase},
		}
		for _, segment := range source.Segments {
			b.Source.Segments = append(b.Source.Segments, batchreport.SourceSegment{Step: segment.Step, Offset: segment.Offset, Bytes: segment.Bytes})
		}
	}
	if warning := receipt.TextWarning; warning != nil {
		b.TextWarning = &batchreport.TextWarning{Count: warning.Count, Positions: []batchreport.TextPosition{}}
		for _, position := range warning.Positions {
			b.TextWarning.Positions = append(b.TextWarning.Positions, batchreport.TextPosition{Offset: position.Offset, Line: position.Line, Column: position.Column})
		}
	}
	return b
}

func reportTransfer(receipt transfer.Receipt) *batchreport.Transfer {
	return &batchreport.Transfer{
		RemotePath: receipt.Path, Server: receipt.Server, ConfigurationKind: receipt.ConfigurationKind,
		SourceFile: receipt.SourceFile, Format: receipt.Format, Verification: string(receipt.Verification),
	}
}

func reportArchive(receipt archive.Receipt) *batchreport.ArchiveReceipt {
	r := &batchreport.ArchiveReceipt{
		Local:   batchreport.FileReceipt{Path: receipt.Local.Path, State: string(receipt.Local.State), Phase: receipt.Local.Phase},
		Cleanup: string(receipt.Cleanup),
	}
	if proof := receipt.Server; proof != nil {
		r.Server = &batchreport.ArchiveProof{Bytes: proof.Bytes, SHA256: proof.SHA256, ArchivePath: proof.ArchivePath, Verified: proof.Verified}
	}
	return r
}

// ReadOnlyClassifier is the platform capability exec requires: it reports
// whether a command can only read device state.
type ReadOnlyClassifier interface {
	IsReadOnly(command string) bool
}

// ExecStage runs read-only commands in one lease and saves each strict
// output body (prompt and command echo removed) to "<name>-<IP>.txt".
type ExecStage struct {
	Commands  []string
	Directory string
}

// Name returns "exec".
func (ExecStage) Name() string { return "exec" }

// Admit refuses a platform unless every command is read-only for it, so no
// connection is opened for a command that could change the device.
func (x ExecStage) Admit(_ fleet.Record, platform dialect.Platform) string {
	classifier, ok := platform.(ReadOnlyClassifier)
	if !ok {
		return "unsupported_operation"
	}
	for _, command := range x.Commands {
		if !classifier.IsReadOnly(command) {
			return "not_read_only"
		}
	}
	return ""
}

// Plan records the output file name and command count in target.
func (x ExecStage) Plan(task fleet.Task, _ dialect.Platform, target *batchreport.Target) {
	target.File, _ = task.Device.BackupFilename("hostname_ip")
	target.Commands = len(x.Commands)
}

// Run runs the read-only commands and saves their output bodies.
func (x ExecStage) Run(ctx context.Context, s *Session, e *Evidence) error {
	name, err := s.Record.BackupFilename("hostname_ip")
	if err != nil {
		return err
	}
	device, err := s.Device(ctx)
	if err != nil {
		return err
	}
	path := filepath.Join(x.Directory, name)
	var body strings.Builder
	runErr := device.WithOperation(ctx, func(ctx context.Context, op *connector.Operation) error {
		for _, text := range x.Commands {
			result, err := op.ExecuteCommand(ctx, connector.Command{Text: text}, connector.ExecuteOptions{})
			if err != nil {
				return err
			}
			step, err := result.SingleAccepted(text)
			if err != nil {
				return err
			}
			output, err := step.Body()
			if err != nil {
				return err
			}
			sum := sha256.Sum256([]byte(output))
			e.Artifacts.Outputs = append(e.Artifacts.Outputs, batchreport.Output{Command: text, Path: path, Bytes: int64(len(output)), SHA256: hex.EncodeToString(sum[:])})
			fmt.Fprintf(&body, "### %s\n%s", text, output)
			if !strings.HasSuffix(output, "\n") {
				body.WriteString("\n")
			}
		}
		return nil
	})
	if body.Len() > 0 {
		if _, err := storage.Write(ctx, path, []byte(body.String())); err != nil {
			return errors.Join(runErr, err)
		}
		e.MarkPartial()
	}
	return runErr
}

// RemoteStage makes the device export its configuration to a remote server
// with its native client (SCP or FTP). Completion is device-reported only.
type RemoteStage struct {
	Registry dialect.Registry
	// Options carries server, protocol, username and password; Path is the
	// remote directory, completed per device with "<host>-<stamp>.conf".
	Options transfer.RemoteOptions
	Stamp   string
}

// Name returns "remote".
func (RemoteStage) Name() string { return "remote" }

// Admit skips platforms without a remote backup policy.
func (RemoteStage) Admit(_ fleet.Record, platform dialect.Platform) string {
	if _, ok := platform.(transfer.RemoteBackupPolicy); !ok {
		return "unsupported_operation"
	}
	return ""
}

// Plan records the remote server and path in target.
func (x RemoteStage) Plan(task fleet.Task, _ dialect.Platform, target *batchreport.Target) {
	target.Server, target.File = x.Options.Server, x.path(task.Device.Host)
}

// Run makes the device export its configuration to the remote server.
func (x RemoteStage) Run(ctx context.Context, s *Session, e *Evidence) error {
	device, err := s.Device(ctx)
	if err != nil {
		return err
	}
	options := x.Options
	options.Path = x.path(s.Record.Host)
	result, err := transfer.RemoteBackup(ctx, x.Registry, device, options)
	if result.Receipt.Reported() {
		e.Artifacts.Remote = &batchreport.Remote{Server: result.Receipt.Server, Destination: result.Receipt.Path, Protocol: result.Receipt.Protocol, Reported: true}
		e.MarkPartial()
	}
	return err
}

func (x RemoteStage) path(host string) string {
	name := strings.ReplaceAll(host, ":", "_") + "-" + x.Stamp + ".conf"
	if x.Options.Path == "" {
		return name
	}
	return strings.TrimSuffix(x.Options.Path, "/") + "/" + name
}
