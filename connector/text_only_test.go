package connector

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestTextOnlyBackupStrictEncodingAndNoSidecar(t *testing.T) {
	for _, tc := range []struct{ name, wire, want string }{
		{"ascii", "configuration", "configuration"},
		{"utf8", "description 中文", "description 中文"},
		{"gbk", "description \xd6\xd0\xce\xc4", "description 中文"},
		{"gb18030", "description \x95\x34\xb2\x35", "description 𠮷"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, _ := fixtureDevice(t, fixtureProfile(), []string{"show config\r\n" + tc.wire + "\r\nsw#"})
			path := filepath.Join(t.TempDir(), "device.txt")
			receipt, err := d.Backup(t.Context(), BackupOptions{Path: path, TextOnly: true})
			if err != nil || !receipt.Complete() || !receipt.HasSavedEvidence() || !receipt.TextOnly || receipt.Source != nil || receipt.TextWarning != nil {
				t.Fatalf("invalid text-only receipt: %v", err)
			}
			data, err := os.ReadFile(path)
			if err != nil || !strings.Contains(string(data), tc.want) {
				t.Fatalf("text mismatch %v", err)
			}
			raw, _ := filepath.Glob(path + ".*.raw")
			if len(raw) != 0 {
				t.Fatal("created raw sidecar")
			}
		})
	}
}
func TestTextOnlyBackupEncodingFailureKeepsPreviousFile(t *testing.T) {
	d, _ := fixtureDevice(t, fixtureProfile(), []string{"show config\r\ndescription 中文\xff\r\nsw#"})
	path := filepath.Join(t.TempDir(), "device.txt")
	if err := os.WriteFile(path, []byte("previous"), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := d.Backup(t.Context(), BackupOptions{Path: path, TextOnly: true})
	if err == nil || r.Complete() {
		t.Fatal("unknown encoding accepted")
	}
	b, _ := os.ReadFile(path)
	if string(b) != "previous" {
		t.Fatal("old backup replaced")
	}
	raw, _ := filepath.Glob(path + ".*.raw")
	if len(raw) != 0 {
		t.Fatal("created raw sidecar")
	}
}
func TestTextOnlyBackupUnchangedNeedsNoSource(t *testing.T) {
	wire := "show config\r\nconfiguration\r\nsw#"
	d, _ := fixtureDevice(t, fixtureProfile(), []string{wire, wire})
	path := filepath.Join(t.TempDir(), "device.txt")
	if _, err := d.Backup(t.Context(), BackupOptions{Path: path, TextOnly: true}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(path)
	r, err := d.Backup(t.Context(), BackupOptions{Path: path, TextOnly: true})
	after, _ := os.Stat(path)
	if err != nil || r.Change != "unchanged" || !r.NoWriteVerified || !r.Complete() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("unchanged receipt invalid: %v", err)
	}
	r.TextOnly = false
	if r.Complete() {
		t.Fatal("source-required receipt marked complete")
	}
}
func TestSessionEventsDoNotDependOnLoggerOrExposeContents(t *testing.T) {
	d, _ := fixtureDevice(t, fixtureProfile(), []string{"secret-command\r\nPRIVATE_BODY\r\nsw#"})
	var names []string
	d.config.LogLevel = "error"
	d.log.level = "error"
	d.config.OnSessionEvent = func(_ context.Context, e SessionEvent) error {
		names = append(names, e.Name)
		if strings.Contains(e.Host+e.Name+e.Status, "PRIVATE") {
			t.Fatal("body exposed")
		}
		return nil
	}
	if _, err := d.ExecuteCommand(t.Context(), Command{Text: "secret-command", Sensitive: true}, ExecuteOptions{}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(names, []string{"connect", "login_complete", "command_start", "command_complete", "operation_complete"}) {
		t.Fatal(names)
	}
}
func TestSessionObserverFailureRetainsCompletedStep(t *testing.T) {
	d, _ := fixtureDevice(t, fixtureProfile(), []string{"show\r\nvalue\r\nsw#"})
	cause := errors.New("observer unavailable")
	d.config.OnSessionEvent = func(_ context.Context, e SessionEvent) error {
		if e.Name == "command_complete" {
			return cause
		}
		return nil
	}
	r, err := d.ExecuteCommand(t.Context(), Command{Text: "show"}, ExecuteOptions{})
	if !errors.Is(err, cause) || len(r.Steps()) != 1 || d.Connected() {
		t.Fatalf("lost completion/error: steps=%d err=%v", len(r.Steps()), err)
	}
}

func TestTextOnlyEncodingFailureDoesNotCreateArtifact(t *testing.T) {
	d, _ := fixtureDevice(t, fixtureProfile(), []string{"show config\r\n中文\xff\r\nsw#"})
	path := filepath.Join(t.TempDir(), "device.txt")
	if _, err := d.Backup(t.Context(), BackupOptions{Path: path, TextOnly: true}); err == nil {
		t.Fatal("unknown encoding accepted")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("invalid artifact published")
	}
}
func TestTextOnlyReceiptNeedsDurableEvidence(t *testing.T) {
	r := BackupReceipt{TextOnly: true, DeviceCompleted: true}
	if r.HasSavedEvidence() || r.Complete() {
		t.Fatal("empty evidence accepted")
	}
	r.File.State = "committed"
	if !r.HasSavedEvidence() || r.Complete() {
		t.Fatal("commit confused with durability")
	}
	r.File.State = "durable"
	if !r.Complete() {
		t.Fatal("durable text-only evidence rejected")
	}
}
func TestSessionObserverFailureBeforeConnectionDoesNotOpenTransport(t *testing.T) {
	cause := errors.New("progress failed")
	opens := 0
	d, err := New(fixtureProfile(), ConnectionConfig{Host: "fixture", OnSessionEvent: func(context.Context, SessionEvent) error { return cause }}, WithTransportFactory(func(context.Context, ConnectionConfig) (io.ReadWriteCloser, error) {
		opens++
		return nil, errors.New("unexpected transport")
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err = d.Connect(t.Context()); !errors.Is(err, cause) || opens != 0 {
		t.Fatal(err, opens)
	}
}
func TestSessionObserverReentryIsBusy(t *testing.T) {
	d, _ := fixtureDevice(t, fixtureProfile(), []string{"show\r\nvalue\r\nsw#"})
	var reentry error
	d.config.OnSessionEvent = func(ctx context.Context, e SessionEvent) error {
		if e.Name == "command_start" {
			_, reentry = d.ExecuteCommand(ctx, Command{Text: "unexpected"}, ExecuteOptions{})
		}
		return nil
	}
	if _, err := d.ExecuteCommand(t.Context(), Command{Text: "show"}, ExecuteOptions{}); err != nil || !errors.Is(reentry, ErrBusy) {
		t.Fatal(err, reentry)
	}
}
