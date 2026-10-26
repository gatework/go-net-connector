package archive

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

type enteredConn struct {
	net.Conn
	entered chan struct{}
	once    sync.Once
}

func (c *enteredConn) Read(p []byte) (int, error) {
	c.once.Do(func() { close(c.entered) })
	return c.Conn.Read(p)
}
func (c *enteredConn) Write(p []byte) (int, error) {
	c.once.Do(func() { close(c.entered) })
	return c.Conn.Write(p)
}

func TestProtocolBlockedIOPreservesCancellation(t *testing.T) {
	for _, op := range []string{"send", "expect", "receive"} {
		t.Run(op, func(t *testing.T) {
			a, b := net.Pipe()
			defer a.Close()
			defer b.Close()
			c := &enteredConn{Conn: a, entered: make(chan struct{})}
			p := NewProtocol(c, c, time.Second)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				var err error
				switch op {
				case "send":
					err = p.Send(ctx, Request{Action: "probe"})
				case "expect":
					_, err = p.Expect(ctx, "ready")
				case "receive":
					p.phase = "archived"
					_, err = p.Receive(ctx, Proof{Bytes: 1}, io.Discard)
				}
				done <- err
			}()
			<-c.entered
			cancel()
			if err := <-done; !errors.Is(err, context.Canceled) {
				t.Fatalf("lost cancellation: %v", err)
			}
		})
	}
}
