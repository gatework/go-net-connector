package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestUnifiedHelpStaysOfflineAndChinese(t *testing.T) {
	for _, args := range [][]string{nil, {"--help"}, {"backup", "--help"}, {"backup", "run", "--help"}, {"topology", "plan", "--help"}, {"config", "init", "--help"}, {"help", "inventory", "list"}, {"version"}, {"completion", "zsh"}} {
		var out, diagnostic bytes.Buffer
		r := Runtime{Args: args, Input: newCommandPanicReader{}, Output: &out, Error: &diagnostic, Environment: map[string]string{"NC_CONFIG": "/unavailable"}}
		if code := r.Run(t.Context()); code != 0 {
			t.Fatalf("%v: %d %s", args, code, diagnostic.String())
		}
		if out.Len() == 0 {
			t.Fatal(args, "empty output")
		}
		for _, english := range []string{"Usage:", "Available Commands:", "Flags:", "help for ", "(default "} {
			if strings.Contains(out.String(), english) {
				t.Fatal(args, english, out.String())
			}
		}
	}
}

func TestUnifiedInvalidCommandsRemainOffline(t *testing.T) {
	for _, args := range [][]string{{"bogus"}, {"backup", "bogus"}, {"backup", "run", "extra"}, {"backup", "run", "--parallel", "0"}, {"topology", "plan", "--tftp"}, {"backup", "export", "192.0.2.1"}} {
		var out, diagnostic bytes.Buffer
		r := Runtime{Args: args, Input: newCommandPanicReader{}, Output: &out, Error: &diagnostic, Environment: map[string]string{"NC_CONFIG": "/unavailable"}}
		if code := r.Run(t.Context()); code != 2 {
			t.Fatalf("%v: %d %s", args, code, diagnostic.String())
		}
	}
}

type newCommandPanicReader struct{}

func (newCommandPanicReader) Read([]byte) (int, error) { panic("stdin read on pure command") }

func TestNetmanCommandIdentity(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"backup", "run", "--help"}, {"completion", "bash"}, {"completion", "zsh"}, {"completion", "fish"}, {"invalid"}} {
		var out, diagnostic bytes.Buffer
		r := Runtime{Args: args, Input: newCommandPanicReader{}, Output: &out, Error: &diagnostic, Environment: map[string]string{}}
		code := r.Run(t.Context())
		text := out.String()
		if args[0] == "invalid" {
			if code != 2 || !strings.HasPrefix(diagnostic.String(), "netman：") {
				t.Fatalf("code=%d diagnostics=%q", code, diagnostic.String())
			}
			text = diagnostic.String()
		} else if code != 0 {
			t.Fatalf("%v: code=%d diagnostics=%s", args, code, diagnostic.String())
		}
		if !strings.Contains(text, "netman") || strings.Contains(text, "net-connector") {
			t.Fatalf("%v: stale command name in %q", args, text)
		}
	}
}
