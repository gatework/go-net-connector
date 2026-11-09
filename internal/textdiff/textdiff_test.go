package textdiff

import (
	"strings"
	"testing"
)

func apply(a []string, ops []Op) []string {
	out := []string{}
	i := 0
	for _, op := range ops {
		switch op.Kind {
		case ' ':
			out = append(out, a[i])
			i++
		case '-':
			i++
		case '+':
			out = append(out, op.Line)
		}
	}
	return out
}

func TestDiffReproducesTarget(t *testing.T) {
	for _, tc := range []struct{ a, b string }{
		{"", ""}, {"a\n", ""}, {"", "a\n"}, {"a\nb\nc\n", "a\nb\nc\n"},
		{"a\nb\nc\n", "a\nx\nc\n"}, {"hostname r1\ninterface Gi0/1\n description old\n", "hostname r1\ninterface Gi0/1\n description new\n shutdown\n"},
		{"1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n", "0\n1\n2\n3\n5\n6\n7\n8\n9\n10\n11\n"},
	} {
		a, b := Lines(tc.a), Lines(tc.b)
		ops := Diff(a, b)
		if got := apply(a, ops); strings.Join(got, "\n") != strings.Join(b, "\n") {
			t.Fatalf("%q -> %q: got %q", tc.a, tc.b, got)
		}
	}
}

func TestUnifiedFormat(t *testing.T) {
	a := Lines("hostname r1\n!\ninterface Gi0/1\n description old\n!\nend\n")
	b := Lines("hostname r1\n!\ninterface Gi0/1\n description new\n!\nend\n")
	want := "--- from\n+++ to\n@@ -2,5 +2,5 @@\n !\n interface Gi0/1\n- description old\n+ description new\n !\n end\n"
	if got := Unified("from", "to", Diff(a, b), 2); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if got := Unified("from", "to", Diff(a, a), 3); got != "" {
		t.Fatalf("identical produced %q", got)
	}
	added, removed := Stats(Diff(a, b))
	if added != 1 || removed != 1 {
		t.Fatal(added, removed)
	}
}

func TestUnifiedSeparatesDistantHunks(t *testing.T) {
	var a, b []string
	for i := range 40 {
		line := "line " + string(rune('A'+i%26))
		a, b = append(a, line), append(b, line)
	}
	b[2], b[35] = "changed early", "changed late"
	if got := strings.Count(Unified("a", "b", Diff(a, b), 3), "@@ -"); got != 2 {
		t.Fatalf("hunks = %d", got)
	}
}
