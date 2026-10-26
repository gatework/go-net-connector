package netdisco

import (
	"errors"
	"net/netip"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/inventory"
)

var ErrPlan = errors.New("netdisco: invalid plan")

type ProfileLookup func(string) (connector.Profile, error)
type Mapping struct {
	Vendor, Connector string
	OS, ModelPrefix   *string
}
type RuleOptions struct {
	Selection                      inventory.SelectionOptions
	VendorOverrides, HostOverrides map[string]string
	Mappings                       []Mapping
}
type Rules struct {
	selection      inventory.Selection
	vendors, hosts map[string]string
	mappings       []Mapping
	lookup         ProfileLookup
}

var labelChars = regexp.MustCompile(`[^a-z0-9]+`)

func normalize(s string) string {
	return strings.TrimSpace(labelChars.ReplaceAllString(strings.ToLower(s), " "))
}
func NewRules(o RuleOptions, lookup ProfileLookup) (*Rules, error) {
	if lookup == nil {
		return nil, ErrPlan
	}
	s, e := inventory.NewSelection(o.Selection)
	if e != nil {
		return nil, e
	}
	r := &Rules{selection: s, vendors: map[string]string{}, hosts: map[string]string{}, lookup: lookup}
	valid := func(v string) bool { _, e := lookup(v); return e == nil }
	for _, list := range [][]string{o.Selection.IncludeVendors, o.Selection.ExcludeVendors} {
		for _, v := range list {
			if !valid(v) {
				return nil, ErrPlan
			}
		}
	}
	for k, v := range o.VendorOverrides {
		k = normalize(k)
		if k == "" || !valid(v) {
			return nil, ErrPlan
		}
		if old, ok := r.vendors[k]; ok && old != v {
			return nil, ErrPlan
		}
		r.vendors[k] = v
	}
	for k, v := range o.HostOverrides {
		a, e := netip.ParseAddr(k)
		if e != nil || a.Zone() != "" || !valid(v) {
			return nil, ErrPlan
		}
		k = a.String()
		if old, ok := r.hosts[k]; ok && old != v {
			return nil, ErrPlan
		}
		r.hosts[k] = v
	}
	for _, m := range o.Mappings {
		m.Vendor = normalize(m.Vendor)
		if m.Vendor == "" || !valid(m.Connector) {
			return nil, ErrPlan
		}
		if m.OS != nil {
			v := normalize(*m.OS)
			m.OS = &v
		}
		if m.ModelPrefix != nil {
			v := normalize(*m.ModelPrefix)
			m.ModelPrefix = &v
		}
		r.mappings = append(r.mappings, m)
	}
	return r, nil
}
func matches(pattern, s string) bool { return regexp.MustCompile(pattern).MatchString(s) }
func (r *Rules) Resolve(d inventory.Device) string {
	if a, e := netip.ParseAddr(d.IP); e == nil {
		if v := r.hosts[a.String()]; v != "" {
			return v
		}
	}
	label, os, model := normalize(d.Vendor), normalize(d.OS), normalize(d.Model)
	for _, m := range r.mappings {
		if m.Vendor == label && (m.OS == nil || *m.OS == os) && (m.ModelPrefix == nil || strings.HasPrefix(model, *m.ModelPrefix)) {
			return m.Connector
		}
	}
	if v := r.vendors[label]; v != "" {
		return v
	}
	h3c := func() string {
		if matches(`wireless|wlan`, os) || matches(`^(h3c\s+)?(wx|ac)\s*\d`, model) {
			return "h3c_wireless"
		}
		return "h3c"
	}
	if os == "comware" && matches(`^h3c\s+\S`, model) {
		return h3c()
	}
	switch {
	case matches(`\bh3c\b`, label):
		return h3c()
	case matches(`\bcisco\b`, label):
		if matches(`nx\s*os|nexus`, os) || matches(`\bnexus\b|\bn\s*[3579]k\b`, model) {
			return "cisco_nxos"
		}
		if matches(`\bios\b|\bios\s*xe\b`, os) {
			return "cisco_ios"
		}
	case matches(`\bradware\b`, label):
		return "radware"
	case matches(`palo\s*alto|paloalto`, label):
		return "palo_alto"
	case matches(`\bhuawei\b`, label):
		return "huawei"
	case matches(`\bhillstone\b`, label):
		return "hillstone"
	case matches(`\bcentec\b`, label):
		return "centec"
	case matches(`\btopsec\b`, label):
		return "topsec"
	case matches(`\bfortinet\b|\bfortigate\b`, label):
		if os == "fortios" || matches(`\bfortigate\b`, label) || matches(`^(fortigate|fgt)\s*\d`, model) {
			return "fortinet"
		}
	case matches(`\bjuniper\b`, label):
		if matches(`\bjunos\b`, os) {
			return "juniper"
		}
	}
	return ""
}

type Record struct{ Host, SourceIP, Name, Vendor, SourceVendor, Model, OS, OSVersion, Serial, Issue string }

func (r *Rules) Record(d inventory.Device) Record {
	v := Record{SourceIP: d.IP, Name: strings.TrimSpace(d.Name), Vendor: r.Resolve(d), SourceVendor: d.Vendor, Model: d.Model, OS: d.OS, OSVersion: d.OSVersion, Serial: d.Serial}
	if v.Name == "" {
		v.Name = strings.TrimSpace(d.DNS)
	}
	a, e := netip.ParseAddr(d.IP)
	if e != nil || a.Zone() != "" {
		v.Issue = "invalid_address"
	} else {
		v.Host = a.String()
	}
	d.IP, d.Name, d.Vendor = v.Host, v.Name, v.Vendor
	if v.Issue == "" && !r.selection.Evaluate(d).Selected() {
		v.Issue = "filtered"
	}
	if v.Issue == "" && v.Vendor == "" {
		v.Issue = "unsupported_vendor"
	}
	return v
}
func (r Record) BackupFilename(style string) (string, error) {
	a, e := netip.ParseAddr(r.Host)
	if e != nil || a.Zone() != "" {
		return "", ErrPlan
	}
	base := strings.ReplaceAll(a.String(), ":", "_") + ".txt"
	if style == "" || style == "ip" {
		return base, nil
	}
	if style != "hostname_ip" {
		return "", ErrPlan
	}
	var b strings.Builder
	dash := false
	for _, c := range strings.ToValidUTF8(r.Name, "-") {
		valid := unicode.IsLetter(c) || unicode.IsNumber(c) || strings.ContainsRune("._-", c)
		if valid {
			b.WriteRune(c)
			dash = false
		} else if !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	label := b.String()
	if len(label) > 160 {
		label = label[:160]
		for !utf8.ValidString(label) {
			label = label[:len(label)-1]
		}
	}
	label = strings.Trim(label, "._-")
	if label == "" {
		label = "unnamed"
	}
	return label + "-" + base, nil
}

type Target struct{ Server, Path string }
type Task struct {
	Index  int
	Device Record
	Target Target
}
type PlanEntry struct {
	Device Record
	Status string
}
type PlanOptions struct {
	Mode                string
	LimitPerVendor      int
	AllowFixedNameReuse bool
	Target              func(Record) (Target, error)
}
type Plan struct {
	mode      string
	entries   []PlanEntry
	tasks     []Task
	transfers map[int]Target
}

func (p Plan) Mode() string         { return p.mode }
func (p Plan) Entries() []PlanEntry { return append([]PlanEntry(nil), p.entries...) }
func (p Plan) Tasks() []Task        { return append([]Task(nil), p.tasks...) }
func (p Plan) Transfers() map[int]Target {
	m := make(map[int]Target, len(p.transfers))
	for k, v := range p.transfers {
		m[k] = v
	}
	return m
}
func NewPlan(rows []inventory.Device, rules *Rules, o PlanOptions) (Plan, error) {
	p := Plan{mode: o.Mode, transfers: map[int]Target{}}
	if rules == nil || (o.Mode != "backup" && o.Mode != "tftp") || o.LimitPerVendor < 0 || o.LimitPerVendor > 5 || (o.Mode == "tftp" && o.Target == nil) {
		return Plan{}, ErrPlan
	}
	counts := map[string]int{}
	for _, d := range rows {
		r := rules.Record(d)
		p.entries = append(p.entries, PlanEntry{Device: r, Status: r.Issue})
		if r.Host != "" {
			counts[r.Host]++
		}
	}
	groups := map[string][]int{}
	groupOrder := []string{}
	for i := range p.entries {
		entry := &p.entries[i]
		r := entry.Device
		if counts[r.Host] > 1 && r.Host != "" {
			entry.Status = "duplicate_host"
		}
		if entry.Status != "" {
			continue
		}
		profile, e := rules.lookup(r.Vendor)
		if e != nil {
			return Plan{}, ErrPlan
		}
		if (o.Mode == "backup" && len(profile.ConfigCommands) == 0) || (o.Mode == "tftp" && !profile.Capabilities.TFTP) {
			entry.Status = "unsupported_operation"
			continue
		}
		if o.Mode == "tftp" {
			target, e := o.Target(r)
			if e != nil {
				return Plan{}, e
			}
			if target.Server == "" || target.Path == "" {
				return Plan{}, ErrPlan
			}
			p.transfers[i] = target
		}
		if _, exists := groups[r.Vendor]; !exists {
			groupOrder = append(groupOrder, r.Vendor)
		}
		groups[r.Vendor] = append(groups[r.Vendor], i)
	}
	// Arbitration uses sampling order; tasks and outcomes retain inventory order.
	arbitration := []int{}
	if o.LimitPerVendor > 0 {
		for _, vendor := range groupOrder {
			indices := groups[vendor]
			sort.SliceStable(indices, func(i, j int) bool { return p.entries[indices[i]].Device.Host < p.entries[indices[j]].Device.Host })
			for n, i := range indices {
				if n >= o.LimitPerVendor {
					p.entries[i].Status = "sample_limit"
				} else {
					arbitration = append(arbitration, i)
				}
			}
		}
	}
	if o.LimitPerVendor == 0 {
		for i, e := range p.entries {
			if e.Status == "" {
				arbitration = append(arbitration, i)
			}
		}
	}
	targets := map[Target]int{}
	if o.Mode == "tftp" {
		for _, i := range arbitration {
			target := p.transfers[i]
			if _, exists := targets[target]; !exists {
				targets[target] = i
			}
		}
	}
	for i := range p.entries {
		e := &p.entries[i]
		if e.Status != "" {
			continue
		}
		target := p.transfers[i]
		if o.Mode == "tftp" && targets[target] != i && !(o.AllowFixedNameReuse && e.Device.Vendor == "palo_alto") {
			e.Status = "remote_filename_collision"
			continue
		}
		e.Status = "ready"
		p.tasks = append(p.tasks, Task{Index: i, Device: e.Device, Target: target})
	}
	return p, nil
}
