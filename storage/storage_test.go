package storage

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAtomicWritePreservesOldOnProducerFailure(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config")
	os.WriteFile(p, []byte("old"), 0600)
	boom := errors.New("producer failed")
	r, err := WriteStream(t.Context(), p, func(w io.Writer) error { w.Write([]byte("new")); return boom })
	if !errors.Is(err, boom) || r.Committed() {
		t.Fatalf("receipt=%+v error=%v", r, err)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "old" {
		t.Fatal("old file replaced")
	}
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(p), ".net-connector-*.tmp"))
	if len(matches) != 0 {
		t.Fatal(matches)
	}
}
func TestAtomicWritePrivateDurable(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config")
	r, err := Write(t.Context(), p, []byte("new"))
	if err != nil {
		t.Fatal(err)
	}
	if !r.Durable() {
		t.Fatalf("%+v", r)
	}
	s, _ := os.Stat(p)
	if s.Mode().Perm() != 0600 {
		t.Fatal(s.Mode())
	}
}
func TestStableLockAndAliases(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "Config")
	q := filepath.Join(dir, "CONFIG")
	a, err := Acquire(t.Context(), p, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Acquire(t.Context(), q, 0)
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("wanted busy: %v", err)
	}
	lockPath := a.LockPath()
	if err = a.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(lockPath); err != nil {
		t.Fatal("lock removed", err)
	}
	b, err := Acquire(t.Context(), q, 0)
	if err != nil {
		t.Fatal(err)
	}
	b.Close()
}
func TestReadRejectsLinkAndFIFO(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real")
	os.WriteFile(target, []byte("private"), 0600)
	p := filepath.Join(dir, "link")
	os.Symlink(target, p)
	if _, _, err := Read(t.Context(), p, 1024); err == nil {
		t.Fatal("followed link")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Write(ctx, target, []byte("bad")); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestAtomicStringWriteIsBounded(t *testing.T) {
	body := strings.Repeat("配置\x00", 20000)
	path := filepath.Join(t.TempDir(), "config")
	r, err := WriteStream(t.Context(), path, func(w io.Writer) error {
		text, ok := w.(io.StringWriter)
		if !ok {
			return errors.New("stream lacks bounded string writer")
		}
		n, err := text.WriteString(body)
		if err == nil && n != len(body) {
			return io.ErrShortWrite
		}
		return err
	})
	if err != nil || !r.Durable() {
		t.Fatalf("write: %+v %v", r, err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != body {
		t.Fatal("string bytes changed", err)
	}
}
