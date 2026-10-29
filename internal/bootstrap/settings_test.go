package bootstrap

import (
	"context"
	"testing"

	"github.com/gatework/go-net-connector/config"
	"github.com/gatework/go-net-connector/fleet"
)

func TestPolicyDoesNotResolveCredentials(t *testing.T) {
	s, e := config.Load(config.Options{Name: "backup", CWD: t.TempDir(), Home: t.TempDir(), Directories: []string{}, Defaults: config.Defaults("backup")})
	if e != nil {
		t.Fatal(e)
	}
	calls := 0
	r := func(string) string { calls++; return "" }
	p, e := New(s, r)
	if e != nil || calls != 0 {
		t.Fatal(e, calls)
	}
	_, e = p.Device(context.Background(), fleet.Record{Host: "192.0.2.1", Vendor: "h3c"})
	if e != ErrCredentials || calls == 0 {
		t.Fatal(e, calls)
	}
}

func TestFrozenLogDirectoryModes(t *testing.T) {
	for _, explicit := range []string{"", "/explicit/logs"} {
		env := map[string]string{}
		if explicit != "" {
			env["NC_LOG_DIRECTORY"] = explicit
		}
		policy, err := config.Load(config.Options{Name: "backup", CWD: t.TempDir(), Home: t.TempDir(), Directories: []string{}, Defaults: config.Defaults("backup"), Environment: env})
		if err != nil {
			t.Fatal(err)
		}
		s, err := New(policy, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		for _, tc := range []struct{ program, mode, want string }{{"net-backup", "backup", ""}, {"net-backup", "tftp", ""}, {"net-backup-batch", "backup", ""}, {"net-backup-tftp", "tftp", "/batch/logs"}, {"net-topology", "topology", "/batch/logs"}} {
			want := tc.want
			if explicit != "" {
				want = explicit
			}
			if got := s.LogDirectory(tc.program, tc.mode, "/batch"); got != want {
				t.Errorf("%s/%s: %q want %q", tc.program, tc.mode, got, want)
			}
		}
	}
}
