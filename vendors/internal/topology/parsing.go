package topology

import (
	"regexp"
	"strings"
	"sync"
	"unicode"

	"github.com/gatework/go-net-connector/vendors/dialect"
)

var (
	patternCache   sync.Map
	horizontalLine = regexp.MustCompile(`^[-=+ ]+$`)
	promptLine     = regexp.MustCompile(`^(?:<[^>]+>|\[[^\]]+\])$`)
)

// Pattern compiles a family-owned parsing expression once and reuses it.
func Pattern(expression string) *regexp.Regexp {
	if cached, ok := patternCache.Load(expression); ok {
		if re, ok := cached.(*regexp.Regexp); ok {
			return re
		}
	}
	compiled := regexp.MustCompile(expression)
	actual, _ := patternCache.LoadOrStore(expression, compiled)
	re, _ := actual.(*regexp.Regexp) // the cache stores only *regexp.Regexp
	return re
}

// Field returns a text field from a TextFSM row.
func Field(row map[string]any, name string) string {
	value, _ := row[name].(string)
	return value
}

// Neighbor is one discovered link.
type Neighbor = dialect.Neighbor

// Count returns the number of matches for a family-owned record expression.
func Count(expression, text string) int {
	return len(Pattern(expression).FindAllStringIndex(text, -1))
}

// TableLines extracts nonblank rows after a recognized table header.
func TableLines(output string, header *regexp.Regexp) ([]string, bool) {
	seen := false
	lines := []string{}
	for line := range strings.SplitSeq(output, "\n") {
		if header.MatchString(line) {
			seen = true
			continue
		}
		if !seen {
			continue
		}
		line = strings.TrimSpace(line)
		if line == "" || horizontalLine.MatchString(line) || promptLine.MatchString(line) {
			continue
		}
		lines = append(lines, line)
	}
	return lines, seen
}

// DecodeQuoted decodes device configuration words while preserving quoted spaces.
func DecodeQuoted(value string, split bool) (string, error) {
	value = strings.TrimSpace(value)
	if !split && !strings.HasPrefix(value, `"`) {
		return value, nil
	}
	var w quotedWords
	for _, r := range value {
		w.next(r)
	}
	if w.quote != 0 || w.escape {
		return "", dialect.ErrParse
	}
	if w.started {
		w.words = append(w.words, w.word.String())
	}
	return strings.Join(w.words, " "), nil
}

// quotedWords splits shell-style words: single quotes are literal, double
// quotes keep backslash escapes for \, ", $, ` and newline only.
type quotedWords struct {
	words   []string
	word    strings.Builder
	quote   rune
	started bool
	escape  bool
}

// next consumes one rune.
func (w *quotedWords) next(r rune) {
	switch {
	case w.escape:
		w.escaped(r)
	case r == '\\' && w.quote != '\'':
		w.escape, w.started = true, true
	case w.quote != 0:
		if r == w.quote {
			w.quote = 0
		} else {
			w.word.WriteRune(r)
		}
	case r == '"' || r == '\'':
		w.quote, w.started = r, true
	case unicode.IsSpace(r):
		if w.started {
			w.words = append(w.words, w.word.String())
			w.word.Reset()
			w.started = false
		}
	default:
		w.word.WriteRune(r)
		w.started = true
	}
}

// escaped consumes the rune after a backslash.
func (w *quotedWords) escaped(r rune) {
	if w.quote == '"' && r != '"' && r != '\\' && r != '$' && r != '`' && r != '\n' {
		w.word.WriteRune('\\')
	}
	if r != '\n' {
		w.word.WriteRune(r)
	}
	w.escape, w.started = false, true
}
