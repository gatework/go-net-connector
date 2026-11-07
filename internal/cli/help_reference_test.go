package cli

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

var updateCommandReference = flag.Bool("update-cli-reference", false, "更新同源中文命令参考")

func TestChineseCommandReference(t *testing.T) {
	var helpErr error
	root := (Runtime{}).commandTree(context.Background(), &helpErr)
	root.InitDefaultHelpCmd()
	var text strings.Builder
	text.WriteString("# netman 命令参考\n\n由命令树生成。维护时运行 `go test ./internal/cli -run TestChineseCommandReference -args -update-cli-reference`；不手工修改以下命令说明。\n\n")
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		if c.Hidden {
			return
		}
		text.WriteString("## " + c.CommandPath() + "\n\n```text\n")
		if err := renderChineseHelp(c, &text); err != nil {
			t.Fatal(err)
		}
		text.WriteString("```\n\n")
		for _, child := range c.Commands() {
			walk(child)
		}
	}
	walk(root)
	path := filepath.Join("..", "..", "docs", "cli-reference.md")
	if *updateCommandReference {
		if err := os.WriteFile(path, []byte(text.String()), 0644); err != nil {
			t.Fatal(err)
		}
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(saved) != text.String() {
		t.Fatal("命令参考与当前帮助不一致；请通过 -update-cli-reference 更新")
	}
}

type shortCommandWriter struct{}

func (shortCommandWriter) Write(p []byte) (int, error) { return len(p) / 2, nil }
func TestHelpOutputFailureReturnsNonzero(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"version"}, {"--version"}, {"help", "backup", "run"}} {
		var diagnostic strings.Builder
		r := Runtime{Args: args, Input: newCommandPanicReader{}, Output: shortCommandWriter{}, Error: &diagnostic, Environment: map[string]string{}}
		if code := r.Run(t.Context()); code != 2 {
			t.Fatalf("%v: %d", args, code)
		}
	}
}

// Even successful local JSON output may race the user's cancellation.
func TestConfigShowCancellationTakesPriority(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "backup.yml")
	if err := os.WriteFile(file, []byte("backup:\n  directory: ./backups\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var diagnostics strings.Builder
	r := Runtime{Args: []string{"config", "show", "--config", file}, CWD: root, Home: root, Directories: []string{}, Environment: map[string]string{}, Input: strings.NewReader(""), Output: commandCancelWriter{cancel}, Error: &diagnostics}
	if code := r.Run(ctx); code != 130 {
		t.Fatalf("successful output hid cancellation: exit=%d diagnostics=%s", code, diagnostics.String())
	}
}

type commandCancelWriter struct{ cancel context.CancelFunc }

func (w commandCancelWriter) Write(p []byte) (int, error) { w.cancel(); return len(p), nil }
