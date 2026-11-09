package driver

import "strings"

// ReadOnlyCommands classifies commands that only read device state, by their
// first word (or menu path prefix, for path-style CLIs). A command with a
// pipe is read-only only when every filter is a known display filter, so
// output redirection such as "| redirect", "| tee", "| append" or Junos
// "| save" is rejected, as is any ">" redirection.
type ReadOnlyCommands struct {
	Verbs []string
	// PathPrefixes match path-style menus such as Radware's "/info".
	PathPrefixes []string
}

var displayFilters = map[string]bool{
	"include": true, "inc": true, "i": true, "exclude": true, "exc": true, "e": true,
	"begin": true, "b": true, "section": true, "sec": true, "count": true,
	"match": true, "except": true, "no-more": true, "display": true, "find": true,
}

// IsReadOnly reports whether command can only read device state.
func (r ReadOnlyCommands) IsReadOnly(command string) bool {
	text := strings.ToLower(strings.TrimSpace(command))
	if text == "" || strings.ContainsAny(text, "><;") {
		return false
	}
	segments := strings.Split(text, "|")
	head := strings.Fields(segments[0])
	if len(head) == 0 {
		return false
	}
	allowed := false
	for _, verb := range r.Verbs {
		allowed = allowed || head[0] == verb
	}
	for _, prefix := range r.PathPrefixes {
		allowed = allowed || head[0] == prefix || strings.HasPrefix(head[0], prefix+"/")
	}
	if !allowed {
		return false
	}
	for _, filter := range segments[1:] {
		words := strings.Fields(filter)
		if len(words) == 0 || !displayFilters[words[0]] {
			return false
		}
		if words[0] == "display" && len(words) > 1 && words[1] != "set" && words[1] != "xml" && words[1] != "json" {
			return false
		}
	}
	return true
}
