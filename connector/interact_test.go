package connector

import (
	"bytes"
	"context"
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
	d, e := New(fixtureProfile(), ConnectionConfig{Host: "fixture"}, WithTransportFactory(factory))
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
