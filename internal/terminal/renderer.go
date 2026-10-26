// Package terminal renders the subset of terminal editing used by device CLIs.
package terminal

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

var ErrLimit = errors.New("terminal: line or escape sequence exceeds limit")
var ErrEncoding = errors.New("terminal: invalid UTF-8")

type Renderer struct {
	target        io.Writer
	line          []byte
	cursor, limit int
	state         byte
	params        []byte
	strict        bool
}

func New(w io.Writer, limit int, strict bool) *Renderer {
	return &Renderer{target: w, limit: limit, strict: strict}
}
func Render(input []byte, strict bool) (string, error) {
	var b strings.Builder
	r := New(&b, 32<<20, strict)
	if _, e := r.Write(input); e != nil {
		return "", e
	}
	if e := r.Finish(); e != nil {
		return "", e
	}
	return b.String(), nil
}
func (r *Renderer) Write(p []byte) (int, error) {
	for i, b := range p {
		if r.state != 0 {
			if e := r.escape(b); e != nil {
				return i, e
			}
			continue
		}
		switch b {
		case 27:
			r.state = 'e'
		case 8:
			r.cursor = max(0, r.cursor-1)
		case 13:
			r.cursor = 0
		case 10:
			if e := r.emit(true); e != nil {
				return i, e
			}
		default:
			if b < 32 && b != 9 {
				continue
			}
			if r.cursor >= r.limit || r.cursor < 0 {
				return i, ErrLimit
			}
			// Sequential text is the common path; append directly instead of
			// allocating a one-byte padding slice for every input byte.
			if r.cursor == len(r.line) {
				r.line = append(r.line, b)
			} else if r.cursor > len(r.line) {
				r.line = append(r.line, bytes.Repeat([]byte{' '}, r.cursor-len(r.line)+1)...)
			}
			r.line[r.cursor] = b
			r.cursor++
		}
	}
	return len(p), nil
}
func (r *Renderer) escape(b byte) error {
	switch r.state {
	case 'e':
		switch b {
		case '[':
			r.state = 'c'
			r.params = r.params[:0]
		case ']':
			r.state = 'o'
		default:
			r.state = 0
		}
	case 'o':
		if b == 7 {
			r.state = 0
		} else if b == 27 {
			r.state = 's'
		}
	case 's':
		if b == '\\' {
			r.state = 0
		} else {
			r.state = 'o'
		}
	case 'c':
		if b >= 0x40 && b <= 0x7e {
			n := 1
			digits := ""
			for _, x := range r.params {
				if x >= '0' && x <= '9' {
					digits += string(x)
				} else if digits != "" {
					break
				}
			}
			if digits != "" {
				v, e := strconv.Atoi(digits)
				if e != nil {
					return ErrLimit
				}
				n = v
			}
			switch b {
			case 'C':
				if n > r.limit-r.cursor {
					return ErrLimit
				}
				r.cursor += n
			case 'D':
				r.cursor = max(0, r.cursor-n)
			case 'G':
				if n > r.limit {
					return ErrLimit
				}
				r.cursor = max(0, n-1)
			case 'K':
				if digits == "" {
					n = 0
				}
				switch n {
				case 0:
					r.line = r.line[:min(r.cursor, len(r.line))]
				case 1:
					for i := 0; i < min(r.cursor+1, len(r.line)); i++ {
						r.line[i] = ' '
					}
				case 2:
					r.line = r.line[:0]
					r.cursor = 0
				}
			}
			r.state = 0
		} else if b >= 0x20 && b <= 0x3f {
			if len(r.params) >= 64 {
				return ErrLimit
			}
			r.params = append(r.params, b)
		} else {
			r.state = 0
		}
	}
	return nil
}
func (r *Renderer) emit(newline bool) error {
	line := bytes.TrimRight(r.line, " \t")
	if !utf8.Valid(line) {
		if r.strict {
			return ErrEncoding
		}
		var b strings.Builder
		for len(line) > 0 {
			v, n := utf8.DecodeRune(line)
			if v == utf8.RuneError && n == 1 {
				fmt.Fprintf(&b, "\\x%02X", line[0])
			} else {
				b.Write(line[:n])
			}
			line = line[n:]
		}
		line = []byte(b.String())
	}
	if n, e := r.target.Write(line); e != nil {
		return e
	} else if n != len(line) {
		return io.ErrShortWrite
	}
	if newline {
		if n, e := r.target.Write([]byte{'\n'}); e != nil {
			return e
		} else if n != 1 {
			return io.ErrShortWrite
		}
	}
	r.line = r.line[:0]
	r.cursor = 0
	return nil
}
func (r *Renderer) Finish() error {
	if len(r.line) > 0 {
		return r.emit(false)
	}
	return nil
}
