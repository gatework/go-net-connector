package archive

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestSSHMasterPrivateForegroundProcess(t *testing.T) {
	bin := t.TempDir()
	log := filepath.Join(bin, "argv")
	argsLog := filepath.Join(bin, "all-args")
	fake := `#!/bin/sh
socket=''
mode='session'
printf '%s\n' "$@" >> "$NC_TEST_SSH_ARGS"
while [ "$#" -gt 0 ]; do
  case "$1" in
    -S) socket="$2"; shift 2 ;;
    -O) mode='check'; shift 2 ;;
    -M) mode='master'; shift ;;
    -N) shift ;;
    *) shift ;;
  esac
done
if [ "$mode" = check ]; then [ -e "$socket" ]; exit $?; fi
if [ "$mode" = master ]; then
  : > "$socket"
  printf 'master-started\n' >> "$NC_TEST_SSH_LOG"
  trap 'rm -f "$socket"; exit 0' TERM INT
  while :; do sleep 1; done
fi
exec cat
`
	if err := os.WriteFile(filepath.Join(bin, "ssh"), []byte(fake), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("NC_TEST_SSH_LOG", log)
	t.Setenv("NC_TEST_SSH_ARGS", argsLog)
	u, _ := url.Parse("ssh://archive@example.test:2222/var/tftp")
	m := &sshMaster{endpoint: u, worker: "/usr/local/bin/tftp-archive", password: "private password"}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := m.prepare(ctx); err != nil {
		t.Fatal(err)
	}
	if m.cmd == nil || m.dir == "" {
		t.Fatal("master not owned")
	}
	if stat, err := os.Stat(m.dir); err != nil || stat.Mode().Perm() != 0700 {
		t.Fatalf("socket directory: %v, %v", stat, err)
	}
	args := strings.Join(m.cmd.Args, " ")
	if strings.Contains(args, m.password) || !strings.Contains(args, "ControlPersist=no") || !strings.Contains(args, "StrictHostKeyChecking=yes") || !strings.Contains(args, "-M -N") {
		t.Fatalf("unsafe or incomplete argv: %s", args)
	}
	helper, err := os.ReadFile(filepath.Join(m.dir, "askpass"))
	if err != nil || strings.Contains(string(helper), m.password) {
		t.Fatal("password written to helper", err)
	}
	ch, err := m.launch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ch.Input.Write([]byte("pipe")); err != nil {
		t.Fatal(err)
	}
	body := make([]byte, 4)
	if _, err := io.ReadFull(ch.Output, body); err != nil || string(body) != "pipe" {
		t.Fatalf("pipe %q: %v", body, err)
	}
	if err := ch.Close(); err != nil {
		t.Fatal(err)
	}
	allArgs, err := os.ReadFile(argsLog)
	if err != nil || !strings.Contains(string(allArgs), "example.test\nexec '/usr/local/bin/tftp-archive'") || strings.Contains(string(allArgs), m.password) {
		t.Fatalf("session argv: %q, %v", allArgs, err)
	}
	dir := m.dir
	if err := m.close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("socket directory remains", err)
	}
	if b, err := os.ReadFile(log); err != nil || !strings.Contains(string(b), "master-started") {
		t.Fatal("master did not run", err)
	}
}

func TestSSHMasterCanceledPreparationReaps(t *testing.T) {
	bin := t.TempDir()
	fake := "#!/bin/sh\nsleep 30\n"
	if err := os.WriteFile(filepath.Join(bin, "ssh"), []byte(fake), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	u, _ := url.Parse("ssh://archive@example.test/var/tftp")
	m := &sshMaster{endpoint: u, worker: defaultWorkerPath}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := m.prepare(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
	if m.dir != "" || m.cmd != nil {
		t.Fatal("canceled master not cleaned")
	}
}

func TestSSHWorkerPathQuote(t *testing.T) {
	if !safeWorkerPath("/opt/worker's path") || safeWorkerPath("worker") || safeWorkerPath("/opt/../worker") || safeWorkerPath("/worker\n") {
		t.Fatal("worker path validation")
	}
	got := shellQuote("/opt/worker's path")
	if got != "'/opt/worker'\\''s path'" {
		t.Fatalf("quote %q", got)
	}
}

func TestParseArchiveEndpointDecodedPath(t *testing.T) {
	u, path, err := parseArchiveEndpoint("ssh://archive@example.test/var/tftp%20root")
	if err != nil || path != "/var/tftp root" || u.Hostname() != "example.test" {
		t.Fatalf("endpoint %v %q %v", u, path, err)
	}
	if _, _, err := parseArchiveEndpoint("ssh://archive@example.test/var/%2e%2e/root"); err == nil {
		t.Fatal("accepted parent traversal")
	}
}

func TestSSHMasterExitReapsDescendant(t *testing.T) {
	bin := t.TempDir()
	pidFile := filepath.Join(bin, "child.pid")
	releaseFile := filepath.Join(bin, "exit.fifo")
	if err := syscall.Mkfifo(releaseFile, 0600); err != nil {
		t.Fatal(err)
	}
	fake := `#!/bin/sh
socket=''
mode='session'
while [ "$#" -gt 0 ]; do
 case "$1" in
  -S) socket="$2"; shift 2 ;;
  -O) mode='check'; shift 2 ;;
  -M) mode='master'; shift ;;
  *) shift ;;
 esac
done
if [ "$mode" = check ]; then [ -e "$socket" ]; exit $?; fi
if [ "$mode" = master ]; then
 sleep 30 & echo $! > "$NC_TEST_CHILD_PID"
 : > "$socket"
 read ack < "$NC_TEST_EXIT_FIFO"
 exit 0
fi
exit 0
`
	if err := os.WriteFile(filepath.Join(bin, "ssh"), []byte(fake), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("NC_TEST_CHILD_PID", pidFile)
	t.Setenv("NC_TEST_EXIT_FIFO", releaseFile)
	u, _ := url.Parse("ssh://archive@example.test/var/tftp")
	m := &sshMaster{endpoint: u, worker: defaultWorkerPath}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.prepare(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.close() })
	b, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	var pid int
	if _, err := fmt.Sscanf(string(b), "%d", &pid); err != nil {
		t.Fatal(err)
	}
	defer syscall.Kill(pid, syscall.SIGKILL)
	// Readiness means the child PID is published. Release the leader only now,
	// then join it explicitly before testing descendant-only group cleanup.
	release, err := os.OpenFile(releaseFile, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = release.WriteString("exit\n"); err != nil {
		t.Fatal(err)
	}
	if err = release.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-m.done:
		if err != nil {
			t.Fatal(err)
		}
		m.done = nil
	case <-ctx.Done():
		t.Fatal("leader did not exit", ctx.Err())
	}
	if !m.cmd.ProcessState.Exited() {
		t.Fatal("leader exit precondition missing")
	}
	if err = syscall.Kill(pid, 0); err != nil {
		t.Fatal("descendant missing before close", err)
	}
	if err = m.close(); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			return
		}
	}
	t.Fatal("SSH master descendant survived cleanup")
}
