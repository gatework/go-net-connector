package display

import (
	"io"
	"os"
	"strings"

	"github.com/rivo/uniseg"
	"golang.org/x/term"
)

// Section separates semantic groups without exceeding the terminal width.
func Section(w io.Writer, title string) string {
	width := 72
	if file, ok := w.(*os.File); ok {
		if n, _, err := term.GetSize(int(file.Fd())); err == nil && n > 1 {
			width = min(n-1, 120)
		}
	}
	if width < 7 {
		return strings.Repeat("-", width) + "\n"
	}
	line := "-- " + Clip(title, width-6) + " "
	return line + strings.Repeat("-", max(0, width-uniseg.StringWidth(line))) + "\n"
}
