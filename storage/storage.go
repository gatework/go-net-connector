// Package storage owns private files, stable path locks, and commit receipts.
package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// ErrBusy reports a destination lock held by another writer.
var ErrBusy = errors.New("storage: destination is busy")

// ErrUnsafe reports a path that is empty, a symlink or not the expected file
// type.
var ErrUnsafe = errors.New("storage: unsafe file")

// ErrLimit reports a file larger than the caller's limit.
var ErrLimit = errors.New("storage: file exceeds limit")

// FileState records atomic replacement and confirmed directory durability.
type FileState string

// File states, in order of progress.
const (
	StateNotCommitted FileState = "not_committed"
	StateCommitted    FileState = "committed"
	StateDurable      FileState = "durable"
)

// Receipt records how far a replacement got: its state and the phase it
// reached.
type Receipt struct {
	Path  string    `json:"path"`
	State FileState `json:"state"`
	Phase string    `json:"phase"`
}

// Committed reports whether the new file was renamed into place.
func (r Receipt) Committed() bool { return r.State == "committed" || r.State == "durable" }

// Durable reports whether the rename was also synced to the directory.
func (r Receipt) Durable() bool { return r.State == "durable" }

// WriteError reports a failed replacement with the receipt of how far it got.
type WriteError struct {
	Receipt Receipt
	Cause   error
}

func (e *WriteError) Error() string {
	return "storage: replacement " + string(e.Receipt.State) + " at " + e.Receipt.Phase
}
func (e *WriteError) Unwrap() error { return e.Cause }

// WriteStream never rolls back a committed rename. The caller owns its producer.
func WriteStream(ctx context.Context, path string, produce func(io.Writer) error) (r Receipt, err error) {
	r = Receipt{Path: path, State: "not_committed", Phase: "resolve_parent"}
	defer func() {
		if err != nil {
			err = &WriteError{Receipt: r, Cause: err}
		}
	}()
	if err = ctx.Err(); err != nil {
		return
	}
	if path == "" || produce == nil {
		err = ErrUnsafe
		return
	}
	var dir string
	if r.Path, dir, err = resolveParent(path); err != nil {
		return
	}
	destination := filepath.Join(dir, filepath.Base(r.Path))
	r.Phase = "temporary_file"
	f, cleanup, err := createTemporary(dir)
	if err != nil {
		return
	}
	defer cleanup(&err)
	if err = fill(ctx, &r, f, produce); err != nil {
		return
	}
	r.Phase = "rename"
	if err = os.Rename(f.Name(), destination); err != nil {
		return
	}
	r.State = "committed"
	err = syncParent(&r, dir)
	return
}

// resolveParent returns the absolute path and the symlink-free directory
// that will hold it.
func resolveParent(path string) (abs, dir string, err error) {
	if abs, err = filepath.Abs(path); err != nil {
		return path, "", err
	}
	dir, err = filepath.EvalSymlinks(filepath.Dir(abs))
	return abs, dir, err
}

// createTemporary creates a private temporary file in dir. cleanup closes
// it and removes it unless it was renamed or replaced, recording the first
// error in *err.
func createTemporary(dir string) (f *os.File, cleanup func(err *error), err error) {
	f, err = os.CreateTemp(dir, ".net-connector-*.tmp")
	if err != nil {
		return nil, nil, err
	}
	temporary := f.Name()
	identity, err := f.Stat()
	if err != nil {
		f.Close()
		os.Remove(temporary)
		return nil, nil, err
	}
	cleanup = func(err *error) {
		if e := f.Close(); *err == nil && e != nil {
			*err = e
		}
		if s, e := os.Lstat(temporary); e == nil && os.SameFile(s, identity) {
			if e = os.Remove(temporary); *err == nil && e != nil {
				*err = e
			}
		}
	}
	return f, cleanup, nil
}

// fill restricts f, streams produce into it and syncs it, advancing
// r.Phase as it goes.
func fill(ctx context.Context, r *Receipt, f *os.File, produce func(io.Writer) error) error {
	if err := f.Chmod(0600); err != nil {
		return err
	}
	r.Phase = "write"
	if err := produce(&contextWriter{ctx: ctx, w: f}); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	r.Phase = "file_sync"
	if err := f.Sync(); err != nil {
		return err
	}
	return ctx.Err()
}

// syncParent makes the rename in dir durable, advancing r.
func syncParent(r *Receipt, dir string) error {
	r.Phase = "directory_open"
	fd, err := unix.Open(dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	parent := os.NewFile(uintptr(fd), dir)
	r.Phase = "directory_sync"
	err = parent.Sync()
	closeErr := parent.Close()
	if err != nil {
		return err
	}
	r.State = "durable"
	r.Phase = "cleanup"
	if closeErr != nil {
		return closeErr
	}
	r.Phase = "complete"
	return nil
}

// contextWriter makes one streamed copy observe cancellation.
type contextWriter struct {
	ctx context.Context //nolint:containedctx // an io.Writer adapter cannot take ctx per Write
	w   io.Writer
}

func (w *contextWriter) Write(p []byte) (int, error) {
	if e := w.ctx.Err(); e != nil {
		return 0, e
	}
	return w.w.Write(p)
}

// WriteString keeps the caller's immutable text while converting only bounded
// chunks for the underlying byte writer. Each chunk observes cancellation.
func (w *contextWriter) WriteString(text string) (int, error) {
	buffer := make([]byte, 32<<10)
	total := 0
	for len(text) > 0 {
		n := copy(buffer, text)
		written, err := w.Write(buffer[:n])
		total += written
		if err != nil {
			return total, err
		}
		if written != n {
			return total, io.ErrShortWrite
		}
		text = text[n:]
	}
	return total, w.ctx.Err()
}

// Write atomically replaces path with body; see WriteStream.
func Write(ctx context.Context, path string, body []byte) (Receipt, error) {
	return WriteStream(ctx, path, func(w io.Writer) error {
		n, e := w.Write(body)
		if e == nil && n != len(body) {
			return io.ErrShortWrite
		}
		return e
	})
}

// Fingerprint identifies a regular file by its content digest, mode and
// identity.
type Fingerprint struct {
	SHA256 string
	Mode   os.FileMode
	Info   os.FileInfo
}

func readFile(ctx context.Context, path string, limit int64, includeContents bool) ([]byte, Fingerprint, error) {
	if e := ctx.Err(); e != nil {
		return nil, Fingerprint{}, e
	}
	if limit <= 0 {
		return nil, Fingerprint{}, ErrLimit
	}
	fd, e := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if e != nil {
		return nil, Fingerprint{}, e
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	s, e := f.Stat()
	if e != nil {
		return nil, Fingerprint{}, e
	}
	if !s.Mode().IsRegular() {
		return nil, Fingerprint{}, ErrUnsafe
	}
	if s.Size() > limit {
		return nil, Fingerprint{}, ErrLimit
	}
	if !includeContents {
		h := sha256.New()
		n, err := io.CopyBuffer(h, io.LimitReader(&contextReader{ctx: ctx, r: f}, limit+1), make([]byte, 32<<10))
		if err != nil {
			return nil, Fingerprint{}, err
		}
		if n > limit {
			return nil, Fingerprint{}, ErrLimit
		}
		return nil, Fingerprint{hex.EncodeToString(h.Sum(nil)), s.Mode(), s}, nil
	}
	b, e := io.ReadAll(io.LimitReader(&contextReader{ctx: ctx, r: f}, limit+1))
	if e != nil {
		return nil, Fingerprint{}, e
	}
	if int64(len(b)) > limit {
		return nil, Fingerprint{}, ErrLimit
	}
	sum := sha256.Sum256(b)
	return b, Fingerprint{hex.EncodeToString(sum[:]), s.Mode(), s}, nil
}

// Inspect verifies a bounded regular file without retaining its content.
func Inspect(ctx context.Context, path string, limit int64) (Fingerprint, error) {
	_, f, e := readFile(ctx, path, limit, false)
	return f, e
}

// Read returns the contents of the regular file at path, up to limit bytes,
// with its fingerprint.
func Read(ctx context.Context, path string, limit int64) ([]byte, Fingerprint, error) {
	return readFile(ctx, path, limit, true)
}

// contextReader makes one bounded read observe cancellation.
type contextReader struct {
	ctx context.Context //nolint:containedctx // an io.Reader adapter cannot take ctx per Read
	r   io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if e := r.ctx.Err(); e != nil {
		return 0, e
	}
	return r.r.Read(p)
}

// SameEntry reports whether path is still the regular file f was taken from.
func SameEntry(path string, f Fingerprint) bool {
	s, e := os.Lstat(path)
	return e == nil && s.Mode().IsRegular() && f.Info != nil && os.SameFile(s, f.Info)
}

// Lock is an exclusive advisory lock guarding writes to a path.
type Lock struct {
	file           *os.File
	path, lockPath string
}

// Path returns the path the lock guards.
func (l *Lock) Path() string { return l.path }

// Close releases the lock. Closing twice is a no-op.
func (l *Lock) Close() error {
	if l.file == nil {
		return nil
	}
	f := l.file
	l.file = nil
	return f.Close()
}

// LockPath returns the hidden lock file that guards path, in path's
// directory.
func LockPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", ErrUnsafe
	}
	abs, e := filepath.Abs(path)
	if e != nil {
		return "", e
	}
	dir, e := filepath.EvalSymlinks(filepath.Dir(abs))
	if e != nil {
		return "", e
	}
	name := norm.NFC.String(cases.Fold().String(norm.NFC.String(filepath.Base(abs))))
	sum := sha256.Sum256([]byte(name))
	return filepath.Join(dir, fmt.Sprintf(".net-connector-%x.lock", sum)), nil
}

// Acquire takes the lock for path, waiting up to timeout; it fails with
// ErrBusy when the lock stays held.
func Acquire(ctx context.Context, path string, timeout time.Duration) (*Lock, error) {
	if timeout < 0 {
		return nil, ErrUnsafe
	}
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	lockPath, e := LockPath(path)
	if e != nil {
		return nil, e
	}
	return acquireLock(ctx, path, lockPath, timeout)
}

// AcquireLockFile uses an existing external protocol's stable lock filename.
func AcquireLockFile(ctx context.Context, path string, timeout time.Duration) (*Lock, error) {
	if timeout < 0 {
		return nil, ErrUnsafe
	}
	return acquireLock(ctx, path, path, timeout)
}
func acquireLock(ctx context.Context, path, lockPath string, timeout time.Duration) (*Lock, error) {
	fd, e := unix.Open(lockPath, unix.O_RDWR|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0600)
	if e != nil {
		return nil, e
	}
	f := os.NewFile(uintptr(fd), lockPath)
	success := false
	defer func() {
		if !success {
			f.Close()
		}
	}()
	verify := func() error {
		var s unix.Stat_t
		if e := unix.Fstat(fd, &s); e != nil {
			return e
		}
		a, e := f.Stat()
		if e != nil {
			return e
		}
		b, e := os.Lstat(lockPath)
		if e != nil {
			return e
		}
		if !a.Mode().IsRegular() || s.Uid != uint32(os.Geteuid()) || s.Nlink != 1 || s.Mode&07777 != 0600 || !os.SameFile(a, b) || !b.Mode().IsRegular() {
			return ErrUnsafe
		}
		return nil
	}
	if e = verify(); e != nil {
		return nil, e
	}
	deadline := time.Now().Add(timeout)
	for {
		if e = ctx.Err(); e != nil {
			return nil, e
		}
		e = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		if e == nil {
			if e = verify(); e != nil {
				return nil, e
			}
			abs, _ := filepath.Abs(path)
			success = true
			return &Lock{f, abs, lockPath}, nil
		}
		if !errors.Is(e, unix.EWOULDBLOCK) && !errors.Is(e, unix.EAGAIN) {
			return nil, e
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, ErrBusy
		}
		wait := min(50*time.Millisecond, remaining)
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
