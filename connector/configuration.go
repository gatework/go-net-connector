package connector

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/gatework/go-net-connector/internal/terminal"
	"github.com/gatework/go-net-connector/storage"
)

type renderedConfig struct{}

func (renderedConfig) Prompt(_ string, current string) string                        { return current }
func (renderedConfig) Validate(context.Context, Command, StepResult, Executor) error { return nil }
func (renderedConfig) Finish(r Result) (string, error) {
	if len(r.steps) == 0 {
		return "", ErrIncomplete
	}
	return terminal.Render([]byte(r.steps[len(r.steps)-1].output), true)
}
func (d *Device) RunningConfig(ctx context.Context) (Result, error) {
	if e := d.acquire(); e != nil {
		return Result{}, e
	}
	defer d.release()
	ctx, cancel := d.operationContext(ctx)
	defer cancel()
	return d.runningConfig(ctx)
}
func (d *Device) runningConfig(ctx context.Context) (result Result, err error) {
	returned := false
	defer func() {
		if !returned {
			_ = d.closeResources()
		}
	}()
	result, err = d.collectConfig(ctx)
	returned = true
	return
}
func (d *Device) collectConfig(ctx context.Context) (result Result, err error) {
	if len(d.profile.ConfigCommands) == 0 {
		return Result{}, fail("configuration_error", "collect", Command{}, ErrUnsupported)
	}
	defer func() {
		if err != nil {
			err = joinCleanup(err, d.closeResources())
		}
	}()
	commands := make([]Command, len(d.profile.ConfigCommands))
	for i, text := range d.profile.ConfigCommands {
		commands[i] = Command{Text: text, OutputSensitive: true}
	}
	var strategy ConfigStrategy = renderedConfig{}
	if d.profile.ConfigStrategy != nil {
		strategy = d.profile.ConfigStrategy()
		if strategy == nil {
			return Result{}, fail("configuration_error", "collect", Command{}, ErrInvalid)
		}
	}
	result, e := d.execute(ctx, Script{Commands: commands}, ExecuteOptions{}, strategy)
	if e != nil {
		return result, e
	}
	config, e := strategy.Finish(result)
	if e != nil {
		return result, configurationFailure(Command{}, e)
	}
	if strings.TrimSpace(config) == "" {
		return result, ErrIncomplete
	}
	hasBody := d.profile.ConfigStrategy != nil
	for _, step := range result.steps[len(result.steps)-1:] {
		body, e := terminal.Render([]byte(step.output), true)
		if e != nil {
			return result, e
		}
		prompt, _ := terminal.Render([]byte(step.prompt), true)
		body = strings.TrimSuffix(strings.TrimSpace(body), strings.TrimSpace(prompt))
		body = strings.TrimSpace(body)
		if strings.HasPrefix(body, step.command.Text) {
			body = strings.TrimSpace(strings.TrimPrefix(body, step.command.Text))
		}
		if body != "" {
			hasBody = true
		}
	}
	if !hasBody {
		return result, fail("incomplete_configuration", "collect", Command{}, ErrIncomplete)
	}
	result.config = &config
	return result, nil
}

// SaveConfig distinguishes command completion from profile-confirmed persistence.
func (d *Device) SaveConfig(ctx context.Context, opts ExecuteOptions) (Result, error) {
	if len(d.profile.SaveCommands) == 0 {
		return Result{}, ErrUnsupported
	}
	if opts.MaxOutputBytes < 0 {
		return Result{}, ErrInvalid
	}
	if err := d.acquire(); err != nil {
		return Result{}, err
	}
	defer d.release()
	ctx, cancel := d.operationContext(ctx)
	defer cancel()
	return d.saveConfig(ctx, opts)
}
func (d *Device) saveConfig(ctx context.Context, opts ExecuteOptions) (result Result, err error) {
	if len(d.profile.SaveCommands) == 0 {
		return result, ErrUnsupported
	}
	if opts.MaxOutputBytes < 0 {
		return result, ErrInvalid
	}
	returned := false
	defer func() {
		if !returned {
			_ = d.closeResources()
		} else if err != nil {
			err = joinCleanup(err, d.closeResources())
		}
	}()
	result, err = d.save(ctx, opts)
	returned = true
	return
}
func (d *Device) save(ctx context.Context, opts ExecuteOptions) (result Result, err error) {
	commands := make([]Command, len(d.profile.SaveCommands))
	for i, text := range d.profile.SaveCommands {
		commands[i] = Command{Text: text}
	}
	result, err = d.execute(ctx, Script{Commands: commands}, opts, nil)
	if err != nil {
		return
	}
	if d.profile.ValidateSave == nil {
		return result, fail("persistence_unconfirmed", "persist", Command{}, ErrUnconfirmed)
	}
	if err = d.profile.ValidateSave(ctx, result); err != nil {
		return result, fail("persistence_unconfirmed", "persist", Command{}, err)
	}
	return result, ctx.Err()
}

type BackupOptions struct {
	Path          string
	LockTimeout   time.Duration
	RejectSymlink bool
}
type BackupReceipt struct {
	Path            string          `json:"path"`
	Bytes           int             `json:"bytes"`
	SHA256          string          `json:"sha256"`
	CollectedAt     time.Time       `json:"collected_at"`
	Change          string          `json:"change"`
	PreviousSHA256  string          `json:"previous_sha256,omitempty"`
	DeviceCompleted bool            `json:"device_completed"`
	NoWriteVerified bool            `json:"no_write_verified"`
	File            storage.Receipt `json:"file"`
}

func (d *Device) Backup(ctx context.Context, opts BackupOptions) (receipt BackupReceipt, err error) {
	if e := d.acquire(); e != nil {
		return receipt, e
	}
	defer d.release()
	ctx, cancel := d.operationContext(ctx)
	defer cancel()
	lock, e := storage.Acquire(ctx, opts.Path, opts.LockTimeout)
	if e != nil {
		return receipt, e
	}
	defer func() {
		if e := lock.Close(); err == nil && e != nil {
			err = fail("backup_finalize_failed", "backup", Command{}, e)
		}
	}()
	result, e := d.runningConfig(ctx)
	if e != nil {
		return receipt, e
	}
	contents, _ := result.Config()
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(contents)))
	receipt = BackupReceipt{Path: lock.Path(), Bytes: len(contents), SHA256: digest, CollectedAt: time.Now().UTC(), DeviceCompleted: true, File: storage.Receipt{Path: lock.Path(), State: "not_committed", Phase: "compare"}}
	_, previous, e := storage.Read(ctx, lock.Path(), d.config.MaxOutputBytes)
	if e != nil && !errors.Is(e, os.ErrNotExist) && !(errors.Is(e, syscall.ELOOP) && !opts.RejectSymlink) {
		return receipt, e
	}
	receipt.PreviousSHA256 = previous.SHA256
	switch {
	case previous.SHA256 == "":
		receipt.Change = "created"
	case previous.SHA256 == digest:
		receipt.Change = "unchanged"
	default:
		receipt.Change = "changed"
	}
	if receipt.Change == "unchanged" && previous.Mode.Perm() == 0600 && storage.SameEntry(lock.Path(), previous) {
		receipt.NoWriteVerified = true
		return receipt, nil
	}
	receipt.File, err = storage.Write(ctx, lock.Path(), []byte(contents))
	if err != nil && receipt.File.Committed() {
		code := "backup_persistence_unconfirmed"
		if receipt.File.Durable() {
			code = "backup_finalize_failed"
		}
		if receipt.File.Phase == "directory_sync" && (errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOSYS) || errors.Is(err, syscall.ENOTSUP) || errors.Is(err, syscall.EOPNOTSUPP)) {
			code = "backup_durability_unsupported"
		}
		err = &BackupPersistenceError{failure: &Error{Code: code, Phase: "backup", cause: err}}
	}
	return
}

// BackupPersistenceError marks a failure after the backup file was committed.
// Backup's returned receipt retains the committed/durable boundary. Unwrap
// preserves the connector metadata and original storage error for explicit use.
// Before commit, Backup returns the original storage.WriteError instead.
type BackupPersistenceError struct{ failure *Error }

func (e *BackupPersistenceError) Error() string    { return e.failure.Error() }
func (e *BackupPersistenceError) GoString() string { return e.Error() }
func (e *BackupPersistenceError) Unwrap() error    { return e.failure }

func configurationFailure(c Command, err error) error {
	var classified *Error
	if errors.As(err, &classified) {
		return err
	}
	if errors.Is(err, ErrIncomplete) {
		return fail("incomplete_configuration", "collect", c, err)
	}
	if errors.Is(err, terminal.ErrEncoding) {
		return fail("invalid_output_encoding", "parse", c, err)
	}
	return fail("configuration_error", "collect", c, err)
}
