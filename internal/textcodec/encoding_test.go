package textcodec

import (
	"errors"
	"strings"
	"testing"
)

func TestDecodeDeviceText(t *testing.T) {
	for _, tc := range []struct{ name, encoding, input, want, detected string }{
		{"ascii", "auto", "sysname core", "sysname core", "utf-8"},
		{"utf8", "auto", "description 中文", "description 中文", "utf-8"},
		{"gbk", "gbk", "description \xd6\xd0\xce\xc4", "description 中文", "gbk"},
		{"short-gbk-auto", "auto", "description \xd6\xd0\xce\xc4", "description 中文", "gb18030"},
		{"one-character-auto", "auto", "\xd6\xd0", "中", "gb18030"},
		{"gb18030", "gb18030", "description \x95\x34\xb2\x35", "description 𠮷", "gb18030"},
		{"gb18030-auto", "auto", "description \x95\x34\xb2\x35", "description 𠮷", "gb18030"},
		{"explicit-overrides-valid-utf8", "gbk", "\xc2\xa9", "漏", "gbk"},
		{"auto-prefers-valid-utf8", "auto", "\xc2\xa9", "©", "utf-8"},
		{"literal-replacement", "gb18030", "\x84\x31\xa4\x37", "\ufffd", "gb18030"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, detected, err := Decode(tc.input, tc.encoding)
			if err != nil || got != tc.want || detected != tc.detected {
				t.Fatalf("got %q (%s), %v; want %q (%s)", got, detected, err, tc.want, tc.detected)
			}
		})
	}
}

func TestDecodeRejectsLossyInput(t *testing.T) {
	for _, encoding := range []string{"auto", "gbk", "gb18030", "utf-8"} {
		for _, input := range []string{"description \xff", "description \xd6", "description \xd6\n", "description \x81\x30\x81"} {
			if got, _, err := Decode(input, encoding); !errors.Is(err, ErrEncoding) || got != "" {
				t.Errorf("%s accepted %q: %q, %v", encoding, input, got, err)
			}
		}
	}
}

func TestAutoDetectsChineseAfterLargeASCIIPrefix(t *testing.T) {
	prefix := strings.Repeat("interface GigabitEthernet1/0/1\r\n", 4096)
	got, _, err := Decode(prefix+" description \xd6\xd0\xce\xc4\r\n<core>", "auto")
	if err != nil || got != prefix+" description 中文\r\n<core>" {
		t.Fatalf("late Chinese lost: %v", err)
	}
}
