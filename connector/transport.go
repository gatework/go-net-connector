//go:build linux || darwin

package connector

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

type processTransport struct {
	master           *os.File
	cmd              *exec.Cmd
	done             chan struct{}
	waitErr          error
	mu               sync.Mutex
	ioClosed, reaped bool
	groupClosed      bool
	trust            *hostTrust
}

func (p *processTransport) Read(b []byte) (int, error) {
	n, e := p.master.Read(b)
	return n, p.NormalizeReadError(e)
}
func (p *processTransport) SetReadDeadline(deadline time.Time) error {
	return p.master.SetReadDeadline(deadline)
}
func (*processTransport) NormalizeReadError(err error) error {
	if errors.Is(err, unix.EIO) {
		return io.EOF
	}
	return err
}
func (p *processTransport) Write(b []byte) (int, error)           { return p.master.Write(b) }
func (p *processTransport) SyscallConn() (syscall.RawConn, error) { return p.master.SyscallConn() }
func (p *processTransport) Authenticated(ctx context.Context) error {
	if p.trust != nil {
		return p.trust.commit(ctx)
	}
	return ctx.Err()
}
func (p *processTransport) Close() error {
	var commitDone <-chan struct{}
	if p.trust != nil {
		commitDone = p.trust.stop()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	var failure error
	if !p.ioClosed {
		if p.master != nil {
			failure = p.master.Close()
		}
		p.ioClosed = true
	}
	if !p.reaped || !p.groupClosed {
		var signalFailure error
		// The session is an independently owned process group, including descendants.
		e := unix.Kill(-p.cmd.Process.Pid, unix.SIGTERM)
		if e != nil && !errors.Is(e, unix.ESRCH) {
			signalFailure = errors.Join(signalFailure, fmt.Errorf("terminate process group: %w", e))
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-p.done:
			timer.Stop()
			p.reaped = true
		case <-timer.C:
		}
		// Reap descendants even if the group leader exited first.
		e = unix.Kill(-p.cmd.Process.Pid, unix.SIGKILL)
		if e != nil && !errors.Is(e, unix.ESRCH) {
			signalFailure = errors.Join(signalFailure, fmt.Errorf("kill process group: %w", e))
		}
		if !p.reaped {
			timer = time.NewTimer(time.Second)
			select {
			case <-p.done:
				timer.Stop()
				p.reaped = true
			case <-timer.C:
				failure = errors.Join(failure, errors.New("connector: process cleanup timed out"))
			}
		}
		// A group can disappear between signal delivery and reaping (Darwin may
		// report EPERM during that race). Absence resolves the signal failure;
		// an extant group retains cleanup ownership for a later retry.
		if signalFailure != nil && errors.Is(unix.Kill(-p.cmd.Process.Pid, 0), unix.ESRCH) {
			signalFailure = nil
		}
		p.groupClosed = signalFailure == nil
		failure = errors.Join(failure, signalFailure)
	}
	if commitDone != nil {
		<-commitDone
	}
	if p.reaped && p.groupClosed && p.trust != nil {
		failure = errors.Join(failure, p.trust.close())
	}
	return failure
}
func startProcess(ctx context.Context, cmd *exec.Cmd) (p *processTransport, err error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	master, slave, e := pty.Open()
	if e != nil {
		return nil, e
	}
	// From here on, a failed initialization still returns the cleanup owner.
	p = &processTransport{master: master, cmd: cmd, reaped: true, groupClosed: true}
	defer func() {
		err = errors.Join(err, slave.Close())
	}()
	if _, e = term.MakeRaw(int(slave.Fd())); e != nil {
		return p, e
	}
	// Register a nonblocking duplicate with the runtime before handing I/O away.
	fd, e := unix.FcntlInt(master.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if e != nil {
		return p, e
	}
	if e = unix.SetNonblock(fd, true); e != nil {
		return p, errors.Join(e, unix.Close(fd))
	}
	old := master
	master = os.NewFile(uintptr(fd), old.Name())
	p.master = master
	if e = old.Close(); e != nil {
		return p, e
	}
	cmd.Stdin = slave
	cmd.Stdout = slave
	cmd.Stderr = slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if e = cmd.Start(); e != nil {
		return p, e
	}
	p.done = make(chan struct{})
	p.reaped, p.groupClosed = false, false
	go func() { p.waitErr = cmd.Wait(); close(p.done) }()
	return p, ctx.Err()
}
func openProcess(ctx context.Context, cfg ConnectionConfig) (io.ReadWriteCloser, error) {
	var args []string
	program := cfg.SSHPath
	var trust *hostTrust
	var e error
	port := cfg.Port
	if cfg.Protocol == "telnet" {
		program = cfg.TelnetPath
		if program == "" {
			program = "telnet"
		}
		if port == 0 {
			port = 23
		}
		args = []string{cfg.Host, strconv.Itoa(port)}
	} else {
		if program == "" {
			program = "ssh"
		}
		if port == 0 {
			port = 22
		}
		trust, e = newHostTrust(ctx, cfg, port)
		if e != nil {
			if trust != nil {
				return &processTransport{trust: trust, ioClosed: true, reaped: true, groupClosed: true}, e
			}
			return nil, e
		}
		args = sshArguments(cfg, trust.snapshot, port)
	}
	p, e := startProcess(ctx, exec.Command(program, args...))
	if p == nil {
		if trust == nil {
			return nil, e
		}
		p = &processTransport{ioClosed: true, reaped: true, groupClosed: true}
	}
	p.trust = trust
	return p, e
}

func sshArguments(cfg ConnectionConfig, knownHosts string, port int) []string {
	checking := "ask"
	if cfg.HostKeyPolicy == "strict" {
		checking = "yes"
	}
	args := append([]string(nil), cfg.LegacySSHArguments...)
	args = append(args, "-tt", "-o", "StrictHostKeyChecking="+checking, "-o", "UserKnownHostsFile="+knownHosts, "-o", "GlobalKnownHostsFile=/dev/null", "-o", "UpdateHostKeys=no", "-o", "ConnectTimeout="+strconv.Itoa(max(1, int(cfg.LoginTimeout.Seconds()))), "-p", strconv.Itoa(port))
	if cfg.IdentityFile != "" {
		args = append(args, "-i", cfg.IdentityFile)
	}
	if cfg.Username != "" {
		args = append(args, "-l", cfg.Username)
	}
	args = append(args, "--", cfg.Host)
	return args
}
