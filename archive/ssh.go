package archive

import (
	"context"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// DefaultWorkerPath is used by both archive construction and CLI defaults.
const DefaultWorkerPath = "/usr/local/bin/tftp-archive"

// sshMaster owns a single foreground ControlMaster and its private socket.
// The password, when configured, is provided only to a private askpass helper.
type sshMaster struct {
	reap        func(int, chan error) error
	endpoint    *url.URL
	worker      string
	password    string
	mu          sync.Mutex
	dir, socket string
	cmd         *exec.Cmd
	done        chan error
}

func safeWorkerPath(path string) bool {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsAny(path, "\x00\r\n") {
		return false
	}
	for _, r := range path {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }

func (m *sshMaster) args(batch bool) []string {
	mode := "yes"
	if !batch {
		mode = "no"
	}
	port := m.endpoint.Port()
	if port == "" {
		port = "22"
	}
	return []string{"-T", "-S", m.socket, "-o", "BatchMode=" + mode,
		"-o", "StrictHostKeyChecking=yes", "-o", "ConnectTimeout=10",
		"-o", "NumberOfPasswordPrompts=1", "-p", port,
		"-l", m.endpoint.User.Username()}
}

func (m *sshMaster) commandArgs(batch bool, tail ...string) []string {
	args := m.args(batch)
	args = append(args, tail...)
	args = append(args, m.endpoint.Hostname())
	return args
}

func (m *sshMaster) prepare(ctx context.Context) (err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cmd != nil {
		return nil
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	m.dir, err = os.MkdirTemp("/tmp", "nc-tftp-")
	if err != nil {
		return err
	}
	m.socket = filepath.Join(m.dir, "ssh")
	defer func() {
		if err != nil {
			err = errors.Join(err, m.closeLocked())
		}
	}()
	args := m.args(m.password == "")
	if m.password != "" {
		// The script contains no credential; OpenSSH receives it in its private environment.
		helper := filepath.Join(m.dir, "askpass")
		if err = os.WriteFile(helper, []byte("#!/bin/sh\nprintf '%s\\n' \"$NC_ARCHIVE_ASKPASS_SECRET\"\n"), 0700); err != nil {
			return err
		}
		args = append(args, "-o", "PreferredAuthentications=password,keyboard-interactive", "-o", "PubkeyAuthentication=no")
	}
	args = append(args, "-M", "-N", "-o", "ControlPersist=no", m.endpoint.Hostname())
	cmd := exec.Command("ssh", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	if m.password != "" {
		cmd.Env = append(os.Environ(), "SSH_ASKPASS="+filepath.Join(m.dir, "askpass"), "SSH_ASKPASS_REQUIRE=force", "DISPLAY=netman:0", "NC_ARCHIVE_ASKPASS_SECRET="+m.password)
	}
	if err = cmd.Start(); err != nil {
		return ErrArchive
	}
	m.cmd = cmd
	m.done = make(chan error, 1)
	done := m.done
	go func() { done <- cmd.Wait(); close(done) }()
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		check := exec.CommandContext(checkCtx, "ssh", m.commandArgs(true, "-O", "check")...)
		check.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		check.WaitDelay = 750 * time.Millisecond
		check.Cancel = func() error {
			if check.Process == nil {
				return os.ErrProcessDone
			}
			e := syscall.Kill(-check.Process.Pid, syscall.SIGKILL)
			if errors.Is(e, syscall.ESRCH) {
				return os.ErrProcessDone
			}
			return e
		}
		check.Stdin, check.Stdout, check.Stderr = nil, nil, nil
		e := check.Run()
		cancel()
		if e == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return ErrArchive
		case <-m.done:
			m.done = nil
			return ErrArchive
		case <-tick.C:
		}
	}
}

func (m *sshMaster) launch(ctx context.Context) (Channel, error) {
	m.mu.Lock()
	ready := m.cmd != nil
	m.mu.Unlock()
	if !ready {
		return Channel{}, ErrArchive
	}
	// OpenSSH sends a single remote command string through the shell.
	command := "exec " + shellQuote(m.worker)
	args := append(m.args(true), m.endpoint.Hostname(), command)
	return CommandLauncher("ssh", args...)(ctx)
}

func (m *sshMaster) close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closeLocked()
}

func (m *sshMaster) closeLocked() error {
	var err error
	if m.cmd != nil {
		reap := m.reap
		if reap == nil {
			reap = reapGroup
		}
		err = reap(m.cmd.Process.Pid, m.done)
		if err != nil {
			return err
		}
		m.cmd, m.done = nil, nil
	}
	if m.dir != "" {
		if err = os.RemoveAll(m.dir); err != nil {
			return err
		}
		m.dir, m.socket = "", ""
	}
	return err
}

func parseArchiveEndpoint(root string) (*url.URL, string, error) {
	u, err := url.Parse(root)
	if err != nil || u.Scheme != "ssh" || u.User == nil || u.Hostname() == "" {
		return nil, "", ErrArchive
	}
	path := u.Path
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, "", ErrArchive
	}
	if port := u.Port(); port != "" {
		n, e := strconv.Atoi(port)
		if e != nil || n < 1 || n > 65535 {
			return nil, "", ErrArchive
		}
	}
	return u, path, nil
}
