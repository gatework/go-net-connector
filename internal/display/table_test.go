package display

import (
	"bytes"
	"strings"
	"testing"

	"github.com/rivo/uniseg"
)

func TestTableCellWidthsAndAddresses(t *testing.T) {
	for _, width := range []int{40, 80, 120} {
		var b bytes.Buffer
		ip := "2001:db8:1234:5678:abcd:1234:5678:1234"
		err := Table(&b, [][5]string{{ip, "中文交换机👩‍💻é长名称", "h3c", "S6550", "地址重复"}}, width)
		if err != nil {
			t.Fatal(err)
		}
		for line := range strings.SplitSeq(b.String(), "\n") {
			if uniseg.StringWidth(line) >= width {
				t.Fatalf("width %d: %q", width, line)
			}
		}
		var joined strings.Builder
		for line := range strings.SplitSeq(b.String(), "\n") {
			if strings.HasPrefix(line, "| ") && strings.TrimSpace(strings.Split(line, "|")[1]) != "IP" {
				joined.WriteString(strings.TrimSpace(strings.Split(line, "|")[1]))
			}
		}
		if joined.String() != ip {
			t.Fatal(joined.String())
		}
	}
}
func TestClipGraphemes(t *testing.T) {
	if got := Clip("中文👩‍💻测试", 5); got != "中文…" {
		t.Fatal(got)
	}
	if strings.ContainsAny(Text("a\x1b\nb"), "\x1b\n") {
		t.Fatal("controls")
	}
}
