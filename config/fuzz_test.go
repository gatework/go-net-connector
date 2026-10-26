package config

import "testing"

func FuzzYAMLBoundary(f *testing.F) {
	f.Add([]byte("backup:\n  parallel: 4\n"))
	f.Add([]byte("x: &x [*x]\n"))
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 1<<16 {
			t.Skip()
		}
		_, _ = ParseYAML(b)
	})
}
