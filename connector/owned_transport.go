package connector

import (
	"io"
	"sync"
	"syscall"
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
