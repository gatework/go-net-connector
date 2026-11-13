package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func diffBatchFixture(t *testing.T, body string) string {
	t.Helper()
	d := t.TempDir()
	name := "core-192.0.2.1.txt"
	if err := os.WriteFile(filepath.Join(d, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(body))
	data, _ := json.Marshal(map[string]any{"schema_version": 4, "operation": "backup", "devices": []any{map[string]any{"host": "192.0.2.1", "status": "succeeded", "artifacts": map[string]any{"backup": map[string]any{"path": filepath.Join(d, name), "bytes": len(body), "sha256": hex.EncodeToString(sum[:])}}}}})
	if err := os.WriteFile(filepath.Join(d, "report.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestBackupDiffExitCodesAndOutput(t *testing.T) {
	from := diffBatchFixture(t, "hostname r1\ninterface Gi0/1\n description old\n")
	same := diffBatchFixture(t, "hostname r1\ninterface Gi0/1\n description old\n")
	to := diffBatchFixture(t, "hostname r1\ninterface Gi0/1\n description new\n")
	run := func(args ...string) (int, string, string) {
		var out, errout bytes.Buffer
		r := Runtime{Args: append([]string{"backup", "diff", "192.0.2.1"}, args...), CWD: t.TempDir(), Home: t.TempDir(), Directories: []string{}, Input: panicReader{}, Output: &out, Error: &errout}
		return r.Run(context.Background()), out.String(), errout.String()
	}
	if code, out, errout := run("--from", from, "--to", same); code != 0 || !strings.Contains(out, "配置一致") {
		t.Fatal(code, out, errout)
	}
	code, out, errout := run("--from", from, "--to", to)
	if code != 1 || !strings.Contains(out, "- description old\n+ description new\n") || !strings.Contains(out, "新增 1 行；删除 1 行") {
		t.Fatal(code, out, errout)
	}
	code, out, _ = run("--from", from, "--to", to, "--json")
	var got map[string]any
	if code != 1 || json.Unmarshal([]byte(out), &got) != nil || got["changed"] != true || got["added"] != float64(1) {
		t.Fatal(code, out)
	}
	if code, _, errout := run("--from", from); code != 2 || !strings.Contains(errout, "--to") {
		t.Fatal(code, errout)
	}
}
