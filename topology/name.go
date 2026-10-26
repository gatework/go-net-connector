package topology

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

type nameRule struct {
	pattern    *regexp.Regexp
	key, short string
}

func rule(pattern, key, short string) nameRule {
	return nameRule{regexp.MustCompile("(?i)^(?:" + pattern + ")([0-9])"), key, short}
}

var nameRules = []nameRule{
	rule("bridge-aggregation|bagg", "bridge-aggregation", "BAGG"), rule("route-aggregation|ragg", "route-aggregation", "RAGG"), rule("aggregate|agg", "aggregate", "Agg"),
	rule("hundredgigabitethernet|hundredgige|hge|100ge|hu", "hundredgigabitethernet", "Hu"), rule("fortygigabitethernet|fortygige|fge|40ge|fo", "fortygigabitethernet", "Fo"), rule("twenty-?fivegigabitethernet|twenty-?fivegige|wge|25ge|twe", "twentyfivegigabitethernet", "Twe"),
	rule("xge|10ge|xgigabitethernet|ten-gigabitethernet|tengigabitethernet|ten-?gige|xethernet|xe|te", "tengigabitethernet", "Te"), rule("smartrate-ethernet|se", "smartrate-ethernet", "SE"), rule("gigabitethernet|ge|gi|g", "gigabitethernet", "Gi"), rule("fastethernet|fa", "fastethernet", "Fa"), rule("ethernet|eth", "ethernet", "Eth"), rule("port-channel|po", "port-channel", "Po"),
}

func normalizeName(name string, short bool) (string, bool) {
	for _, r := range nameRules {
		if m := r.pattern.FindStringIndex(name); m != nil {
			prefix := r.key
			if short {
				prefix = r.short
			}
			return prefix + name[m[1]-1:], true
		}
	}
	return name, false
}
func InterfaceKey(name string) string { n, _ := normalizeName(name, false); return strings.ToLower(n) }
func ShortInterface(name string, lowercase bool) string {
	n, _ := normalizeName(name, true)
	if lowercase {
		n = strings.ToLower(n)
	}
	return n
}
func ConfigurationInterface(name string) string {
	for _, r := range []nameRule{rule("SE", "Smartrate-Ethernet", ""), rule("XGE", "Ten-GigabitEthernet", ""), rule("GE|Gi|G", "GigabitEthernet", ""), rule("Te", "TenGigabitEthernet", ""), rule("Fa", "FastEthernet", ""), rule("Eth", "Ethernet", ""), rule("Po", "port-channel", "")} {
		if m := r.pattern.FindStringIndex(name); m != nil {
			return r.key + name[m[1]-1:]
		}
	}
	return name
}
func EquivalentDescription(current, expected string) bool {
	if strings.EqualFold(current, expected) {
		return true
	}
	i, j := strings.LastIndex(current, "_"), strings.LastIndex(expected, "_")
	if i < 0 || j < 0 || !strings.EqualFold(current[:i], expected[:j]) {
		return false
	}
	a, ok := normalizeName(current[i+1:], false)
	b, other := normalizeName(expected[j+1:], false)
	return ok && other && strings.EqualFold(a, b)
}
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

func ValidateInterface(value string) error {
	if !safeInterface.MatchString(value) {
		return ErrInvalid
	}
	return nil
}
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

var unknown = regexp.MustCompile(`(?i)^(?:-+|n/?a|unknown|none|null|not\s+(?:available|advertised))?$`)

func (n Neighbor) IdentityIssue() string {
	if unknown.MatchString(strings.TrimSpace(n.NeighborName)) {
		return "unknown_neighbor_name"
	}
	if unknown.MatchString(strings.TrimSpace(n.NeighborInterface)) {
		return "unknown_neighbor_interface"
	}
	return ""
}
