package config

import (
	"context"
	"testing"
)

func TestExplicitAllCanClearSampleLimit(t *testing.T) {
	override, e := FromEnvironment(map[string]string{"NC_SAMPLE_PER_PLATFORM": "0"})
	if e != nil {
		t.Fatal(e)
	}
	s, e := Load(context.Background(), Options{Name: "backup", CWD: t.TempDir(), Home: t.TempDir(), Directories: []string{}, Environment: map[string]string{}, Defaults: Defaults("backup"), Overrides: override, AllowMissing: true})
	if e != nil {
		t.Fatal(e)
	}
	if got := s.Value("backup.limit_per_platform"); got != 0 {
		t.Fatal(got)
	}
}
