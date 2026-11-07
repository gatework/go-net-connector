package cli

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"
)

func TestCredentialPipeReadCanBeCanceled(t *testing.T) {
	in, out, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	defer in.Close()
	defer out.Close()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { var b [1]byte; _, err := contextInput(ctx, in).Read(b[:]); done <- err }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("input did not cancel")
	}
}

func TestSecretCancellationRestoresTerminal(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()
	before, err := term.GetState(int(slave.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, e := readTerminalSecret(ctx, slave); done <- e }()
	deadline := time.Now().Add(time.Second)
	for {
		state, e := term.GetState(int(slave.Fd()))
		if e != nil {
			t.Fatal(e)
		}
		if !reflect.DeepEqual(before, state) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("secret input did not enter terminal mode")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case e := <-done:
		if !errors.Is(e, context.Canceled) {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("secret input did not cancel")
	}
	after, err := term.GetState(int(slave.Fd()))
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("terminal was not restored", err)
	}
}
