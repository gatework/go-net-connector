package display

import (
	"fmt"
	"io"
	"strings"

	"github.com/rivo/uniseg"
)

// Table writes bounded five-column inventory rows, wrapping addresses losslessly.
func Table(w io.Writer, rows [][5]string, width int) error {
	width = max(32, width)
	available := width - 17
	widths := []int{max(7, available*25/100), max(3, available*25/100), max(3, available*13/100), max(3, available*17/100), max(4, available*20/100)}
	for sum := 0; ; {
		sum = 0
		for _, n := range widths {
			sum += n
		}
		if sum <= available {
			break
		}
		for i := range widths {
			if widths[i] > 3 {
				widths[i]--
				break
			}
		}
	}
	var border strings.Builder
	border.WriteString("+")
	for _, n := range widths {
		border.WriteString(strings.Repeat("-", n+2) + "+")
	}
	border.WriteString("\n")
	writeRow := func(row [5]string) error {
		address := []string{}
		part := ""
		size := 0
		g := uniseg.NewGraphemes(Text(row[0]))
		for g.Next() {
			if size+g.Width() > widths[0] {
				address = append(address, part)
				part = ""
				size = 0
			}
			part += g.Str()
			size += g.Width()
		}
		address = append(address, part)
		for line, ip := range address {
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
	if _, err := io.WriteString(w, border.String()); err != nil {
		return err
	}
	if err := writeRow([5]string{"IP", "名称", "厂商", "型号", "状态"}); err != nil {
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
		if err := writeRow(row); err != nil {
			return err
		}
	}
	_, err := io.WriteString(w, border.String())
	return err
}
