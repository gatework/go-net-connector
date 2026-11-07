package textcodec

import (
	"reflect"
	"strings"
	"testing"
)

func TestBackupViewPreservesUTF8AroundBadByte(t *testing.T) {
	text, encoding, count := BackupView("description 中文\xff\nother 中文\n")
	if text != "description 中文\\xFF\nother 中文\n" || count != 1 || encoding != "escaped" {
		t.Fatalf("unexpected %q %s %d", text, encoding, count)
	}
}
func TestBackupViewDecodesLegacyLine(t *testing.T) {
	text, encoding, count := BackupView("description \xd6\xd0\xce\xc4\n")
	if text != "description 中文\n" || encoding != "gb18030" || count != 0 {
		t.Fatalf("unexpected %q %s %d", text, encoding, count)
	}
}
func TestBackupViewKeepsLiteralText(t *testing.T) {
	input := "description � \\xFF 中文\n"
	got, _, count := BackupView(input)
	if got != input || count != 0 || strings.Contains(got, "\\xEF") {
		t.Fatal("literal changed")
	}
}

func TestBackupViewSourceOffsets(t *testing.T) {
	text, encoding, count, offsets := BackupViewSource("中文\xff\nvalid\n中\xfe")
	if text != "中文\\xFF\nvalid\n中\\xFE" || encoding != "escaped" || count != 2 || !reflect.DeepEqual(offsets, []int{6, 17}) {
		t.Fatalf("%q %s %d %v", text, encoding, count, offsets)
	}
	_, _, count, offsets = BackupViewSource(strings.Repeat("中\xff\n", 100))
	if count != 100 || len(offsets) != 64 {
		t.Fatalf("count %d offsets %d", count, len(offsets))
	}
}
