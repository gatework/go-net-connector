package fleet

import (
	"errors"
	"net/netip"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gatework/go-net-connector/transfer"

	"github.com/gatework/go-net-connector/inventory"
	"github.com/gatework/go-net-connector/vendors/dialect"
)

// ErrInvalidPlan identifies invalid rules, task plans, or report inputs.
var ErrInvalidPlan = errors.New("netdisco: invalid plan")

// Mapping assigns a connector to devices of a normalized vendor, optionally
// narrowed by OS and model prefix.
type Mapping struct {
	Vendor, Connector string
	OS, ModelPrefix   *string
}

// RuleOptions configures inventory selection and how devices map to
// connectors.
type RuleOptions struct {
	Selection                      inventory.SelectionOptions
	VendorOverrides, HostOverrides map[string]string
	Mappings                       []Mapping
}

// Rules select inventory devices and resolve each one's connector.
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

// NewRules validates o against the connectors in registry and returns the
// rules that map inventory devices to connectors.
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
	for _, list := range [][]string{o.Selection.IncludeVendors, o.Selection.ExcludeVendors} {
		for _, v := range list {
			if !r.known(v) {
				return nil, ErrInvalidPlan
			}
		}
	}
	if e := r.addVendorOverrides(o.VendorOverrides); e != nil {
		return nil, e
	}
	if e := r.addHostOverrides(o.HostOverrides); e != nil {
		return nil, e
	}
	if e := r.addMappings(o.Mappings); e != nil {
		return nil, e
	}
	return r, nil
}

// Resolve returns the connector for d: a host override, then a mapping, a
// vendor override, and finally platform recognition, or "" when none applies.
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

// Record normalizes d, resolves its connector and records why it is skipped,
// if it is.
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

// known reports whether connector is registered.
func (r *Rules) known(connector string) bool {
	_, ok := r.registered[connector]
	return ok
}

// addVendorOverrides records connector overrides by normalized vendor
// label, rejecting labels that normalize to the same key with different
// connectors.
func (r *Rules) addVendorOverrides(overrides map[string]string) error {
	for k, v := range overrides {
		k = normalizeIdentityField(k)
		if k == "" || !r.known(v) {
			return ErrInvalidPlan
		}
		if old, ok := r.vendors[k]; ok && old != v {
			return ErrInvalidPlan
		}
		r.vendors[k] = v
	}
	return nil
}

// normalizedField returns a normalized copy of an optional field.
func normalizedField(v *string) *string {
	if v == nil {
		return nil
	}
	n := normalizeIdentityField(*v)
	return &n
}

func platformResolver(registry dialect.Registry) PlatformResolver {
	resolver, _ := registry.(PlatformResolver)
	return resolver
}

// addHostOverrides records connector overrides by canonical address.
func (r *Rules) addHostOverrides(overrides map[string]string) error {
	for k, v := range overrides {
		a, e := netip.ParseAddr(k)
		if e != nil || a.Zone() != "" || !r.known(v) {
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

// Record is a normalized inventory device with its resolved connector in
// Vendor and any skip reason in Issue.
type Record struct{ Host, SourceIP, Name, Vendor, SourceVendor, Model, OS, OSVersion, Serial, Issue string }

// addMappings records identity mappings with normalized fields.
func (r *Rules) addMappings(mappings []Mapping) error {
	for _, m := range mappings {
		m.Vendor = normalizeIdentityField(m.Vendor)
		if m.Vendor == "" || !r.known(m.Connector) {
			return ErrInvalidPlan
		}
		m.OS = normalizedField(m.OS)
		m.ModelPrefix = normalizedField(m.ModelPrefix)
		r.mappings = append(r.mappings, m)
	}
	return nil
}

// BackupFilename returns "<IP>.txt" for style "ip" or "<name>-<IP>.txt" for
// "hostname_ip", with the name made file-safe.
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

// Task is a ready plan entry with its transfer target.
type Task struct {
	Index  int
	Device Record
	Target transfer.Target
}

// PlanEntry is one inventory row and its plan status.
type PlanEntry struct {
	Device Record
	Status string
}

// PlanOptions selects the plan mode, sample limit and TFTP target resolution.
type PlanOptions struct {
	Mode                string
	LimitPerVendor      int
	AllowFixedNameReuse bool
	Target              func(Record) (transfer.Target, error)
}

// Plan is the classified inventory and the tasks to run, both in inventory
// order.
type Plan struct {
	mode      mode
	entries   []PlanEntry
	tasks     []Task
	transfers map[int]transfer.Target
}

// NewPlan classifies every inventory row and schedules the eligible ones as
// tasks in inventory order, applying the per-vendor sample limit and, for
// TFTP, the remote filename collision rule.
func NewPlan(rows []inventory.Device, rules *Rules, o PlanOptions) (Plan, error) {
	if rules == nil || (o.Mode != "backup" && o.Mode != "tftp") || o.LimitPerVendor < 0 || o.LimitPerVendor > 5 || (o.Mode == "tftp" && o.Target == nil) {
		return Plan{}, ErrInvalidPlan
	}
	b := &planBuilder{
		rules:        rules,
		options:      o,
		plan:         Plan{mode: mode(o.Mode), transfers: map[int]transfer.Target{}},
		capabilities: map[string]capability{},
		groups:       map[string][]int{},
	}
	b.record(rows)
	for i := range b.plan.entries {
		if e := b.admit(i); e != nil {
			return Plan{}, e
		}
	}
	b.schedule(b.arbitrate())
	return b.plan, nil
}

// capability is what a connector supports in the plan's mode.
type capability struct {
	backup, tftp, allowDestinationReuse bool
}

// planBuilder accumulates one plan.
type planBuilder struct {
	rules        *Rules
	options      PlanOptions
	plan         Plan
	capabilities map[string]capability
	groups       map[string][]int
	groupOrder   []string
}

// record resolves every row and marks hosts that appear more than once.
func (b *planBuilder) record(rows []inventory.Device) {
	counts := map[string]int{}
	for _, d := range rows {
		r := b.rules.Record(d)
		b.plan.entries = append(b.plan.entries, PlanEntry{Device: r, Status: string(planStatus(r.Issue))})
		if r.Host != "" {
			counts[r.Host]++
		}
	}
	for i := range b.plan.entries {
		if e := &b.plan.entries[i]; e.Device.Host != "" && counts[e.Device.Host] > 1 {
			e.Status = string(planStatusDuplicateHost)
		}
	}
}

// admit checks that entry i's connector supports the mode, resolves its
// TFTP target and adds it to its vendor group.
func (b *planBuilder) admit(i int) error {
	entry := &b.plan.entries[i]
	r := entry.Device
	if planStatus(entry.Status) != "" {
		return nil
	}
	supported, e := b.capability(r.Vendor)
	if e != nil {
		return e
	}
	tftp := b.plan.mode == modeTFTP
	if !tftp && !supported.backup || tftp && !supported.tftp {
		entry.Status = string(planStatusUnsupportedOperation)
		return nil
	}
	if tftp {
		target, e := b.options.Target(r)
		if e != nil {
			return e
		}
		if target.Server == "" || target.Path == "" {
			return ErrInvalidPlan
		}
		b.plan.transfers[i] = target
	}
	if _, exists := b.groups[r.Vendor]; !exists {
		b.groupOrder = append(b.groupOrder, r.Vendor)
	}
	b.groups[r.Vendor] = append(b.groups[r.Vendor], i)
	return nil
}

// capability returns, and caches, what connector supports in the mode.
func (b *planBuilder) capability(connector string) (capability, error) {
	if c, ok := b.capabilities[connector]; ok {
		return c, nil
	}
	declaration, e := b.rules.registry.Lookup(connector)
	if e != nil {
		return capability{}, ErrInvalidPlan
	}
	var c capability
	if b.plan.mode == modeBackup {
		c.backup = len(declaration.Terminal().ConfigCommands) > 0
	} else {
		policy, ok := declaration.(transfer.TFTPPolicy)
		c.tftp = ok
		c.allowDestinationReuse = ok && policy.AllowsConcurrentDestinationReuse()
	}
	b.capabilities[connector] = c
	return c, nil
}

// arbitrate returns the admitted entries in sampling order: per vendor by
// host up to the sample limit, or every admitted entry when unlimited.
// Entries past the limit are marked.
func (b *planBuilder) arbitrate() []int {
	entries := b.plan.entries
	var order []int
	if b.options.LimitPerVendor == 0 {
		for i, e := range entries {
			if planStatus(e.Status) == "" {
				order = append(order, i)
			}
		}
		return order
	}
	for _, vendor := range b.groupOrder {
		indices := b.groups[vendor]
		slices.SortStableFunc(indices, func(x, y int) int { return strings.Compare(entries[x].Device.Host, entries[y].Device.Host) })
		for n, i := range indices {
			if n >= b.options.LimitPerVendor {
				entries[i].Status = string(planStatusSampleLimit)
			} else {
				order = append(order, i)
			}
		}
	}
	return order
}

// schedule marks the remaining entries ready in inventory order. For TFTP
// the first entry in sampling order owns each remote destination unless the
// connector allows concurrent reuse.
func (b *planBuilder) schedule(order []int) {
	type destination struct{ server, path string }
	owners := map[destination]int{}
	if b.plan.mode == modeTFTP {
		for _, i := range order {
			t := b.plan.transfers[i]
			if _, exists := owners[destination{t.Server, t.Path}]; !exists {
				owners[destination{t.Server, t.Path}] = i
			}
		}
	}
	for i := range b.plan.entries {
		e := &b.plan.entries[i]
		if planStatus(e.Status) != "" {
			continue
		}
		target := b.plan.transfers[i]
		reuse := b.options.AllowFixedNameReuse && b.capabilities[e.Device.Vendor].allowDestinationReuse
		if b.plan.mode == modeTFTP && owners[destination{target.Server, target.Path}] != i && !reuse {
			e.Status = string(planStatusRemoteFilenameCollision)
			continue
		}
		e.Status = string(planStatusReady)
		b.plan.tasks = append(b.plan.tasks, Task{Index: i, Device: e.Device, Target: target})
	}
}

// Entries returns a copy of every entry.
func (p Plan) Entries() []PlanEntry { return append([]PlanEntry(nil), p.entries...) }

// ErrMissingCredentials reports a device with no configured login credentials.
var ErrMissingCredentials = errors.New("fleet: missing device credentials")

// Tasks returns a copy of the ready tasks.
func (p Plan) Tasks() []Task { return append([]Task(nil), p.tasks...) }
