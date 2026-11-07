package bootstrap

import (
	"testing"
	"time"

	"github.com/gatework/go-net-connector/config"
)

func TestTFTPUploadTimeoutConfiguration(t *testing.T) {
	for _, tc := range []struct {
		env  map[string]string
		want time.Duration
	}{{map[string]string{}, 180 * time.Second}, {map[string]string{"NC_TFTP_UPLOAD_TIMEOUT": "120"}, 120 * time.Second}} {
		p, e := config.Load(config.Options{Name: "backup", CWD: t.TempDir(), Home: t.TempDir(), Directories: []string{}, Environment: tc.env, Defaults: config.Defaults("tftp"), AllowMissing: true})
		if e != nil {
			t.Fatal(e)
		}
		s, e := New(p, nil)
		if e != nil {
			t.Fatal(e)
		}
		if s.TFTPUploadTimeout() != tc.want {
			t.Fatal(s.TFTPUploadTimeout())
		}
	}
}

func TestTFTPPolicyIsResolvedOnceAtConstruction(t *testing.T) {
	policy, err := config.Load(config.Options{
		Name:        "backup",
		CWD:         t.TempDir(),
		Home:        t.TempDir(),
		Directories: []string{},
		Defaults:    config.Defaults("tftp"),
		Environment: map[string]string{"NC_TFTP_HOST": "192.0.2.50"},
	})
	if err != nil {
		t.Fatal(err)
	}
	services, err := New(policy, nil)
	if err != nil {
		t.Fatal(err)
	}
	first, err := services.TFTPPolicy()
	if err != nil {
		t.Fatal(err)
	}
	second, err := services.TFTPPolicy()
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("TFTP policy was rebuilt for each workflow")
	}
	target, err := first.For("192.0.2.1", "h3c")
	if err != nil || target.Server != "192.0.2.50" {
		t.Fatalf("target=%+v error=%v", target, err)
	}
}
