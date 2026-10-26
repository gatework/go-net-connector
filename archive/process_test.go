package archive

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/gatework/go-net-connector/transfer"
)

func TestInstalledWorkerPipes(t *testing.T) {
	worker := os.Getenv("NC_TEST_ARCHIVE_WORKER")
	if worker == "" {
		t.Skip("set NC_TEST_ARCHIVE_WORKER to built worker")
	}
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	client, e := NewClient(ClientOptions{Root: root, Directory: t.TempDir(), Launcher: CommandLauncher(worker)})
	if e != nil {
		t.Fatal(e)
	}
	if e = client.Prepare(context.Background()); e != nil {
		t.Fatal(e)
	}
	receipt, e := client.Upload(context.Background(), "running.txt", "device.txt", func(_ context.Context, name string) (transfer.Receipt, error) {
		if e := os.WriteFile(filepath.Join(root, name), []byte("process config"), 0600); e != nil {
			return transfer.Receipt{}, e
		}
		return transfer.Receipt{Path: name, Verification: "device_reported"}, nil
	})
	if e != nil || !receipt.Verified() {
		t.Fatal(receipt, e)
	}
}

func TestCommandLauncherCloseKillsDescendantsAfterLeaderExit(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	ch, err := CommandLauncher("/bin/sh", "-c", "sleep 30 & echo $! > '"+pidFile+"'; exit 0")(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var pid int
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		b, _ := os.ReadFile(pidFile)
		if _, _ = fmt.Sscanf(string(b), "%d", &pid); pid > 0 {
			break
		}
	}
	if pid == 0 {
		t.Fatal("descendant pid unavailable")
	}
	defer syscall.Kill(pid, syscall.SIGKILL)
	if err := ch.Close(); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			return
		}
	}
	t.Fatal("descendant survived Close")
}
