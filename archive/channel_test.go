package archive

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestChannelPreservesPrimaryAndCleanupErrors(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	primary, cleanup := errors.New("primary"), errors.New("cleanup")
	err := runChannel(t.Context(), func(context.Context) (Channel, error) {
		return Channel{Input: a, Output: a, Close: func() error { a.Close(); return cleanup }}, nil
	}, time.Second, func(*Protocol) error { return primary }, nil)
	if !errors.Is(err, primary) || !errors.Is(err, cleanup) {
		t.Fatalf("lost cause: %v", err)
	}
}
func TestChannelOwnsPartialLauncherReturn(t *testing.T) {
	closed := false
	primary, cleanup := errors.New("setup"), errors.New("cleanup")
	err := runChannel(t.Context(), func(context.Context) (Channel, error) {
		return Channel{Close: func() error { closed = true; return cleanup }}, primary
	}, time.Second, func(*Protocol) error { t.Fatal("ran failed channel"); return nil }, nil)
	if !closed || !errors.Is(err, primary) || !errors.Is(err, cleanup) {
		t.Fatalf("closed=%v err=%v", closed, err)
	}
}
