package connector

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRunningConfigRejectsEchoOnly(t *testing.T) {
	d, _ := fixtureDevice(t, fixtureProfile(), []string{"show config\nsw#"})
	r, e := d.RunningConfig(t.Context())
	if e == nil || len(r.Steps()) != 1 {
		t.Fatalf("%v %v", r, e)
	}
}

func TestRunningConfigAutomaticallyDecodesGBK(t *testing.T) {
	wire := "show config\r\n description \xd6\xd0\xce\xc4\r\nsw#"
	d, _ := fixtureDevice(t, fixtureProfile(), []string{wire})
	r, err := d.RunningConfig(t.Context())
	if err != nil || r.Output() != "show config\n description 中文\nsw#" {
		t.Fatalf("automatic decode: %v, %v", r, err)
	}
	if r.Steps()[0].Raw() != wire || r.Steps()[0].OutputEncoding() != "gb18030" {
		t.Fatal("original bytes or encoding lost")
	}
}
func TestBackupPreservesUnchangedMtime(t *testing.T) {
	contents := "show config\r\nhostname test\r\nsw#"
	d, _ := fixtureDevice(t, fixtureProfile(), []string{contents, contents, contents})
	path := filepath.Join(t.TempDir(), "device.txt")
	first, e := d.Backup(t.Context(), BackupOptions{Path: path})
	if e != nil {
		t.Fatal(e)
	}
	if first.Change != "created" || !first.File.Durable() || first.NoWriteVerified {
		t.Fatalf("%+v", first)
	}
	when := time.Unix(100, 0)
	os.Chtimes(path, when, when)
	second, e := d.Backup(t.Context(), BackupOptions{Path: path})
	if e != nil {
		t.Fatal(e)
	}
	s, _ := os.Stat(path)
	if second.Change != "unchanged" || !second.NoWriteVerified || second.File.Committed() || !s.ModTime().Equal(when) {
		t.Fatalf("%+v %v", second, s.ModTime())
	}
	b, _ := os.ReadFile(path)
	if second.SHA256 != fmt.Sprintf("%x", sha256.Sum256(b)) {
		t.Fatal("receipt digest wrong")
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	third, err := d.Backup(t.Context(), BackupOptions{Path: path})
	if err != nil || third.Change != "unchanged" || third.NoWriteVerified || !third.File.Durable() {
		t.Fatalf("insecure existing file was not replaced: %+v %v", third, err)
	}
}
func TestBackupRefusesBusyPathBeforeOpeningDevice(t *testing.T) {
	// A recursive callback must not obtain path ownership inside an active session.
	d, _ := fixtureDevice(t, fixtureProfile(), []string{"ok\nsw#"})
	_, e := d.ExecuteCommand(t.Context(), Command{Text: "show"}, ExecuteOptions{OnReceipt: func(_ctx context.Context, _ StepResult) error {
		_, err := d.Backup(_ctx, BackupOptions{Path: filepath.Join(t.TempDir(), "x")})
		if err != ErrBusy {
			t.Error(err)
		}
		return nil
	}})
	if e != nil {
		t.Fatal(e)
	}
}
