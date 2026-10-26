package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenAppendRejectsLinksAndPublicFiles(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	os.WriteFile(target, []byte("safe"), 0600)
	for _, kind := range []string{"symlink", "hardlink", "public"} {
		t.Run(kind, func(t *testing.T) {
			p := filepath.Join(dir, kind)
			switch kind {
			case "symlink":
				os.Symlink(target, p)
			case "hardlink":
				os.Link(target, p)
			case "public":
				os.WriteFile(p, []byte("safe"), 0644)
			}
			f, e := OpenAppend(p)
			if e == nil {
				f.Close()
				t.Fatal("accepted unsafe log")
			}
		})
	}
}
