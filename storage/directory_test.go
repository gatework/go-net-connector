package storage

import (
	"path/filepath"
	"testing"
	"time"
)

func TestBatchDirectoriesAreUnique(t *testing.T) {
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
}
