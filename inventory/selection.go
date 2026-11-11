// Package inventory filters existing device records without discovery or I/O.
package inventory

import (
	"errors"
	"net/netip"
	"path"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

var ErrInvalid = errors.New("inventory: invalid selection")

type Device struct {
	IP        string `json:"ip" yaml:"ip"`
	Name      string `json:"name" yaml:"name"`
	DNS       string `json:"dns" yaml:"dns"`
	Vendor    string `json:"vendor" yaml:"vendor"`
	OS        string `json:"os" yaml:"os"`
	Model     string `json:"model" yaml:"model"`
	OSVersion string `json:"os_ver" yaml:"os_ver"`
	Serial    string `json:"serial" yaml:"serial"`
}
type interval struct{ first, last netip.Addr }
type AddressSet struct {
	exact     map[netip.Addr]struct{}
	intervals []interval
}

// NewAddressSet parses exact addresses, CIDR prefixes and first-last
// ranges (an IPv4 range may abbreviate the last address to its final
// octet). Overlapping and adjacent ranges are merged.
func NewAddressSet(values []string) (AddressSet, error) {
	s := AddressSet{exact: make(map[netip.Addr]struct{})}
	for _, value := range values {
		if value == "" {
			return s, ErrInvalid
		}
		if strings.Contains(value, "/") {
			r, e := prefixInterval(value)
			if e != nil {
				return s, e
			}
			s.intervals = append(s.intervals, r)
			continue
		}
		if strings.Contains(strings.SplitN(value, "%", 2)[0], "-") {
			r, e := rangeInterval(value)
			if e != nil {
				return s, e
			}
			s.intervals = append(s.intervals, r)
			continue
		}
		address, e := netip.ParseAddr(value)
		if e != nil || address.Zone() != "" {
			return s, ErrInvalid
		}
		s.exact[address] = struct{}{}
	}
	s.intervals = mergeIntervals(s.intervals)
	return s, nil
}

// prefixInterval returns the addresses a CIDR prefix covers.
func prefixInterval(value string) (interval, error) {
	prefix, e := netip.ParsePrefix(value)
	if e != nil {
		return interval{}, ErrInvalid
	}
	prefix = prefix.Masked()
	first := prefix.Addr()
	if first.Is4() {
		b := first.As4()
		for i := prefix.Bits(); i < 32; i++ {
			b[i/8] |= 1 << uint(7-i%8)
		}
		return interval{first, netip.AddrFrom4(b)}, nil
	}
	b := first.As16()
	for i := prefix.Bits(); i < 128; i++ {
		b[i/8] |= 1 << uint(7-i%8)
	}
	return interval{first, netip.AddrFrom16(b)}, nil
}

// rangeInterval parses "first-last", completing an IPv4 last address given
// as a final octet.
func rangeInterval(value string) (interval, error) {
	parts := strings.SplitN(value, "-", 2)
	first, e := netip.ParseAddr(parts[0])
	if e != nil {
		return interval{}, ErrInvalid
	}
	if first.Is4() && !strings.Contains(parts[1], ".") {
		octets := strings.Split(first.String(), ".")
		parts[1] = strings.Join(octets[:3], ".") + "." + parts[1]
	}
	last, e := netip.ParseAddr(parts[1])
	if e != nil || first.BitLen() != last.BitLen() || first.Zone() != "" || last.Zone() != "" || first.Compare(last) > 0 {
		return interval{}, ErrInvalid
	}
	return interval{first, last}, nil
}

// mergeIntervals sorts intervals and merges those of one address family
// that overlap or touch.
func mergeIntervals(intervals []interval) []interval {
	sort.Slice(intervals, func(i, j int) bool { return intervals[i].first.Compare(intervals[j].first) < 0 })
	merged := make([]interval, 0, len(intervals))
	for _, v := range intervals {
		if len(merged) > 0 {
			last := &merged[len(merged)-1]
			adj := last.last.Next()
			if v.first.BitLen() == last.first.BitLen() && (v.first.Compare(last.last) <= 0 || (adj.IsValid() && v.first == adj)) {
				if v.last.Compare(last.last) > 0 {
					last.last = v.last
				}
				continue
			}
		}
		merged = append(merged, v)
	}
	return merged
}

func (s AddressSet) Empty() bool { return len(s.exact) == 0 && len(s.intervals) == 0 }
func (s AddressSet) Contains(host string) bool {
	a, e := netip.ParseAddr(host)
	if e != nil {
		return false
	}
	if _, ok := s.exact[a]; ok {
		return true
	}
	if a.Zone() != "" {
		a = a.WithZone("")
	}
	i := sort.Search(len(s.intervals), func(i int) bool { return s.intervals[i].last.Compare(a) >= 0 })
	return i < len(s.intervals) && s.intervals[i].first.BitLen() == a.BitLen() && s.intervals[i].first.Compare(a) <= 0
}

type SelectionOptions struct {
	IncludeHosts, ExcludeHosts, IncludeNames, ExcludeNames, IncludeVendors, ExcludeVendors, IncludeModels, ExcludeModels []string
	NameMatch, ModelMatch                                                                                                string
}
type textRules struct {
	include, exclude []string
	mode             MatchMode
	globs            map[string]string
}
type Selection struct {
	includeHosts, excludeHosts AddressSet
	names, models, vendors     textRules
}
type Decision struct {
	Reasons []string `json:"reasons"`
}

func (d Decision) Selected() bool { return len(d.Reasons) == 0 }
func NewSelection(o SelectionOptions) (Selection, error) {
	var s Selection
	var e error
	s.includeHosts, e = NewAddressSet(o.IncludeHosts)
	if e != nil {
		return s, e
	}
	s.excludeHosts, e = NewAddressSet(o.ExcludeHosts)
	if e != nil {
		return s, e
	}
	makeRules := func(in, out []string, mode MatchMode) (textRules, error) {
		if mode == "" {
			mode = MatchContains
		}
		if !mode.Valid() {
			return textRules{}, ErrInvalid
		}
		r := textRules{mode: mode, globs: map[string]string{}}
		for idx, list := range [][]string{in, out} {
			for _, v := range list {
				if !utf8.ValidString(v) || strings.TrimSpace(v) == "" || strings.IndexFunc(v, unicode.IsControl) >= 0 {
					return r, ErrInvalid
				}
				if mode == MatchGlob {
					r.globs[strings.ToLower(v)] = translateGlob(strings.ToLower(v))
				}
				if idx == 0 {
					r.include = append(r.include, strings.ToLower(v))
				} else {
					r.exclude = append(r.exclude, strings.ToLower(v))
				}
			}
		}
		return r, nil
	}
	s.names, e = makeRules(o.IncludeNames, o.ExcludeNames, MatchMode(o.NameMatch))
	if e != nil {
		return s, e
	}
	s.models, e = makeRules(o.IncludeModels, o.ExcludeModels, MatchMode(o.ModelMatch))
	if e != nil {
		return s, e
	}
	s.vendors, e = makeRules(o.IncludeVendors, o.ExcludeVendors, MatchExact)
	return s, e
}
// Evaluate returns the reasons d is not selected; none means selected.
func (s Selection) Evaluate(d Device) Decision {
	reasons := make([]string, 0, 4)
	if s.excludeHosts.Contains(d.IP) {
		reasons = append(reasons, "host_excluded")
	} else if !s.includeHosts.Empty() && !s.includeHosts.Contains(d.IP) {
		reasons = append(reasons, "host_not_included")
	}
	for _, field := range []struct {
		name, value string
		rules       textRules
	}{{"vendor", d.Vendor, s.vendors}, {"name", d.Name, s.names}, {"model", d.Model, s.models}} {
		if reason := field.rules.reason(field.name, field.value); reason != "" {
			reasons = append(reasons, reason)
		}
	}
	return Decision{reasons}
}

// reason returns why value fails the rules for the named field, or "".
func (r textRules) reason(name, value string) string {
	value = strings.ToLower(strings.ToValidUTF8(value, ""))
	if r.matches(value, r.exclude) {
		return name + "_excluded"
	}
	if len(r.include) > 0 && !r.matches(value, r.include) {
		return name + "_not_included"
	}
	return ""
}

// matches reports whether the lower-case value matches any pattern.
func (r textRules) matches(value string, patterns []string) bool {
	globValue := value
	if r.mode == MatchGlob {
		globValue = strings.ReplaceAll(value, "/", "\x00")
	}
	for _, p := range patterns {
		if r.match(value, globValue, p) {
			return true
		}
	}
	return false
}

// match reports whether value matches pattern p under the rules' mode. A
// glob does not match a leading dot unless the pattern starts with one.
func (r textRules) match(value, globValue, p string) bool {
	switch r.mode {
	case MatchExact:
		return value == p
	case MatchContains:
		return strings.Contains(value, p)
	case MatchPrefix:
		return strings.HasPrefix(value, p)
	case MatchGlob:
		if strings.HasPrefix(value, ".") && !strings.HasPrefix(p, ".") {
			return false
		}
		ok, e := path.Match(r.globs[p], globValue)
		return e == nil && ok
	default:
		return false
	}
}

func translateGlob(pattern string) string {
	// Glob semantics: '*' may cross '/', and both [!...] and [^...] negate a
	// class. Escapes are respected.
	translated := make([]byte, 0, len(pattern))
	inClass := false
	for i := 0; i < len(pattern); i++ {
		if pattern[i] == '\\' {
			translated = append(translated, pattern[i])
			if i+1 < len(pattern) {
				i++
				translated = append(translated, pattern[i])
			}
			continue
		}
		if pattern[i] == ']' && inClass {
			inClass = false
			translated = append(translated, pattern[i])
			continue
		}
		if pattern[i] == '[' && !inClass {
			if i+2 < len(pattern) && pattern[i+1] == '!' && pattern[i+2] == ']' {
				// An empty negated class matches any single character.
				translated = append(translated, '?')
				i += 2
				continue
			}
			inClass = true
			translated = append(translated, '[')
			if i+2 < len(pattern) && pattern[i+1] == '!' {
				translated = append(translated, '^')
				i++
			}
			continue
		}
		translated = append(translated, pattern[i])
	}
	return strings.ReplaceAll(string(translated), "/", "\x00")
}
