package connector

import (
	"io"
	"sync"
	"syscall"
	"time"
)

// closeOwner separates transport cleanup from Session.Close's output errors.
// Successful cleanup is never repeated; failed cleanup remains retryable.
type closeOwner struct {
	io.ReadWriteCloser
	mu     sync.Mutex
	closed bool
}

func (t *closeOwner) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil
	}
	e := t.ReadWriteCloser.Close()
	if e == nil {
		t.closed = true
	}
	return e
}
func (t *closeOwner) complete() bool { t.mu.Lock(); defer t.mu.Unlock(); return t.closed }

type osCloseOwner struct {
	*closeOwner
	syscall.Conn
}

func (t *osCloseOwner) SetReadDeadline(deadline time.Time) error {
	if stream, ok := t.ReadWriteCloser.(interface{ SetReadDeadline(t time.Time) error }); ok {
		return stream.SetReadDeadline(deadline)
	}
	return ErrUnsupported
}

func (t *osCloseOwner) NormalizeReadError(err error) error {
	if stream, ok := t.ReadWriteCloser.(interface{ NormalizeReadError(err error) error }); ok {
		return stream.NormalizeReadError(err)
	}
	return err
}
