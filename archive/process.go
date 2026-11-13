package archive

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"slices"
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
		case <-done: //nolint:errcheck // only reaping matters; the exit status after a signal is not a result
			reaped = true
		case <-timer.C:
		}
	}
	if e := syscall.Kill(-pid, syscall.SIGTERM); e != nil && !errors.Is(e, syscall.ESRCH) {
		signalErr = errors.Join(signalErr, e)
	}
	wait(100 * time.Millisecond)
	// Always signal the group again after reaping the leader: children can
	// survive TERM or the leader can have exited before cleanup began.
	if e := syscall.Kill(-pid, syscall.SIGKILL); e != nil && !errors.Is(e, syscall.ESRCH) {
		signalErr = errors.Join(signalErr, e)
	}
	wait(750 * time.Millisecond)
	if !reaped {
		waitErr = ErrArchive
	}
	deadline := time.Now().Add(500 * time.Millisecond)
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
	return commandLauncher(executable, slices.Clone(arguments), reapGroup)
}
func commandLauncher(executable string, args []string, reap func(int, chan error) error) Launcher {
	return func(ctx context.Context) (Channel, error) {
		cmd := exec.CommandContext(ctx, executable, args...) //nolint:gosec // the executable is the configured tftp-archive helper path
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.WaitDelay = 750 * time.Millisecond
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
		go func(waitDone chan error) { waitDone <- cmd.Wait(); close(waitDone) }(done)
		var mu sync.Mutex
		closed := false
		var closeErr error
		closeFn := func() error {
			mu.Lock()
			defer mu.Unlock()
			if closed {
				return closeErr
			}
			closeErr = errors.Join(closePipe(input), closePipe(output))
			select {
			case e := <-done:
				done = nil
				if e != nil {
					closeErr = errors.Join(closeErr, ErrArchive)
				}
			default:
			}
			reapErr := reap(cmd.Process.Pid, done)
			closed = reapErr == nil
			return errors.Join(closeErr, reapErr)
		}
		return Channel{Input: input, Output: output, Close: closeFn}, nil
	}
}
