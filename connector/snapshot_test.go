package connector

import (
	"regexp"
	"testing"
)

func TestParseScriptCRLFPreservesBytesAndLines(t *testing.T) {
	s, err := ParseScript("# comment\r\n\r\n show version  \r\nshow clock\r\n", "input")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Commands) != 2 || s.Commands[0].Text != " show version  " || s.Commands[0].Line != 3 || s.Commands[1].Line != 4 {
		t.Fatal("command bytes or lines changed")
	}
	if _, err := ParseScript("show\rclock\n", "input"); err == nil {
		t.Fatal("embedded CR accepted")
	}
}
func TestRegexpSnapshotPreservesMatchingMode(t *testing.T) {
	for _, r := range []*regexp.Regexp{regexp.MustCompilePOSIX("a|ab"), regexp.MustCompile("a|ab")} {
		r.Longest()
		if got := clonePattern(r).FindString("ab"); got != "ab" {
			t.Fatalf("snapshot match=%q", got)
		}
	}
	r := regexp.MustCompile("a|ab")
	snapshot := clonePattern(r)
	r.Longest()
	if got := snapshot.FindString("ab"); got != "a" {
		t.Fatal("caller changed snapshot")
	}
}
