package terminal

import (
	"io"
	"strings"
	"testing"
)

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) { return 0, nil }
func TestRenderRejectsShortOutput(t *testing.T) {
	r := New(shortWriter{}, 1024, true)
	_, e := r.Write([]byte("body\n"))
	if e != io.ErrShortWrite {
		t.Fatal(e)
	}
}

func TestRenderEditsAndSplitEscapes(t *testing.T) {
	var b strings.Builder
	r := New(&b, 32<<20, true)
	for _, s := range []string{"abc\rXY", "\x1b[", "K\n你好", "\x1b]0;title", "\x1b\\\n"} {
		if _, e := r.Write([]byte(s)); e != nil {
			t.Fatal(e)
		}
	}
	if e := r.Finish(); e != nil {
		t.Fatal(e)
	}
	if b.String() != "XY\n你好\n" {
		t.Fatalf("%q", b.String())
	}
}
func TestRenderLimitsAndEncoding(t *testing.T) {
	for _, s := range []string{"\xff", "a\b\xff"} {
		if _, e := Render([]byte(s), true); e == nil {
			t.Fatal("invalid encoding accepted")
		}
	}
	var b strings.Builder
	r := New(&b, 3, true)
	if _, e := r.Write([]byte("1234")); e == nil {
		t.Fatal("limit ignored")
	}
}
