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

var ErrBusy = errors.New("storage: destination is busy")
var ErrUnsafe = errors.New("storage: unsafe file")
var ErrLimit = errors.New("storage: file exceeds limit")

// FileState records atomic replacement and confirmed directory durability.
type FileState string

const (
	StateNotCommitted FileState = "not_committed"
	StateCommitted    FileState = "committed"
	StateDurable      FileState = "durable"
)

type Receipt struct {
	Path  string    `json:"path"`
	State FileState `json:"state"`
	Phase string    `json:"phase"`
}

func (r Receipt) Committed() bool { return r.State == "committed" || r.State == "durable" }
func (r Receipt) Durable() bool   { return r.State == "durable" }

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
	var abs, dir string
	abs, err = filepath.Abs(path)
	if err != nil {
		return
	}
	r.Path = abs
	dir, err = filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return
	}
	destination := filepath.Join(dir, filepath.Base(abs))
	r.Phase = "temporary_file"
	var f *os.File
	f, err = os.CreateTemp(dir, ".net-connector-*.tmp")
	if err != nil {
		return
	}
	temporary := f.Name()
	identity, e := f.Stat()
	if e != nil {
		f.Close()
		os.Remove(temporary)
		err = e
		return
	}
	defer func() {
		if e := f.Close(); err == nil && e != nil {
			err = e
		}
		if s, e := os.Lstat(temporary); e == nil && os.SameFile(s, identity) {
			if e = os.Remove(temporary); err == nil && e != nil {
				err = e
			}
		}
	}()
	if err = f.Chmod(0600); err != nil {
		return
	}
	r.Phase = "write"
	if err = produce(&contextWriter{ctx: ctx, w: f}); err != nil {
		return
	}
	if err = ctx.Err(); err != nil {
		return
	}
	r.Phase = "file_sync"
	if err = f.Sync(); err != nil {
		return
	}
	if err = ctx.Err(); err != nil {
		return
	}
	r.Phase = "rename"
	if err = os.Rename(temporary, destination); err != nil {
		return
	}
	r.State = "committed"
	r.Phase = "directory_open"
	var fd int
	fd, err = unix.Open(dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return
	}
	parent := os.NewFile(uintptr(fd), dir)
	r.Phase = "directory_sync"
	err = parent.Sync()
	closeErr := parent.Close()
	if err != nil {
		return
	}
	r.State = "durable"
	r.Phase = "cleanup"
	if closeErr != nil {
		err = closeErr
		return
	}
	r.Phase = "complete"
	return
}

type contextWriter struct {
	ctx context.Context
	w   io.Writer
}

func (w *contextWriter) Write(p []byte) (int, error) {
	if e := w.ctx.Err(); e != nil {
		return 0, e
	}
	return w.w.Write(p)
}
func Write(ctx context.Context, path string, body []byte) (Receipt, error) {
	return WriteStream(ctx, path, func(w io.Writer) error {
		n, e := w.Write(body)
		if e == nil && n != len(body) {
			return io.ErrShortWrite
		}
		return e
	})
}

type Fingerprint struct {
	SHA256 string
	Mode   os.FileMode
	Info   os.FileInfo
}

func Read(ctx context.Context, path string, limit int64) ([]byte, Fingerprint, error) {
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

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if e := r.ctx.Err(); e != nil {
		return 0, e
	}
	return r.r.Read(p)
}
func SameEntry(path string, f Fingerprint) bool {
	s, e := os.Lstat(path)
	return e == nil && s.Mode().IsRegular() && f.Info != nil && os.SameFile(s, f.Info)
}

type Lock struct {
	file           *os.File
	path, lockPath string
}

func (l *Lock) Path() string     { return l.path }
func (l *Lock) LockPath() string { return l.lockPath }
func (l *Lock) Close() error {
	if l.file == nil {
		return nil
	}
	f := l.file
	l.file = nil
	return f.Close()
}
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
