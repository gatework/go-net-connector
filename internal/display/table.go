package display

import (
	"fmt"
	"io"
	"strings"

	"github.com/rivo/uniseg"
)

// Table writes bounded five-column inventory rows, wrapping addresses losslessly.
func Table(w io.Writer, rows [][5]string, width int) error {
	widths := columnWidths(width)
	var border strings.Builder
	border.WriteString("+")
	for _, n := range widths {
		border.WriteString(strings.Repeat("-", n+2) + "+")
	}
	border.WriteString("\n")
	if _, err := io.WriteString(w, border.String()); err != nil {
		return err
	}
	if err := writeTableRow(w, widths, [5]string{"IP", "名称", "厂商", "型号", "状态"}); err != nil {
		return err
	}
	if _, err := io.WriteString(w, border.String()); err != nil {
		return err
	}
	for _, row := range rows {
		for i, v := range row {
			if v == "" {
				row[i] = "-"
			}
		}
		if err := writeTableRow(w, widths, row); err != nil {
			return err
		}
	}
	_, err := io.WriteString(w, border.String())
	return err
}

// columnWidths shares the content width of a table width wide among the
// five columns, shrinking the leftmost wide columns until they fit.
func columnWidths(width int) [5]int {
	available := max(32, width) - 17
	widths := [5]int{max(7, available*25/100), max(3, available*25/100), max(3, available*13/100), max(3, available*17/100), max(4, available*20/100)}
	for {
		sum := 0
		for _, n := range widths {
			sum += n
		}
		if sum <= available {
			return widths
		}
		for i := range widths {
			if widths[i] > 3 {
				widths[i]--
				break
			}
		}
	}
}

// writeTableRow writes row, wrapping the address column over as many lines
// as it needs and clipping the other columns.
func writeTableRow(w io.Writer, widths [5]int, row [5]string) error {
	for line, ip := range wrapGraphemes(Text(row[0]), widths[0]) {
		var s strings.Builder
		s.WriteString("|")
		for i, value := range row {
			if line > 0 {
				value = ""
			}
			if i == 0 {
				value = ip
			}
			v := Clip(value, widths[i])
			s.WriteString(" " + v + strings.Repeat(" ", widths[i]-uniseg.StringWidth(v)) + " |")
		}
		if _, err := fmt.Fprintln(w, s.String()); err != nil {
			return err
		}
	}
	return nil
}

// wrapGraphemes splits text into lines at most width cells wide without
// splitting a grapheme.
func wrapGraphemes(text string, width int) []string {
	var lines []string
	part := ""
	size := 0
	g := uniseg.NewGraphemes(text)
	for g.Next() {
		if size+g.Width() > width {
			lines = append(lines, part)
			part = ""
			size = 0
		}
		part += g.Str()
		size += g.Width()
	}
	return append(lines, part)
}
