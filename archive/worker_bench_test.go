package archive

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func BenchmarkReadStableArchiveSource(b *testing.B) {
	for _, size := range []int{64 << 10, 1 << 20, 32 << 20} {
		b.Run(fmt.Sprintf("%dKiB", size>>10), func(b *testing.B) {
			root := b.TempDir()
			payload := make([]byte, size)
			for i := range payload {
				payload[i] = byte(i*31 + 7)
			}
			if err := os.WriteFile(filepath.Join(root, "config.cfg"), payload, 0600); err != nil {
				b.Fatal(err)
			}
			directory, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
			if err != nil {
				b.Fatal(err)
			}
			defer unix.Close(directory)
			want := fmt.Sprintf("%x", sha256.Sum256(payload))
			b.ReportAllocs()
			b.SetBytes(int64(size))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				digest, _, err := readFile(context.Background(), directory, "config.cfg", false, nil)
				if err != nil || digest != want {
					b.Fatalf("digest=%q error=%v", digest, err)
				}
			}
		})
	}
}
