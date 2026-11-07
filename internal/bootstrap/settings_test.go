package bootstrap

import (
	"context"
	"testing"

	"github.com/gatework/go-net-connector/config"
	"github.com/gatework/go-net-connector/connector"
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

func TestResolvedSettingsDoNotInventMissingBatchDefaults(t *testing.T) {
	defaults := config.Defaults("backup")
	delete(defaults["backup"].(map[string]any), "parallel")
	policy, err := config.Load(config.Options{Name: "backup", CWD: t.TempDir(), Home: t.TempDir(), Directories: []string{}, Defaults: defaults})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = resolveSettings(policy); err == nil {
		t.Fatal("missing batch parallel default was silently restored by bootstrap")
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
		for _, tc := range []struct{ mode, want string }{{"backup", ""}, {"tftp", "/batch/logs"}, {"topology", "/batch/logs"}} {
			want := tc.want
			if explicit != "" {
				want = explicit
			}
			if got := s.LogDirectory(tc.mode, "/batch"); got != want {
				t.Errorf("%s: %q want %q", tc.mode, got, want)
			}
		}
	}
}

func TestDeviceTemplateDoesNotRetainHostOrSecrets(t *testing.T) {
	policy, err := config.Load(config.Options{Name: "backup", CWD: t.TempDir(), Home: t.TempDir(), Directories: []string{}, Defaults: config.Defaults("backup")})
	if err != nil {
		t.Fatal(err)
	}
	username := "first"
	services, err := New(policy, func(key string) string {
		if key == "NC_DEVICE_USERNAME" {
			return username
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	var got []connector.ConnectionConfig
	services = services.WithDeviceFactory(func(_ connector.Profile, c connector.ConnectionConfig) (*connector.Device, error) {
		got = append(got, c)
		return nil, nil
	})
	for _, host := range []string{"192.0.2.1", "192.0.2.2"} {
		if _, err = services.Device(t.Context(), fleet.Record{Host: host, Vendor: "h3c"}); err != nil {
			t.Fatal(err)
		}
		username = "second"
	}
	if got[0].Host != "192.0.2.1" || got[1].Host != "192.0.2.2" || got[0].Username != "first" || got[1].Username != "second" {
		t.Fatal(got)
	}
	if c := services.templates["h3c"]; c.Host != "" || c.Username != "" || c.Password != "" {
		t.Fatal("template retained per-device credentials")
	}
}
