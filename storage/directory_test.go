package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBatchDirectoriesAndSavedExport(t *testing.T) {
	root := t.TempDir()
	stamp := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	a, e := CreateBatchDirectory(root, stamp)
	if e != nil {
		t.Fatal(e)
	}
	b, e := CreateBatchDirectory(root, stamp)
	if e != nil || a == b || filepath.Base(a) != "2026-01-02_08-00-00" {
		t.Fatal(a, b, e)
	}
	if _, e = Write(context.Background(), filepath.Join(root, "2001_db8__1.txt"), []byte("config")); e != nil {
		t.Fatal(e)
	}
	data, e := ReadSaved(context.Background(), root, "2001:db8::1")
	if e != nil || string(data) != "config" {
		t.Fatal(string(data), e)
	}
	os.Symlink(filepath.Join(root, "2001_db8__1.txt"), filepath.Join(root, "192.0.2.1.txt"))
	if _, e = ReadSaved(context.Background(), root, "192.0.2.1"); e == nil {
		t.Fatal("symlink followed")
	}
}
