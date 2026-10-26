package archive

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// reapGroup terminates the owned process group and waits for both its leader
// and descendants. The leader can exit before a descendant, so Wait alone is
// never evidence that the group is gone.
func reapGroup(pid int, done chan error) error {
	var signalErr, waitErr error
	reaped := done == nil
	wait := func(duration time.Duration) {
		if reaped {
			return
		}
		timer := time.NewTimer(duration)
		defer timer.Stop()
		select {
		case <-done:
			reaped = true
		case <-timer.C:
		}
	}
	if e := syscall.Kill(-pid, syscall.SIGTERM); e != nil && !errors.Is(e, syscall.ESRCH) {
		signalErr = errors.Join(signalErr, e)
	}
	wait(200 * time.Millisecond)
	// Always signal the group again after reaping the leader: children can
	// survive TERM or the leader can have exited before cleanup began.
	if e := syscall.Kill(-pid, syscall.SIGKILL); e != nil && !errors.Is(e, syscall.ESRCH) {
		signalErr = errors.Join(signalErr, e)
	}
	wait(2 * time.Second)
	if !reaped {
		waitErr = ErrArchive
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		e := syscall.Kill(-pid, 0)
		if errors.Is(e, syscall.ESRCH) {
			return waitErr
		}
		if time.Now().After(deadline) {
			return errors.Join(signalErr, waitErr, ErrArchive)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func closePipe(file io.Closer) error {
	err := file.Close()
	if errors.Is(err, os.ErrClosed) {
		return nil
	}
	return err
}

// CommandLauncher starts one worker per transaction over ordinary pipes.
// The caller supplies an installed executable; this never installs remote code.
func CommandLauncher(executable string, arguments ...string) Launcher {
	args := append([]string(nil), arguments...)
	return func(ctx context.Context) (Channel, error) {
		cmd := exec.CommandContext(ctx, executable, args...)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.WaitDelay = 2 * time.Second
		cmd.Cancel = func() error {
			if cmd.Process == nil {
				return os.ErrProcessDone
			}
			e := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
			if errors.Is(e, syscall.ESRCH) {
				return os.ErrProcessDone
			}
			return e
		}
		stdinRead, input, e := os.Pipe()
		if e != nil {
			return Channel{}, ErrArchive
		}
		output, stdoutWrite, e := os.Pipe()
		if e != nil {
			stdinRead.Close()
			input.Close()
			return Channel{}, ErrArchive
		}
		cmd.Stdin, cmd.Stdout = stdinRead, stdoutWrite
		if e = cmd.Start(); e != nil {
			stdinRead.Close()
			input.Close()
			output.Close()
			stdoutWrite.Close()
			return Channel{}, ErrArchive
		}
		stdinRead.Close()
		stdoutWrite.Close()
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		var mu sync.Mutex
		closed := false
		var closeErr error
		close := func() error {
			mu.Lock()
			defer mu.Unlock()
			if closed {
				return closeErr
			}
			closed = true
			closeErr = errors.Join(closePipe(input), closePipe(output))
			select {
			case e := <-done:
				done = nil
				if e != nil {
					closeErr = errors.Join(closeErr, ErrArchive)
				}
			default:
			}
			closeErr = errors.Join(closeErr, reapGroup(cmd.Process.Pid, done))
			return closeErr
		}
		return Channel{Input: input, Output: output, Close: close}, nil
	}
}
