package inventory

import (
	"fmt"
	"reflect"
	"testing"
)

func TestAddressSetDoesNotExpandNetworks(t *testing.T) {
	set, e := NewAddressSet([]string{"2001:db8::/32", "192.0.2.10-20", "198.51.100.0/24"})
	if e != nil {
		t.Fatal(e)
	}
	for _, host := range []string{"2001:db8:ffff::1", "192.0.2.20", "198.51.100.255"} {
		if !set.Contains(host) {
			t.Fatal(host)
		}
	}
	for _, host := range []string{"2001:db9::1", "192.0.2.21", "host.example"} {
		if set.Contains(host) {
			t.Fatal(host)
		}
	}
	if len(set.intervals) > 3 {
		t.Fatal("network expanded")
	}
	for _, bad := range []string{"192.0.2.20-10", "192.0.2.1-2001:db8::1", "0.0.0.0/33", "fe80::1%en0", "router.example"} {
		if _, e = NewAddressSet([]string{bad}); e == nil {
			t.Fatal(bad)
		}
	}
}

func TestAddressSetPreservesIPv4MappedAddressFamily(t *testing.T) {
	set, err := NewAddressSet([]string{"::ffff:192.0.2.1"})
	if err != nil {
		t.Fatal(err)
	}
	if !set.Contains("::ffff:192.0.2.1") || set.Contains("192.0.2.1") {
		t.Fatal("IPv4-mapped IPv6 must retain its address family")
	}
}
func TestSelectionIntersectionAndExclusions(t *testing.T) {
	opts := SelectionOptions{IncludeHosts: []string{"192.0.2.0/24"}, ExcludeHosts: []string{"192.0.2.9"}, IncludeNames: []string{"core*"}, NameMatch: "glob", IncludeVendors: []string{"h3c"}, ExcludeModels: []string{"old"}}
	s, e := NewSelection(opts)
	if e != nil {
		t.Fatal(e)
	}
	opts.IncludeHosts[0] = "0.0.0.0/0"
	if !s.Evaluate(Device{IP: "192.0.2.10", Name: "CORE-1", Vendor: "h3c"}).Selected() {
		t.Fatal("matching device excluded")
	}
	got := s.Evaluate(Device{IP: "192.0.2.9", Name: "edge", Vendor: "cisco_ios", Model: "old-unit"}).Reasons
	want := []string{"host_excluded", "vendor_not_included", "name_not_included", "model_excluded"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%v", got)
	}
	if s.Evaluate(Device{IP: "198.51.100.1", Name: "core-2", Vendor: "h3c"}).Selected() {
		t.Fatal("input mutation affected selection")
	}
}

func TestSelectionRubyNegatedGlobClasses(t *testing.T) {
	for _, exclude := range []bool{false, true} {
		o := SelectionOptions{NameMatch: "glob"}
		if exclude {
			o.ExcludeNames = []string{"[!]"}
		} else {
			o.IncludeNames = []string{"[!]"}
		}
		s, err := NewSelection(o)
		if err != nil {
			t.Fatal(err)
		}
		for _, value := range []string{"!", "a", "ab"} {
			want := len(value) == 1
			if exclude {
				want = !want
			}
			if got := s.Evaluate(Device{Name: value}).Selected(); got != want {
				t.Errorf("[!] exclude=%v value=%q selected=%v want=%v", exclude, value, got, want)
			}
		}
	}
	for _, marker := range []string{"!", "^"} {
		for _, field := range []string{"name", "model"} {
			for _, exclude := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/exclude_%v", marker, field, exclude), func(t *testing.T) {
					o := SelectionOptions{NameMatch: "glob", ModelMatch: "glob"}
					patterns := []string{"[" + marker + "a]*"}
					if field == "name" {
						if exclude {
							o.ExcludeNames = patterns
						} else {
							o.IncludeNames = patterns
						}
					} else {
						if exclude {
							o.ExcludeModels = patterns
						} else {
							o.IncludeModels = patterns
						}
					}
					s, e := NewSelection(o)
					if e != nil {
						t.Fatal(e)
					}
					for _, value := range []string{"alpha", "beta"} {
						d := Device{Name: value, Model: value}
						want := value == "beta"
						if exclude {
							want = !want
						}
						if got := s.Evaluate(d).Selected(); got != want {
							t.Errorf("%s selected=%v want=%v", value, got, want)
						}
					}
				})
			}
		}
	}
	for _, pattern := range []string{`\[!a]*`, `[a!]*`, `[a[!]*`} {
		s, e := NewSelection(SelectionOptions{NameMatch: "glob", IncludeNames: []string{pattern}})
		if e != nil {
			t.Fatal(e)
		}
		if s.Evaluate(Device{Name: "beta"}).Selected() {
			t.Fatalf("literal ! changed in %s", pattern)
		}
	}
}
