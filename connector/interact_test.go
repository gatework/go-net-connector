package connector

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"testing"
	"time"
)

func TestInteractRelaysAndKeepsBorrowedInputOpen(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	input, writer, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	defer input.Close()
	defer writer.Close()
	begin := make(chan struct{})
	done := make(chan struct{})
	var received string
	factory := func(context.Context, ConnectionConfig) (io.ReadWriteCloser, error) {
		a, b := net.Pipe()
		go func() {
			defer b.Close()
			defer close(done)
			io.WriteString(b, "sw#")
			<-begin
			io.WriteString(b, "welcome\n")
			p := make([]byte, 5)
			io.ReadFull(b, p)
			received = string(p)
			io.WriteString(b, "reply\n")
		}()
		return a, nil
	}
	d, e := New(fixtureDriver(), ConnectionConfig{Host: "fixture"}, WithTransportFactory(factory))
	if e != nil {
		t.Fatal(e)
	}
	defer d.Close()
	if e = d.Connect(ctx); e != nil {
		t.Fatal(e)
	}
	close(begin)
	io.WriteString(writer, "ping\n")
	var output bytes.Buffer
	_, e = d.Interact(ctx, InteractOptions{Input: input, Output: &output})
	if e != nil {
		t.Fatal(e)
	}
	<-done
	if received != "ping\n" || output.String() != "welcome\nreply\n" || d.Connected() {
		t.Fatalf("%q %q", received, output.String())
	}
	if _, e = input.Stat(); e != nil {
		t.Fatal("borrowed input closed", e)
	}
}

type failingCloseConn struct {
	net.Conn
	cause error
}

func (c *failingCloseConn) Close() error { _ = c.Conn.Close(); return c.cause }
func TestInteractPreservesPrimaryAndCleanup(t *testing.T) {
	for _, mode := range []string{"cancel", "output"} {
		t.Run(mode, func(t *testing.T) {
			primary, cleanup := errors.New("output failed"), errors.New("cleanup failed")
			input, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()
			defer writer.Close()
			a, b := net.Pipe()
			done := make(chan struct{})
			start := make(chan struct{})
			go func() {
				defer close(done)
				defer b.Close()
				io.WriteString(b, "sw#")
				<-start
				io.WriteString(b, "body")
				io.Copy(io.Discard, b)
			}()
			d, err := New(fixtureDriver(), ConnectionConfig{Host: "fixture"}, WithTransportFactory(func(context.Context, ConnectionConfig) (io.ReadWriteCloser, error) {
				return &failingCloseConn{a, cleanup}, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			if err = d.Connect(t.Context()); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			out := writerFunc(func(p []byte) (int, error) {
				if mode == "cancel" {
					cancel()
					return len(p), nil
				}
				return 0, primary
			})
			close(start)
			_, err = d.Interact(ctx, InteractOptions{Input: input, Output: out, SkipRaw: true})
			if mode == "cancel" {
				primary = context.Canceled
			}
			if !errors.Is(err, primary) || !errors.Is(err, cleanup) {
				t.Fatalf("lost cause: %v", err)
			}
			if _, err = input.Stat(); err != nil {
				t.Fatal("borrowed input closed")
			}
			<-done
		})
	}
}
