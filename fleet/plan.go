package fleet

import (
	"errors"
	"net/netip"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gatework/go-net-connector/transfer"

	"github.com/gatework/go-net-connector/inventory"
	"github.com/gatework/go-net-connector/vendors/dialect"
)

// ErrInvalidPlan identifies invalid rules, task plans, or report inputs.
var ErrInvalidPlan = errors.New("netdisco: invalid plan")

// Mapping matches normalized inventory identity to a connector platform.
type Mapping struct {
	Vendor, Connector string
	OS, ModelPrefix   *string
}

// RuleOptions defines inventory filtering and explicit platform overrides.
type RuleOptions struct {
	Selection                      inventory.SelectionOptions
	VendorOverrides, HostOverrides map[string]string
	Mappings                       []Mapping
}

// Rules owns validated selection and platform resolution rules.
type Rules struct {
	selection        inventory.Selection
	vendors, hosts   map[string]string
	mappings         []Mapping
	registered       map[string]struct{}
	platformResolver PlatformResolver
	registry         dialect.Registry
}

// PlatformResolver belongs to Fleet, which consumes platform recognition.
type PlatformResolver interface {
	ResolvePlatform(identity dialect.InventoryIdentity) (platform string, found bool)
}

var labelChars = regexp.MustCompile(`[^a-z0-9]+`)

func normalizeIdentityField(s string) string {
	return strings.TrimSpace(labelChars.ReplaceAllString(strings.ToLower(s), " "))
}

// NewRules validates and owns rules against an explicit platform registry.
func NewRules(o RuleOptions, registry dialect.Registry) (*Rules, error) {
	if registry == nil {
		return nil, ErrInvalidPlan
	}
	names := registry.Names()
	if len(names) == 0 {
		return nil, ErrInvalidPlan
	}
	s, e := inventory.NewSelection(o.Selection)
	if e != nil {
		return nil, e
	}
	registered := make(map[string]struct{}, len(names))
	for _, name := range names {
		registered[name] = struct{}{}
	}
	r := &Rules{
		selection:        s,
		vendors:          map[string]string{},
		hosts:            map[string]string{},
		registered:       registered,
		platformResolver: platformResolver(registry),
		registry:         registry,
	}
	if err := r.configure(o); err != nil {
		return nil, err
	}
	return r, nil
}

func platformResolver(registry dialect.Registry) PlatformResolver {
	resolver, _ := registry.(PlatformResolver)
	return resolver
}

// Resolve returns the registered platform selected for an inventory device.
func (r *Rules) Resolve(d inventory.Device) string {
	if a, e := netip.ParseAddr(d.IP); e == nil {
		if v := r.hosts[a.String()]; v != "" {
			return v
		}
	}
	label, os, model := normalizeIdentityField(d.Vendor), normalizeIdentityField(d.OS), normalizeIdentityField(d.Model)
	for _, m := range r.mappings {
		if m.Vendor == label && (m.OS == nil || *m.OS == os) && (m.ModelPrefix == nil || strings.HasPrefix(model, *m.ModelPrefix)) {
			return m.Connector
		}
	}
	if v := r.vendors[label]; v != "" {
		return v
	}
	if r.platformResolver == nil {
		return ""
	}
	platform, found := r.platformResolver.ResolvePlatform(dialect.InventoryIdentity{Vendor: d.Vendor, OS: d.OS, Model: d.Model})
	if !found {
		return ""
	}
	if _, registered := r.registered[platform]; !registered {
		return ""
	}
	return platform
}

// Record contains normalized device identity and any planning issue.
type Record struct{ Host, SourceIP, Name, Vendor, SourceVendor, Model, OS, OSVersion, Serial, Issue string }

// Record normalizes a device and records any reason it cannot be selected.
func (r *Rules) Record(d inventory.Device) Record {
	v := Record{SourceIP: d.IP, Name: strings.TrimSpace(d.Name), Vendor: r.Resolve(d), SourceVendor: d.Vendor, Model: d.Model, OS: d.OS, OSVersion: d.OSVersion, Serial: d.Serial}
	if v.Name == "" {
		v.Name = strings.TrimSpace(d.DNS)
	}
	a, e := netip.ParseAddr(d.IP)
	if e != nil || a.Zone() != "" {
		v.Issue = string(planStatusInvalidAddress)
	} else {
		v.Host = a.String()
	}
	d.IP, d.Name, d.Vendor = v.Host, v.Name, v.Vendor
	if v.Issue == "" && !r.selection.Evaluate(d).Selected() {
		v.Issue = string(planStatusFiltered)
	}
	if v.Issue == "" && v.Vendor == "" {
		v.Issue = string(planStatusUnsupportedVendor)
	}
	return v
}
func (r *Rules) configure(o RuleOptions) error {
	valid := func(v string) bool { _, ok := r.registered[v]; return ok }
	for _, list := range [][]string{o.Selection.IncludeVendors, o.Selection.ExcludeVendors} {
		for _, v := range list {
			if !valid(v) {
				return ErrInvalidPlan
			}
		}
	}
	for k, v := range o.VendorOverrides {
		k = normalizeIdentityField(k)
		if k == "" || !valid(v) {
			return ErrInvalidPlan
		}
		if old, ok := r.vendors[k]; ok && old != v {
			return ErrInvalidPlan
		}
		r.vendors[k] = v
	}
	if err := r.configureHosts(o.HostOverrides); err != nil {
		return err
	}
	return r.configureMappings(o.Mappings)
}

func (r *Rules) configureHosts(overrides map[string]string) error {
	valid := func(v string) bool { _, ok := r.registered[v]; return ok }
	for k, v := range overrides {
		a, e := netip.ParseAddr(k)
		if e != nil || a.Zone() != "" || !valid(v) {
			return ErrInvalidPlan
		}
		k = a.String()
		if old, ok := r.hosts[k]; ok && old != v {
			return ErrInvalidPlan
		}
		r.hosts[k] = v
	}
	return nil
}

func (r *Rules) configureMappings(mappings []Mapping) error {
	valid := func(v string) bool { _, ok := r.registered[v]; return ok }
	for _, m := range mappings {
		m.Vendor = normalizeIdentityField(m.Vendor)
		if m.Vendor == "" || !valid(m.Connector) {
			return ErrInvalidPlan
		}
		if m.OS != nil {
			v := normalizeIdentityField(*m.OS)
			m.OS = &v
		}
		if m.ModelPrefix != nil {
			v := normalizeIdentityField(*m.ModelPrefix)
			m.ModelPrefix = &v
		}
		r.mappings = append(r.mappings, m)
	}
	return nil
}

// BackupFilename returns a safe filename using the configured naming style.
func (r Record) BackupFilename(style string) (string, error) {
	a, e := netip.ParseAddr(r.Host)
	if e != nil || a.Zone() != "" {
		return "", ErrInvalidPlan
	}
	base := strings.ReplaceAll(a.String(), ":", "_") + ".txt"
	if style == "" || style == "ip" {
		return base, nil
	}
	if style != "hostname_ip" {
		return "", ErrInvalidPlan
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

// Task binds an inventory entry to its transfer target.
type Task struct {
	Index  int
	Device Record
	Target transfer.Target
}

// PlanEntry records one inventory device and its planning status.
type PlanEntry struct {
	Device Record
	Status string
}

// PlanOptions defines operation, sampling and transfer arbitration policy.
type PlanOptions struct {
	Mode                string
	LimitPerVendor      int
	AllowFixedNameReuse bool
	Target              func(Record) (transfer.Target, error)
}

// Plan owns inventory outcomes and the ready tasks derived from them.
type Plan struct {
	mode      mode
	entries   []PlanEntry
	tasks     []Task
	transfers map[int]transfer.Target
}

// NewPlan validates operation capabilities and arbitrates duplicate hosts and destinations.
func NewPlan(rows []inventory.Device, rules *Rules, o PlanOptions) (Plan, error) {
	if rules == nil || (o.Mode != "backup" && o.Mode != "tftp") || o.LimitPerVendor < 0 || o.LimitPerVendor > 5 || (o.Mode == "tftp" && o.Target == nil) {
		return Plan{}, ErrInvalidPlan
	}
	p := Plan{mode: mode(o.Mode), transfers: map[int]transfer.Target{}}
	counts := map[string]int{}
	for _, d := range rows {
		r := rules.Record(d)
		p.entries = append(p.entries, PlanEntry{Device: r, Status: string(planStatus(r.Issue))})
		if r.Host != "" {
			counts[r.Host]++
		}
	}
	capabilities, groups, groupOrder, err := preparePlanEntries(&p, counts, rules, o)
	if err != nil {
		return Plan{}, err
	}
	arbitration := arbitratePlan(&p, groups, groupOrder, o.LimitPerVendor)
	preparePlanTasks(&p, arbitration, capabilities, o)
	return p, nil
}

// Entries returns a copy of the inventory outcomes in inventory order.
func (p Plan) Entries() []PlanEntry { return append([]PlanEntry(nil), p.entries...) }

// ErrMissingCredentials reports a device with no configured login credentials.
var ErrMissingCredentials = errors.New("fleet: missing device credentials")

// Tasks returns a copy of the ready tasks in inventory order.
func (p Plan) Tasks() []Task { return append([]Task(nil), p.tasks...) }

type capability struct {
	backup, tftp, allowDestinationReuse bool
}

func (r *Rules) capability(vendor string, planMode mode) (capability, error) {
	declaration, err := r.registry.Lookup(vendor)
	if err != nil {
		return capability{}, ErrInvalidPlan
	}
	if planMode == modeBackup {
		return capability{backup: len(declaration.Terminal().ConfigCommands) > 0}, nil
	}
	tftpPolicy, hasTransfer := declaration.(transfer.TFTPPolicy)
	return capability{tftp: hasTransfer, allowDestinationReuse: hasTransfer && tftpPolicy.AllowsConcurrentDestinationReuse()}, nil
}

func preparePlanEntries(p *Plan, counts map[string]int, rules *Rules, o PlanOptions) (map[string]capability, map[string][]int, []string, error) {
	capabilities := make(map[string]capability)
	groups := map[string][]int{}
	groupOrder := []string{}
	for i := range p.entries {
		entry := &p.entries[i]
		r := entry.Device
		if counts[r.Host] > 1 && r.Host != "" {
			entry.Status = string(planStatusDuplicateHost)
		}
		if planStatus(entry.Status) != "" {
			continue
		}
		supported, ok := capabilities[r.Vendor]
		if !ok {
			var err error
			supported, err = rules.capability(r.Vendor, p.mode)
			if err != nil {
				return nil, nil, nil, err
			}
			capabilities[r.Vendor] = supported
		}
		if (p.mode == modeBackup && !supported.backup) || (p.mode == modeTFTP && !supported.tftp) {
			entry.Status = string(planStatusUnsupportedOperation)
			continue
		}
		if p.mode == modeTFTP {
			target, e := o.Target(r)
			if e != nil {
				return nil, nil, nil, e
			}
			if target.Server == "" || target.Path == "" {
				return nil, nil, nil, ErrInvalidPlan
			}
			p.transfers[i] = target
		}
		if _, exists := groups[r.Vendor]; !exists {
			groupOrder = append(groupOrder, r.Vendor)
		}
		groups[r.Vendor] = append(groups[r.Vendor], i)
	}
	return capabilities, groups, groupOrder, nil
}

// arbitratePlan uses sampling order; tasks and outcomes retain inventory order.
func arbitratePlan(p *Plan, groups map[string][]int, groupOrder []string, limit int) []int {
	arbitration := []int{}
	if limit > 0 {
		for _, vendor := range groupOrder {
			indices := groups[vendor]
			sort.SliceStable(indices, func(i, j int) bool { return p.entries[indices[i]].Device.Host < p.entries[indices[j]].Device.Host })
			for n, i := range indices {
				if n >= limit {
					p.entries[i].Status = string(planStatusSampleLimit)
				} else {
					arbitration = append(arbitration, i)
				}
			}
		}
	}
	if limit == 0 {
		for i, e := range p.entries {
			if planStatus(e.Status) == "" {
				arbitration = append(arbitration, i)
			}
		}
	}
	return arbitration
}

func preparePlanTasks(p *Plan, arbitration []int, capabilities map[string]capability, o PlanOptions) {
	targets := map[[2]string]int{}
	if p.mode == modeTFTP {
		for _, i := range arbitration {
			target := p.transfers[i]
			if _, exists := targets[[2]string{target.Server, target.Path}]; !exists {
				targets[[2]string{target.Server, target.Path}] = i
			}
		}
	}
	for i := range p.entries {
		e := &p.entries[i]
		if planStatus(e.Status) != "" {
			continue
		}
		target := p.transfers[i]
		reuseDestination := o.AllowFixedNameReuse && capabilities[e.Device.Vendor].allowDestinationReuse
		if p.mode == modeTFTP && targets[[2]string{target.Server, target.Path}] != i && !reuseDestination {
			e.Status = string(planStatusRemoteFilenameCollision)
			continue
		}
		e.Status = string(planStatusReady)
		p.tasks = append(p.tasks, Task{Index: i, Device: e.Device, Target: target})
	}
}
