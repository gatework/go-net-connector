package bootstrap

import (
	"context"
	"errors"
	"testing"

	"github.com/gatework/go-net-connector/config"
)

func TestStructuredSourceFileOverridesDefaults(t *testing.T) {
	p, e := config.Load(context.Background(), config.Options{Name: "backup", CWD: t.TempDir(), Home: t.TempDir(), Directories: []string{}, Environment: map[string]string{"NC_TFTP_SOURCE_FILES": `{"huawei":"flash:/custom.cfg"}`}, Defaults: config.Defaults("tftp")})
	if e != nil {
		t.Fatal(e)
	}
	s, e := New(p, nil)
	if e != nil {
		t.Fatal(e)
	}
	policy, e := s.TFTPPolicy()
	if e != nil {
		t.Fatal(e)
	}
	target, e := policy.For("192.0.2.1", "huawei")
	if e != nil || target.SourceFile != "flash:/custom.cfg" {
		t.Fatalf("source=%q error=%v", target.SourceFile, e)
	}
}

func TestRemovedSourceFileEnvironmentIsRejected(t *testing.T) {
	_, e := config.Load(context.Background(), config.Options{Name: "backup", CWD: t.TempDir(), Home: t.TempDir(), Directories: []string{}, Environment: map[string]string{"NC_HUAWEI_TFTP_SOURCE_FILE": "flash:/custom.cfg"}, Defaults: config.Defaults("tftp")})
	field, ok := errors.AsType[*config.FieldError](e)
	if !ok || field.Problem != "removed" || field.Field != "NC_HUAWEI_TFTP_SOURCE_FILE" || field.Replacement != "tftp.source_files.huawei" || !errors.Is(e, config.ErrInvalid) {
		t.Fatalf("removed variable not reported: %#v", e)
	}
}
