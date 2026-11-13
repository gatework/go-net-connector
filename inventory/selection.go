// Package inventory filters existing device records without discovery or I/O.
package inventory

import (
	"errors"
	"net/netip"
	"path"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ErrInvalid identifies invalid address or text selection rules.
var ErrInvalid = errors.New("inventory: invalid selection")

// Device contains the inventory facts used for selection and platform resolution.
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

// AddressSet contains exact addresses and normalized closed address intervals.
type AddressSet struct {
	exact     map[netip.Addr]struct{}
	intervals []interval
}

// NewAddressSet parses addresses, prefixes and closed ranges into a normalized set.
func NewAddressSet(values []string) (AddressSet, error) {
	s := AddressSet{exact: make(map[netip.Addr]struct{})}
	for _, value := range values {
		if value == "" {
			return s, ErrInvalid
		}
		if span, matched, err := parseAddressInterval(value); matched {
			if err != nil {
				return s, err
			}
			s.intervals = append(s.intervals, span)
			continue
		}
		address, e := netip.ParseAddr(value)
		if e != nil || address.Zone() != "" {
			return s, ErrInvalid
		}
		s.exact[address] = struct{}{}
	}
	mergeAddressIntervals(&s)
	return s, nil
}
func parseAddressInterval(value string) (interval, bool, error) {
	if strings.Contains(value, "/") {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return interval{}, true, ErrInvalid
		}
		prefix = prefix.Masked()
		first := prefix.Addr()
		return interval{first, prefixLast(prefix)}, true, nil
	}
	if strings.Contains(strings.SplitN(value, "%", 2)[0], "-") {
		parts := strings.SplitN(value, "-", 2)
		first, err := netip.ParseAddr(parts[0])
		if err != nil {
			return interval{}, true, ErrInvalid
		}
		if first.Is4() && !strings.Contains(parts[1], ".") {
			octets := strings.Split(first.String(), ".")
			parts[1] = strings.Join(octets[:3], ".") + "." + parts[1]
		}
		last, err := netip.ParseAddr(parts[1])
		if err != nil || first.BitLen() != last.BitLen() || first.Zone() != "" || last.Zone() != "" || first.Compare(last) > 0 {
			return interval{}, true, ErrInvalid
		}
		return interval{first, last}, true, nil
	}
	return interval{}, false, nil
}

func prefixLast(prefix netip.Prefix) netip.Addr {
	first := prefix.Addr()
	if first.Is4() {
		b := first.As4()
		for i := prefix.Bits(); i < 32; i++ {
			b[i/8] |= 1 << uint(7-i%8)
		}
		return netip.AddrFrom4(b)
	}
	b := first.As16()
	for i := prefix.Bits(); i < 128; i++ {
		b[i/8] |= 1 << uint(7-i%8)
	}
	return netip.AddrFrom16(b)
}

// Empty reports whether the set contains no addresses.
func (s AddressSet) Empty() bool { return len(s.exact) == 0 && len(s.intervals) == 0 }

// Contains reports whether an address belongs to the set.
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

func mergeAddressIntervals(s *AddressSet) {
	slices.SortFunc(s.intervals, func(a, b interval) int { return a.first.Compare(b.first) })
	merged := make([]interval, 0, len(s.intervals))
	for _, v := range s.intervals {
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
	s.intervals = merged
}

// SelectionOptions defines include and exclude rules for inventory fields.
type SelectionOptions struct {
	IncludeHosts, ExcludeHosts, IncludeNames, ExcludeNames, IncludePlatforms, ExcludePlatforms, IncludeModels, ExcludeModels []string
	NameMatch, ModelMatch                                                                                                    string
}
type textRules struct {
	include, exclude []string
	mode             MatchMode
	globs            map[string]string
}

// Selection owns validated address and text filters.
type Selection struct {
	includeHosts, excludeHosts AddressSet
	names, models, vendors     textRules
}

// Decision records the reasons an inventory device was excluded.
type Decision struct {
	Reasons []string `json:"reasons"`
}

// Selected reports whether no exclusion reason was recorded.
func (d Decision) Selected() bool { return len(d.Reasons) == 0 }

// NewSelection validates and owns the supplied inventory filters.
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
	s.vendors, e = makeRules(o.IncludePlatforms, o.ExcludePlatforms, MatchExact)
	return s, e
}

// Evaluate returns exclusion reasons in host, vendor, name and model order.
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

		if field.rules.matches(field.value, field.rules.exclude) {
			reasons = append(reasons, field.name+"_excluded")
		} else if len(field.rules.include) > 0 && !field.rules.matches(field.value, field.rules.include) {
			reasons = append(reasons, field.name+"_not_included")
		}
	}
	return Decision{reasons}
}
func (r textRules) matches(text string, list []string) bool {
	value := strings.ToLower(strings.ToValidUTF8(text, ""))
	globValue := value
	if r.mode == MatchGlob {
		globValue = strings.ReplaceAll(value, "/", "\x00")
	}
	for _, pattern := range list {
		switch r.mode {
		case MatchExact:
			if value == pattern {
				return true
			}
		case MatchContains:
			if strings.Contains(value, pattern) {
				return true
			}
		case MatchPrefix:
			if strings.HasPrefix(value, pattern) {
				return true
			}
		case MatchGlob:
			if !strings.HasPrefix(value, ".") || strings.HasPrefix(pattern, ".") {
				ok, err := path.Match(r.globs[pattern], globValue)
				if err == nil && ok {
					return true
				}
			}
		}
	}
	return false
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
