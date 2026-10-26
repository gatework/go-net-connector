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

func NewAddressSet(values []string) (AddressSet, error) {
	s := AddressSet{exact: make(map[netip.Addr]struct{})}
	for _, value := range values {
		if value == "" {
			return s, ErrInvalid
		}
		if strings.Contains(value, "/") {
			prefix, e := netip.ParsePrefix(value)
			if e != nil {
				return s, ErrInvalid
			}
			prefix = prefix.Masked()
			first := prefix.Addr()
			var last netip.Addr
			if first.Is4() {
				b := first.As4()
				for i := prefix.Bits(); i < 32; i++ {
					b[i/8] |= 1 << uint(7-i%8)
				}
				last = netip.AddrFrom4(b)
			} else {
				b := first.As16()
				for i := prefix.Bits(); i < 128; i++ {
					b[i/8] |= 1 << uint(7-i%8)
				}
				last = netip.AddrFrom16(b)
			}
			s.intervals = append(s.intervals, interval{first, last})
			continue
		}
		if strings.Contains(strings.SplitN(value, "%", 2)[0], "-") {
			parts := strings.SplitN(value, "-", 2)
			first, e := netip.ParseAddr(parts[0])
			if e != nil {
				return s, ErrInvalid
			}
			if first.Is4() && !strings.Contains(parts[1], ".") {
				octets := strings.Split(first.String(), ".")
				parts[1] = strings.Join(octets[:3], ".") + "." + parts[1]
			}
			last, e := netip.ParseAddr(parts[1])
			if e != nil || first.BitLen() != last.BitLen() || first.Zone() != "" || last.Zone() != "" || first.Compare(last) > 0 {
				return s, ErrInvalid
			}
			s.intervals = append(s.intervals, interval{first, last})
			continue
		}
		address, e := netip.ParseAddr(value)
		if e != nil {
			return s, ErrInvalid
		}
		s.exact[address] = struct{}{}
	}
	sort.Slice(s.intervals, func(i, j int) bool { return s.intervals[i].first.Compare(s.intervals[j].first) < 0 })
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
	return s, nil
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
	mode             string
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
	makeRules := func(in, out []string, mode string) (textRules, error) {
		if mode == "" {
			mode = "contains"
		}
		if mode != "contains" && mode != "prefix" && mode != "glob" && mode != "exact" {
			return textRules{}, ErrInvalid
		}
		r := textRules{mode: mode}
		for idx, list := range [][]string{in, out} {
			for _, v := range list {
				if !utf8.ValidString(v) || strings.TrimSpace(v) == "" || strings.IndexFunc(v, unicode.IsControl) >= 0 {
					return r, ErrInvalid
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
	s.names, e = makeRules(o.IncludeNames, o.ExcludeNames, o.NameMatch)
	if e != nil {
		return s, e
	}
	s.models, e = makeRules(o.IncludeModels, o.ExcludeModels, o.ModelMatch)
	if e != nil {
		return s, e
	}
	s.vendors, e = makeRules(o.IncludeVendors, o.ExcludeVendors, "exact")
	return s, e
}
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
		value := strings.ToLower(strings.ToValidUTF8(field.value, ""))
		matches := func(list []string) bool {
			for _, p := range list {
				switch field.rules.mode {
				case "exact":
					if value == p {
						return true
					}
				case "contains":
					if strings.Contains(value, p) {
						return true
					}
				case "prefix":
					if strings.HasPrefix(value, p) {
						return true
					}
				case "glob":
					if glob(p, value) {
						return true
					}
				}
			}
			return false
		}
		if matches(field.rules.exclude) {
			reasons = append(reasons, field.name+"_excluded")
		} else if len(field.rules.include) > 0 && !matches(field.rules.include) {
			reasons = append(reasons, field.name+"_not_included")
		}
	}
	return Decision{reasons}
}
func glob(pattern, value string) bool {
	if strings.HasPrefix(value, ".") && !strings.HasPrefix(pattern, ".") {
		return false
	}
	// Ruby fnmatch without PATHNAME permits '*' to cross '/'.
	// Ruby accepts both [!...] and [^...] for class negation. Respect escapes.
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
				// Ruby treats the empty negated class as any single character.
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
	p := strings.ReplaceAll(string(translated), "/", "\x00")
	v := strings.ReplaceAll(value, "/", "\x00")
	ok, e := path.Match(p, v)
	return e == nil && ok
}
