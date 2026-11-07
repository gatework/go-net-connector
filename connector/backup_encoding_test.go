package connector

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBackupPreservesInvalidBytesAndChinese(t *testing.T) {
	wire := "show config\r\n description 中文\xff\r\nsw#"
	d, _ := fixtureDevice(t, fixtureProfile(), []string{wire})
	path := filepath.Join(t.TempDir(), "device.txt")
	receipt, err := d.Backup(t.Context(), BackupOptions{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "中文\\xFF") {
		t.Fatalf("reading view not preserved: %v", err)
	}
	if receipt.Source == nil || receipt.TextWarning == nil || receipt.TextWarning.Count != 1 {
		t.Fatal("missing raw source/warning")
	}
	raw, err := os.ReadFile(receipt.Source.Path)
	if err != nil || string(raw) != wire || receipt.Source.SHA256 != fmt.Sprintf("%x", sha256.Sum256(raw)) || !receipt.Source.File.Durable() {
		t.Fatal("raw mismatch")
	}
	clone := receipt.Clone()
	clone.TextWarning.Positions[0].Offset = 999
	clone.Source.Segments[0].Bytes = 999
	if receipt.TextWarning.Positions[0].Offset == 999 || receipt.Source.Segments[0].Bytes == 999 {
		t.Fatal("mutable warning/segments")
	}
}
func TestBackupRawFailureDoesNotReplaceOldText(t *testing.T) {
	wire := "show config\r\nnew config\r\nsw#"
	d, _ := fixtureDevice(t, fixtureProfile(), []string{wire})
	path := filepath.Join(t.TempDir(), "device.txt")
	os.WriteFile(path, []byte("old backup"), 0600)
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(wire)))
	os.Mkdir(path+"."+digest+".raw", 0700)
	receipt, err := d.Backup(t.Context(), BackupOptions{Path: path})
	if err == nil || receipt.File.Committed() {
		t.Fatal("raw failure accepted")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "old backup" {
		t.Fatal("old text replaced")
	}
}

func TestBackupDoesNotRelaxRunningConfig(t *testing.T) {
	d, _ := fixtureDevice(t, fixtureProfile(), []string{"show config\r\n description 中文\xff\r\nsw#"})
	_, err := d.RunningConfig(t.Context())
	if err == nil {
		t.Fatal("strict configuration accepted invalid encoding")
	}
}
func TestBackupEscapeExpansionRespectsOutputCap(t *testing.T) {
	wire := "show config\r\n description " + strings.Repeat("\xff", 20) + "\r\nsw#"
	d, _ := fixtureDevice(t, fixtureProfile(), []string{wire})
	d.config.MaxOutputBytes = int64(len(wire))
	path := filepath.Join(t.TempDir(), "device.txt")
	_, err := d.Backup(t.Context(), BackupOptions{Path: path})
	if err == nil {
		t.Fatal("escaped view exceeded output cap")
	}
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("oversized view published")
	}
}
func TestBackupRetainsDeviceRejectionBeforeTerminalEdit(t *testing.T) {
	d, _ := fixtureDevice(t, fixtureProfile(), []string{"% Invalid \xff\r\x1b[2Kshow config\r\nbody\r\nsw#"})
	path := filepath.Join(t.TempDir(), "device.txt")
	_, err := d.Backup(t.Context(), BackupOptions{Path: path})
	if err == nil {
		t.Fatal("erased device rejection accepted")
	}
}
func TestBackupRawSymlinkDoesNotReplaceOldText(t *testing.T) {
	wire := "show config\r\nnew config\r\nsw#"
	d, _ := fixtureDevice(t, fixtureProfile(), []string{wire})
	dir := t.TempDir()
	path := filepath.Join(dir, "device.txt")
	os.WriteFile(path, []byte("old"), 0600)
	target := filepath.Join(dir, "target")
	os.WriteFile(target, []byte("private"), 0600)
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(wire)))
	os.Symlink(target, path+"."+digest+".raw")
	_, err := d.Backup(t.Context(), BackupOptions{Path: path})
	if err == nil {
		t.Fatal("unsafe source accepted")
	}
	old, _ := os.ReadFile(path)
	other, _ := os.ReadFile(target)
	if string(old) != "old" || string(other) != "private" {
		t.Fatal("symlink target or previous backup changed")
	}
}
func TestBackupWarningsAreBoundedAndKeepRawOffsets(t *testing.T) {
	wire := "show config\r\n description 中文" + strings.Repeat("\xff", 100) + "\r\nsw#"
	d, _ := fixtureDevice(t, fixtureProfile(), []string{wire})
	r, err := d.Backup(t.Context(), BackupOptions{Path: filepath.Join(t.TempDir(), "device.txt")})
	if err != nil {
		t.Fatal(err)
	}
	if r.TextWarning.Count != 100 || len(r.TextWarning.Positions) != 64 {
		t.Fatal("warning count lost")
	}
	p := r.TextWarning.Positions[0]
	if p.Offset != strings.Index(wire, "\xff") || p.Line != 2 || p.Column != strings.Index(strings.Split(wire, "\n")[1], "\xff")+1 {
		t.Fatal("incorrect original byte position")
	}
}

func TestBackupWarningPositionsAcrossSteps(t *testing.T) {
	var result Result
	for _, raw := range []string{"中\xff", "文\xfe\n"} {
		data := &responseData{raw: raw, output: raw}
		if _, err := data.convertBackup(); err != nil {
			t.Fatal(err)
		}
		result.steps = append(result.steps, StepResult{response: Response{data: data}})
	}
	_, _, _, warning := backupSource(result)
	if warning == nil || warning.Count != 2 || len(warning.Positions) != 2 {
		t.Fatal("missing warning")
	}
	p := warning.Positions[1]
	if p.Offset != 7 || p.Line != 1 || p.Column != 8 {
		t.Fatalf("second position: %+v", p)
	}
}
