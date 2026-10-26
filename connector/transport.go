//go:build linux || darwin

package connector

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/gatework/go-net-connector/storage"
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
	if errors.Is(e, unix.EIO) {
		e = io.EOF
	}
	return n, e
}
func (p *processTransport) Write(b []byte) (int, error)           { return p.master.Write(b) }
func (p *processTransport) SyscallConn() (syscall.RawConn, error) { return p.master.SyscallConn() }
func (p *processTransport) Authenticated() error {
	if p.trust != nil {
		return p.trust.commit()
	}
	return nil
}
func (p *processTransport) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	var failure error
	if !p.ioClosed {
		failure = p.master.Close()
		p.ioClosed = true
	}
	if !p.reaped || !p.groupClosed {
		var signalFailure error
		// The session is an independently owned process group, including descendants.
		e := unix.Kill(-p.cmd.Process.Pid, unix.SIGTERM)
		if e != nil && !errors.Is(e, unix.ESRCH) {
			signalFailure = errors.Join(signalFailure, fmt.Errorf("terminate process group: %w", e))
		}
		timer := time.NewTimer(200 * time.Millisecond)
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
			timer = time.NewTimer(2 * time.Second)
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
	if p.reaped && p.groupClosed && p.trust != nil {
		if e := p.trust.close(); e != nil {
			failure = errors.Join(failure, e)
		} else {
			p.trust = nil
		}
	}
	return failure
}
func startProcess(ctx context.Context, cmd *exec.Cmd) (*processTransport, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	master, slave, e := pty.Open()
	if e != nil {
		return nil, e
	}
	ok := false
	defer func() {
		slave.Close()
		if !ok {
			master.Close()
		}
	}()
	if _, e = term.MakeRaw(int(slave.Fd())); e != nil {
		return nil, e
	}
	// Register a nonblocking duplicate with the runtime before handing I/O away.
	fd, e := unix.FcntlInt(master.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if e != nil {
		return nil, e
	}
	if e = unix.SetNonblock(fd, true); e != nil {
		unix.Close(fd)
		return nil, e
	}
	old := master
	master = os.NewFile(uintptr(fd), old.Name())
	old.Close()
	cmd.Stdin = slave
	cmd.Stdout = slave
	cmd.Stderr = slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if e = cmd.Start(); e != nil {
		return nil, e
	}
	p := &processTransport{master: master, cmd: cmd, done: make(chan struct{})}
	go func() { p.waitErr = cmd.Wait(); close(p.done) }()
	ok = true
	if e = ctx.Err(); e != nil {
		p.Close()
		return nil, e
	}
	return p, nil
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
			return nil, e
		}
		args = sshArguments(cfg, trust.snapshot, port)
	}
	p, e := startProcess(ctx, exec.Command(program, args...))
	if e != nil {
		if trust != nil {
			trust.close()
		}
		return nil, e
	}
	p.trust = trust
	return p, nil
}

type hostTrust struct {
	ctx                                    context.Context
	destination, snapshot, directory, host string
	policy                                 HostKeyPolicy
	original                               []string
}

func newHostTrust(ctx context.Context, cfg ConnectionConfig, port int) (*hostTrust, error) {
	destination := cfg.KnownHosts
	if destination == "" {
		home, e := os.UserHomeDir()
		if e != nil {
			return nil, e
		}
		destination = filepath.Join(home, ".ssh", "known_hosts")
	}
	absolute, e := filepath.Abs(destination)
	if e != nil {
		return nil, e
	}
	b, _, e := storage.Read(ctx, absolute, 4<<20)
	if e != nil && !errors.Is(e, os.ErrNotExist) {
		return nil, e
	}
	dir, e := os.MkdirTemp("", "net-connector-hostkeys-")
	if e != nil {
		return nil, e
	}
	target := cfg.Host
	if port != 22 {
		target = "[" + cfg.Host + "]:" + strconv.Itoa(port)
	}
	t := &hostTrust{ctx: ctx, destination: absolute, snapshot: filepath.Join(dir, "known_hosts"), directory: dir, host: target, policy: cfg.HostKeyPolicy}
	if e = os.WriteFile(t.snapshot, b, 0600); e != nil {
		t.close()
		return nil, e
	}
	t.original, e = t.keys(t.snapshot)
	if e != nil {
		t.close()
		return nil, e
	}
	if cfg.replaceHostKey {
		if e = t.remove(t.snapshot); e != nil {
			t.close()
			return nil, e
		}
	}
	return t, nil
}
func (t *hostTrust) keys(path string) ([]string, error) {
	output, e := exec.CommandContext(t.ctx, "ssh-keygen", "-F", t.host, "-f", path).Output()
	if e != nil {
		var status *exec.ExitError
		if errors.As(e, &status) && status.ExitCode() == 1 {
			return nil, nil
		}
		return nil, errors.New("connector: host key lookup failed")
	}
	var keys []string
	for _, line := range strings.Split(string(output), "\n") {
		if line != "" && !strings.HasPrefix(line, "#") {
			keys = append(keys, line)
		}
	}
	return keys, nil
}
func (t *hostTrust) remove(path string) error {
	if e := exec.CommandContext(t.ctx, "ssh-keygen", "-R", t.host, "-f", path).Run(); e != nil {
		return errors.New("connector: host key update failed")
	}
	return nil
}
func (t *hostTrust) commit() error {
	if t.policy == "strict" {
		return nil
	}
	candidate, e := t.keys(t.snapshot)
	if e != nil {
		return e
	}
	if len(candidate) == 0 {
		return errors.New("connector: authenticated host has no key evidence")
	}
	if e = os.MkdirAll(filepath.Dir(t.destination), 0700); e != nil {
		return e
	}
	lock, e := storage.Acquire(t.ctx, t.destination, 5*time.Second)
	if e != nil {
		return e
	}
	defer lock.Close()
	current, _, e := storage.Read(t.ctx, t.destination, 4<<20)
	if e != nil && !errors.Is(e, os.ErrNotExist) {
		return e
	}
	merged := filepath.Join(t.directory, "merged")
	if e = os.WriteFile(merged, current, 0600); e != nil {
		return e
	}
	currentKeys, e := t.keys(merged)
	if e != nil {
		return e
	}
	if strings.Join(currentKeys, "\n") != strings.Join(t.original, "\n") && strings.Join(currentKeys, "\n") != strings.Join(candidate, "\n") {
		return errors.New("connector: concurrent host key conflict")
	}
	if strings.Join(currentKeys, "\n") == strings.Join(candidate, "\n") {
		return nil
	}
	if len(currentKeys) > 0 {
		if e = t.remove(merged); e != nil {
			return e
		}
		current, e = os.ReadFile(merged)
		if e != nil {
			return e
		}
	}
	if len(current) > 0 && current[len(current)-1] != '\n' {
		current = append(current, '\n')
	}
	current = append(current, []byte(strings.Join(candidate, "\n")+"\n")...)
	_, e = storage.Write(t.ctx, t.destination, current)
	return e
}
func (t *hostTrust) close() error { return os.RemoveAll(t.directory) }

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
