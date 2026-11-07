package textcodec

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// BackupView is a preservation view, never input for strict configuration parsers.
// Complete lines are used after native terminal editing. Existing valid UTF-8
// wins over a legacy candidate; only undecodable bytes are escaped.
func BackupView(input string) (text, encoding string, escaped int) {
	text, encoding, escaped, _ = backupView(input, false)
	return
}

// BackupViewSource also records the first 64 escaped source-byte offsets.
// Counting and positioning share the same decoding decisions as the view.
func BackupViewSource(input string) (text, encoding string, escaped int, offsets []int) {
	return backupView(input, true)
}

func backupView(input string, positions bool) (text, encoding string, escaped int, offsets []int) {
	var out strings.Builder
	legacy, unicode := false, false
	offset := 0
	for line := range strings.SplitAfterSeq(input, "\n") {
		offset += len(line)
		end := offset
		if utf8.ValidString(line) {
			out.WriteString(line)
			if hasUTF8Multibyte(line) {
				unicode = true
			}
			continue
		}
		if !hasUTF8Multibyte(line) {
			if converted, detected, err := Decode(line, "auto"); err == nil {
				out.WriteString(converted)
				legacy = legacy || detected != "utf-8"
				continue
			}
		}
		for len(line) > 0 {
			r, n := utf8.DecodeRuneInString(line)
			if r == utf8.RuneError && n == 1 {
				if positions && len(offsets) < 64 {
					offsets = append(offsets, end-len(line))
				}
				fmt.Fprintf(&out, "\\x%02X", line[0])
				escaped++
			} else {
				out.WriteString(line[:n])
				unicode = unicode || n > 1
			}
			line = line[n:]
		}
	}
	encoding = "utf-8"
	if legacy {
		encoding = "gb18030"
		if unicode {
			encoding = "mixed"
		}
	}
	if escaped > 0 {
		encoding = "escaped"
	}
	return out.String(), encoding, escaped, offsets
}
func hasUTF8Multibyte(input string) bool {
	for len(input) > 0 {
		_, n := utf8.DecodeRuneInString(input)
		if n > 1 {
			return true
		}
		input = input[n:]
	}
	return false
}
