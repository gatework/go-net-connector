package display

import (
	"strings"
	"unicode"

	"github.com/rivo/uniseg"
)

// Text removes terminal control characters from untrusted labels.
func Text(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, strings.ToValidUTF8(s, "�"))
}

// Clip limits terminal cells without splitting a grapheme cluster.
func Clip(s string, width int) string {
	s = Text(s)
	if width <= 0 {
		return ""
	}
	if uniseg.StringWidth(s) <= width {
		return s
	}
	var b strings.Builder
	g := uniseg.NewGraphemes(s)
	used := 0
	for g.Next() {
		w := g.Width()
		if used+w > width-1 {
			break
		}
		b.WriteString(g.Str())
		used += w
	}
	return b.String() + "…"
}
