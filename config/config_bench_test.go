package config

import (
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkLoadLayeredConfiguration(b *testing.B) {
	root := b.TempDir()
	low := filepath.Join(root, "low")
	high := filepath.Join(root, "high")
	for _, directory := range []string{low, high} {
		if err := os.Mkdir(directory, 0700); err != nil {
			b.Fatal(err)
		}
	}
	fixtures := map[string]string{
		filepath.Join(low, "net-connector.yml"):  "ssh:\n  protocol: ssh\nbackup:\n  directory: ./saved\n  parallel: 2\n",
		filepath.Join(high, "net-connector.yml"): "backup:\n  parallel: 3\n",
		filepath.Join(high, ".env"):              "NC_PARALLEL=4\nNC_DEVICE_USERNAME=bench\n",
	}
	for path, data := range fixtures {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			b.Fatal(err)
		}
	}
	options := Options{
		Name:        "backup",
		CWD:         root,
		Home:        root,
		Directories: []string{low, high},
		Environment: map[string]string{"NC_PARALLEL": "8"},
		Overrides:   map[string]any{"backup": map[string]any{"parallel": 5}},
	}
	if _, err := Load(options); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(1 << 10)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Load(options); err != nil {
			b.Fatal(err)
		}
	}
}
