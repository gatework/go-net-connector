package archive

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"testing"
)

func TestPoolRetainsFailedCleanupForRetry(t *testing.T) {
	sentinel := errors.New("reaper failed")
	calls := 0
	m := &sshMaster{cmd: &exec.Cmd{Process: &os.Process{Pid: 999999}}, dir: t.TempDir(), reap: func(int, chan error) error {
		calls++
		if calls == 1 {
			return sentinel
		}
		return nil
	}}
	p := &Pool{masters: []*sshMaster{m}, stop: make(chan struct{}), closeDone: make(chan struct{})}
	if err := p.Close(); !errors.Is(err, sentinel) || m.cmd == nil || m.dir == "" {
		t.Fatalf("cleanup ownership lost: %v", err)
	}
	if err := p.Close(); err != nil || calls != 2 || m.cmd != nil || m.dir != "" {
		t.Fatalf("retry calls=%d err=%v", calls, err)
	}
	if err := p.Close(); err != nil || calls != 2 {
		t.Fatal("repeated successful cleanup")
	}
}
func TestClientRetainsFailedChannelCleanup(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	calls := 0
	sentinel := errors.New("close failed")
	c, err := NewClient(ClientOptions{Root: t.TempDir(), Directory: t.TempDir(), Launcher: func(context.Context) (Channel, error) {
		return Channel{Input: a, Output: a, Close: func() error {
			calls++
			if calls == 1 {
				return sentinel
			}
			return a.Close()
		}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	err = c.channel(t.Context(), func(*Protocol) error { return nil })
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil || calls != 2 {
		t.Fatalf("cleanup retry calls=%d err=%v", calls, err)
	}
	if err := c.Close(); err != nil || calls != 2 {
		t.Fatal("successful cleanup repeated")
	}
	if err := c.channel(t.Context(), func(*Protocol) error { return nil }); err == nil {
		t.Fatal("closed client admitted work")
	}
}

func TestWorkerChannelRetriesFailedReap(t *testing.T) {
	calls := 0
	sentinel := errors.New("temporary reap failure")
	launch := commandLauncher("/bin/sh", []string{"-c", "exec sleep 30"}, func(pid int, done chan error) error {
		calls++
		if calls == 1 {
			return sentinel
		}
		return reapGroup(pid, done)
	})
	ch, err := launch(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer ch.Close()
	if err := ch.Close(); !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	if err := ch.Close(); err != nil || calls != 2 {
		t.Fatalf("reap retry calls=%d err=%v", calls, err)
	}
	if err := ch.Close(); err != nil || calls != 2 {
		t.Fatal("repeated completed reap")
	}
}
