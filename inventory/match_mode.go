package inventory

// MatchMode selects how name and model filters compare device fields. Its
// values are part of the configuration and CLI contracts.
type MatchMode string

const (
	MatchContains MatchMode = "contains"
	MatchPrefix   MatchMode = "prefix"
	MatchGlob     MatchMode = "glob"
	// MatchExact is used for vendor filters; it is not a user-selectable
	// name or model mode.
	MatchExact MatchMode = "exact"
)

// MatchModes returns the user-selectable name and model match modes.
func MatchModes() []string {
	return []string{string(MatchContains), string(MatchPrefix), string(MatchGlob)}
}

// Valid reports whether m is a known match mode.
func (m MatchMode) Valid() bool {
	return m == MatchContains || m == MatchPrefix || m == MatchGlob || m == MatchExact
}
