package vendors

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gatework/go-net-connector/connector"
)

func TestH3CBackupConvertsGBKToUTF8(t *testing.T) {
	for _, vendor := range []string{"h3c", "h3c_wireless"} {
		t.Run(vendor, func(t *testing.T) {
			// Literal GB2312/GBK bytes for 中文; the expected text is independent
			// of the decoder used by production code.
			wire := "dis cur\r\n#\r\n description \xd6\xd0\xce\xc4\r\n#\r\n<core>"
			want := "dis cur\n#\n description 中文\n#\n<core>"
			d, check := dialogue(t, vendor, "<core>", []exchange{{"dis cur\n", wire}})
			path := filepath.Join(t.TempDir(), "core.cfg")
			receipt, err := d.Backup(t.Context(), connector.BackupOptions{Path: path})
			if err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(path)
			if err != nil || string(got) != want {
				t.Fatalf("backup = %q, err = %v; want %q", got, err, want)
			}
			if !receipt.File.Durable() || receipt.Bytes != len(want) || receipt.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(want))) {
				t.Fatalf("incorrect UTF-8 receipt: %+v", receipt)
			}
			check()
		})
	}
}

func TestH3CPagedGBKPreservesRawResponse(t *testing.T) {
	first := "dis cur\r\n description \xd6\xd0\xce\xc4\r\n--More--"
	last := "\r\n description \xba\xcb\xd0\xc4\r\n<core>"
	d, _ := dialogue(t, "h3c", "<core>", []exchange{{"dis cur\n", first}, {" ", last}})
	r, err := d.RunningConfig(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	steps := r.Steps()
	if len(steps) != 1 || steps[0].Raw() != first+last || steps[0].OutputEncoding() != "gb18030" {
		t.Fatalf("original bytes or encoding lost: %v", r)
	}
	if got := r.Output(); !strings.Contains(got, "description 中文\n") || !strings.Contains(got, "description 核心\n") || strings.Contains(got, "--More--") {
		t.Fatalf("paged config = %q", got)
	}
}

func TestH3CAutoPreservesUTF8(t *testing.T) {
	d, _ := dialogue(t, "h3c", "<core>", []exchange{{"dis cur\n", "dis cur\r\n description 中文\r\n<core>"}})
	r, err := d.RunningConfig(t.Context())
	if err != nil || r.Output() != "dis cur\n description 中文\n<core>" || r.Steps()[0].OutputEncoding() != "utf-8" {
		t.Fatalf("UTF-8 response changed: %v %v", r, err)
	}
}

func TestH3CGBKTerminalEditsBeforeTranscoding(t *testing.T) {
	wire := "dis cur\r\n description \xd6\xd0\b\bOK\r\n<core>"
	d, _ := dialogue(t, "h3c", "<core>", []exchange{{"dis cur\n", wire}})
	r, err := d.RunningConfig(t.Context())
	if err != nil || r.Output() != "dis cur\n description OK\n<core>" || r.Steps()[0].Raw() != wire {
		t.Fatalf("GBK terminal edits: %v, %v", r, err)
	}
}

func TestH3CRejectsEchoOnly(t *testing.T) {
	d, _ := dialogue(t, "h3c", "<core>", []exchange{{"dis cur\n", "dis cur\r\n<core>"}})
	r, err := d.RunningConfig(t.Context())
	if !errors.Is(err, connector.ErrIncomplete) || len(r.Steps()) != 1 {
		t.Fatalf("empty configuration accepted: %v, %v", r, err)
	}
}

func TestH3CInvalidEncodingPreservesPreviousBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "core.cfg")
	if err := os.WriteFile(path, []byte("last good backup"), 0600); err != nil {
		t.Fatal(err)
	}
	d, _ := dialogue(t, "h3c", "<core>", []exchange{{"dis cur\n", "dis cur\r\n description \xff\r\n<core>"}})
	receipt, err := d.Backup(t.Context(), connector.BackupOptions{Path: path})
	var classified *connector.Error
	if !errors.As(err, &classified) || classified.Code != "invalid_output_encoding" || receipt.File.Committed() || d.Connected() {
		t.Fatalf("invalid encoding accepted: %+v, %v", receipt, err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "last good backup" {
		t.Fatalf("previous backup changed: %q, %v", got, err)
	}
}

func TestH3CDecodedOutputLimitPreservesReceipt(t *testing.T) {
	wire := "description " + strings.Repeat("\xd6\xd0", 10) + "\n<core>"
	d, _ := dialogue(t, "h3c", "<core>", []exchange{{"show\n", wire}}, connector.ConnectionConfig{MaxOutputBytes: int64(len(wire))})
	r, err := d.ExecuteCommand(t.Context(), connector.Command{Text: "show"}, connector.ExecuteOptions{})
	if !errors.Is(err, connector.ErrOutputLimit) || len(r.Steps()) != 1 || r.Steps()[0].Raw() != wire || d.Connected() {
		t.Fatalf("decoded byte limit: %v, %v", r, err)
	}
}

func TestH3CLogConvertsAndRedactsEncodedSecrets(t *testing.T) {
	t.Run("automatic", func(t *testing.T) {
		var log bytes.Buffer
		d, check := dialogue(t, "h3c", "<core>", []exchange{{"show\n", "description \xd6\xd0\xce\xc4\r\n" + "password \xc2\xa9\r\n<core>"}}, connector.ConnectionConfig{Password: "漏", Logger: &log})
		_, err := d.ExecuteCommand(t.Context(), connector.Command{Text: "show"}, connector.ExecuteOptions{})
		if err != nil {
			t.Fatal(err)
		}
		check()
		text := log.String()
		if !strings.Contains(text, "description 中文") || !strings.Contains(text, "[REDACTED]") || strings.Contains(text, "漏") || strings.Contains(text, "©") || strings.Contains(text, `\xC2`) {
			t.Fatalf("invalid or unredacted log: %q", text)
		}
	})
}
