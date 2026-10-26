package archive

import (
	"context"
	"github.com/gatework/go-net-connector/transfer"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewPoolValidationAndSlots(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	for _, tc := range []struct {
		name string
		opts PoolOptions
	}{
		{"zero parallel", PoolOptions{Root: root, Directory: t.TempDir()}},
		{"over limit", PoolOptions{Root: root, Directory: t.TempDir(), Parallel: 501}},
		{"unsafe worker", PoolOptions{Root: root, Directory: t.TempDir(), Parallel: 1, WorkerPath: "relative"}},
		{"password line break", PoolOptions{Root: root, Directory: t.TempDir(), Parallel: 1, Password: "bad\nsecret"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewPool(tc.opts); err == nil {
				t.Fatal("accepted invalid options")
			}
		})
	}
	for _, parallel := range []int{1, 4, 5, 500} {
		p, err := NewPool(PoolOptions{Root: root, Directory: t.TempDir(), Parallel: parallel})
		if err != nil {
			t.Fatal(err)
		}
		if len(p.clients) != (parallel+3)/4 || len(p.available) != parallel {
			t.Fatalf("parallel %d: clients %d, slots %d", parallel, len(p.clients), len(p.available))
		}
		if err := p.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPoolLocalPipes(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	p, err := NewPool(PoolOptions{Root: root, Directory: t.TempDir(), Parallel: 5})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if err := p.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	receipt, err := p.Upload(context.Background(), "running.cfg", "archive.cfg", func(_ context.Context, filename string) (transfer.Receipt, error) {
		if err := os.WriteFile(filepath.Join(root, filename), []byte("real local pipe"), 0600); err != nil {
			return transfer.Receipt{}, err
		}
		return transfer.Receipt{Path: filename, Verification: "device_reported"}, nil
	})
	if err != nil || !receipt.Verified() || receipt.LocalPath == "" {
		t.Fatalf("receipt %+v, err %v", receipt, err)
	}
	if len(p.available) != 5 {
		t.Fatalf("returned slots: %d", len(p.available))
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Upload(context.Background(), "running.cfg", "again.cfg", nil); err == nil {
		t.Fatal("upload after close")
	}
}

func TestPoolCloseCancelsActiveUpload(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	p, err := NewPool(PoolOptions{Root: root, Directory: t.TempDir(), Parallel: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		_, err := p.Upload(context.Background(), "running.cfg", "archive.cfg", func(ctx context.Context, _ string) (transfer.Receipt, error) {
			close(entered)
			<-ctx.Done()
			return transfer.Receipt{}, ctx.Err()
		})
		finished <- err
	}()
	<-entered
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-finished; err == nil {
		t.Fatal("upload did not cancel")
	}
}

func TestPoolLocalBatchReservationAndCollision(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	base := "2026-10-26_12-34-56"
	report := filepath.Join(t.TempDir(), base)
	for i, want := range []string{base, base + "_01", base + "_02"} {
		if i == 1 {
			// A separate batch that uses the same report name must reserve a suffix.
		}
		p, err := NewPool(PoolOptions{Root: root, Directory: report, Parallel: 5})
		if err != nil {
			t.Fatal(err)
		}
		if err := p.Prepare(context.Background()); err != nil {
			t.Fatal(err)
		}
		for _, client := range p.clients {
			if client.batch != want {
				t.Fatalf("batch %q, want %q", client.batch, want)
			}
		}
		if _, err := os.Stat(filepath.Join(root, "archive", want)); err != nil {
			t.Fatal(err)
		}
		if err := p.Close(); err != nil {
			t.Fatal(err)
		}
	}
	for _, want := range []string{base + "_09", base + "_09_01"} {
		p, err := NewPool(PoolOptions{Root: root, Directory: filepath.Join(t.TempDir(), base+"_09"), Parallel: 1})
		if err != nil {
			t.Fatal(err)
		}
		if err := p.Prepare(context.Background()); err != nil {
			t.Fatal(err)
		}
		if p.clients[0].batch != want {
			t.Fatalf("batch %q, want %q", p.clients[0].batch, want)
		}
		if err := p.Close(); err != nil {
			t.Fatal(err)
		}
	}

}

func TestPoolCloseCancelsAndJoinsPrepare(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	p, err := NewPool(PoolOptions{Root: root, Directory: t.TempDir(), Parallel: 1})
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	p.clients[0].launch = func(ctx context.Context) (Channel, error) {
		client, server := net.Pipe()
		done := make(chan struct{})
		go func() {
			defer close(done)
			defer server.Close()
			buf := make([]byte, 4096)
			server.Read(buf)
			close(entered)
			<-ctx.Done()
		}()
		return Channel{Input: client, Output: client, Close: func() error { client.Close(); <-done; return nil }}, nil
	}
	prepared := make(chan error, 1)
	go func() { prepared <- p.Prepare(context.Background()) }()
	<-entered
	closed := make(chan error, 1)
	go func() { closed <- p.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Close blocked behind Prepare")
	}
	select {
	case err := <-prepared:
		if err == nil {
			t.Fatal("Prepare succeeded after Close")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Prepare not joined")
	}
}

func TestPoolOptionDiagnosticsRetainExecutionPassword(t *testing.T) {
	o := PoolOptions{Root: "ssh://archive@example.test/var/tftp", Directory: t.TempDir(), Parallel: 1, Password: "private password"}
	_ = o.String()
	_, err := o.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewPool(o)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if len(p.masters) != 1 || p.masters[0].password != o.Password || p.masters[0].endpoint.Path != "/var/tftp" {
		t.Fatal("option diagnostics changed execution")
	}
}
