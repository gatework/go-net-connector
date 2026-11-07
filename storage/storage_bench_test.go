package storage

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Both fingerprint paths verify the same file; Inspect deliberately drops the
// returned contents. Setup and expected fingerprint are outside timed regions.
func BenchmarkFingerprint8MiB(b *testing.B) {
	path := filepath.Join(b.TempDir(), "config")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 8<<20)), 0600); err != nil {
		b.Fatal(err)
	}
	_, want, err := Read(b.Context(), path, 8<<20)
	if err != nil {
		b.Fatal(err)
	}
	for _, retain := range []bool{true, false} {
		name := "Inspect"
		if retain {
			name = "Read"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(8 << 20)
			for b.Loop() {
				var got Fingerprint
				var err error
				if retain {
					_, got, err = Read(b.Context(), path, 8<<20)
				} else {
					got, err = Inspect(b.Context(), path, 8<<20)
				}
				if err != nil || got.SHA256 != want.SHA256 {
					b.Fatal("fingerprint changed", err)
				}
			}
		})
	}
}
func BenchmarkWriteText8MiB(b *testing.B) {
	body := strings.Repeat("x", 8<<20)
	for _, stream := range []bool{false, true} {
		name := "Bytes"
		if stream {
			name = "Stream"
		}
		b.Run(name, func(b *testing.B) {
			path := filepath.Join(b.TempDir(), "config")
			b.ReportAllocs()
			b.SetBytes(8 << 20)
			for b.Loop() {
				var r Receipt
				var err error
				if stream {
					r, err = WriteStream(b.Context(), path, func(w io.Writer) error { _, e := io.WriteString(w, body); return e })
				} else {
					r, err = Write(b.Context(), path, []byte(body))
				}
				if err != nil || !r.Durable() {
					b.Fatal("write", err)
				}
			}
		})
	}
}
