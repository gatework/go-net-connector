package config

import (
	"bytes"
	"os"
	"testing"
)

func TestInitializerOmitsImplicitDefaults(t *testing.T) {
	s, e := Load(Options{Name: "backup", CWD: t.TempDir(), Home: t.TempDir(), Directories: []string{}, Environment: map[string]string{}, Defaults: Defaults("backup"), AllowMissing: true})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = Initialize(s, "backup"); e != nil {
		t.Fatal(e)
	}
	p, _ := s.InitPaths()
	b, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	for _, key := range []string{"include_hosts:", "cache_ttl:", "parallel:", "ssh_worker_path:"} {
		if bytes.Contains(b, []byte(key)) {
			t.Fatalf("implicit default %s persisted", key)
		}
	}
}
