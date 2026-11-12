package topology

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gatework/go-net-connector/internal/ifname"
)

// InterfaceKey returns the canonical key that identifies an interface name
// regardless of abbreviation.
func InterfaceKey(name string) string { return ifname.Key(name) }

// ShortInterface abbreviates an interface name, optionally in lower case.
func ShortInterface(name string, lowercase bool) string {
	n, _ := ifname.Short(name)
	if lowercase {
		n = strings.ToLower(n)
	}
	return n
}

// EquivalentDescription reports whether two descriptions match ignoring case
// and interface name abbreviation in the trailing port.
func EquivalentDescription(current, expected string) bool {
	if strings.EqualFold(current, expected) {
		return true
	}
	i, j := strings.LastIndex(current, "_"), strings.LastIndex(expected, "_")
	if i < 0 || j < 0 || !strings.EqualFold(current[:i], expected[:j]) {
		return false
	}
	a, ok := ifname.Canonical(current[i+1:])
	b, other := ifname.Canonical(expected[j+1:])
	return ok && other && strings.EqualFold(a, b)
}

// ValidateDescription accepts 1 to 80 letters, digits and "_.: /-".
func ValidateDescription(value string) error {
	if len(value) < 1 || len(value) > 80 || !utf8.ValidString(value) {
		return ErrInvalid
	}
	for _, r := range value {
		if !unicode.IsLetter(r) && !unicode.IsNumber(r) && !strings.ContainsRune("_.: /-", r) {
			return ErrInvalid
		}
	}
	return nil
}

var safeInterface = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9.:/-]*$`)

// ValidateInterface accepts a configuration-safe interface name.
func ValidateInterface(value string) error {
	if !safeInterface.MatchString(value) {
		return ErrInvalid
	}
	return nil
}

// FormatDescription returns the expected description "To_<neighbor>_<port>"
// for peer, or the custom formatter's result.
func FormatDescription(peer Neighbor, opts FormatOptions) (string, error) {
	port := peer.NeighborInterface
	if !opts.FullInterfaceNames {
		port = ShortInterface(port, opts.Lowercase)
	} else if opts.Lowercase {
		port = strings.ToLower(port)
	}
	v := "To_" + strings.ReplaceAll(peer.NeighborName, " ", "_") + "_" + strings.ReplaceAll(port, " ", "_")
	return v, ValidateDescription(v)
}
