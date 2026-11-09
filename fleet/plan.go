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
	selection        inventory.Selection
	vendors, hosts   map[string]string
	mappings         []Mapping
	registered       map[string]struct{}
	platformResolver PlatformResolver
	registry         dialect.Registry
}

// PlatformResolver belongs to Fleet, which consumes platform recognition.
type PlatformResolver interface {
	ResolvePlatform(dialect.InventoryIdentity) (platform string, found bool)
}

var labelChars = regexp.MustCompile(`[^a-z0-9]+`)

func normalizeIdentityField(s string) string {
	return strings.TrimSpace(labelChars.ReplaceAllString(strings.ToLower(s), " "))
}
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
	valid := func(v string) bool { _, ok := registered[v]; return ok }
	for _, list := range [][]string{o.Selection.IncludeVendors, o.Selection.ExcludeVendors} {
		for _, v := range list {
			if !valid(v) {
				return nil, ErrInvalidPlan
			}
		}
	}
	for k, v := range o.VendorOverrides {
		k = normalizeIdentityField(k)
		if k == "" || !valid(v) {
			return nil, ErrInvalidPlan
		}
		if old, ok := r.vendors[k]; ok && old != v {
			return nil, ErrInvalidPlan
		}
		r.vendors[k] = v
	}
	for k, v := range o.HostOverrides {
		a, e := netip.ParseAddr(k)
		if e != nil || a.Zone() != "" || !valid(v) {
			return nil, ErrInvalidPlan
		}
		k = a.String()
		if old, ok := r.hosts[k]; ok && old != v {
			return nil, ErrInvalidPlan
		}
		r.hosts[k] = v
	}
	for _, m := range o.Mappings {
		m.Vendor = normalizeIdentityField(m.Vendor)
		if m.Vendor == "" || !valid(m.Connector) {
			return nil, ErrInvalidPlan
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
	return r, nil
}

func platformResolver(registry dialect.Registry) PlatformResolver {
	resolver, _ := registry.(PlatformResolver)
	return resolver
}

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

type Record struct{ Host, SourceIP, Name, Vendor, SourceVendor, Model, OS, OSVersion, Serial, Issue string }

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

type Task struct {
	Index  int
	Device Record
	Target transfer.Target
}
type PlanEntry struct {
	Device Record
	Status string
}
type PlanOptions struct {
	Mode                string
	LimitPerVendor      int
	AllowFixedNameReuse bool
	Target              func(Record) (transfer.Target, error)
}
type Plan struct {
	mode      mode
	entries   []PlanEntry
	tasks     []Task
	transfers map[int]transfer.Target
}

func (p Plan) Entries() []PlanEntry { return append([]PlanEntry(nil), p.entries...) }
func (p Plan) Tasks() []Task        { return append([]Task(nil), p.tasks...) }

// ErrMissingCredentials reports a device with no configured login credentials.
var ErrMissingCredentials = errors.New("fleet: missing device credentials")

func NewPlan(rows []inventory.Device, rules *Rules, o PlanOptions) (Plan, error) {
	if rules == nil || (o.Mode != "backup" && o.Mode != "tftp") || o.LimitPerVendor < 0 || o.LimitPerVendor > 5 || (o.Mode == "tftp" && o.Target == nil) {
		return Plan{}, ErrInvalidPlan
	}
	planMode := mode(o.Mode)
	p := Plan{mode: planMode, transfers: map[int]transfer.Target{}}
	counts := map[string]int{}
	for _, d := range rows {
		r := rules.Record(d)
		p.entries = append(p.entries, PlanEntry{Device: r, Status: string(planStatus(r.Issue))})
		if r.Host != "" {
			counts[r.Host]++
		}
	}
	type capability struct {
		backup, tftp, allowDestinationReuse bool
	}
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
			declaration, e := rules.registry.Lookup(r.Vendor)
			if e != nil {
				return Plan{}, ErrInvalidPlan
			}
			if planMode == modeBackup {
				profile := declaration.Terminal()
				supported.backup = len(profile.ConfigCommands) > 0
			} else {
				tftpPolicy, hasTransfer := declaration.(transfer.TFTPPolicy)
				supported.tftp = hasTransfer
				supported.allowDestinationReuse = hasTransfer && tftpPolicy.AllowsConcurrentDestinationReuse()
			}
			capabilities[r.Vendor] = supported
		}
		if (planMode == modeBackup && !supported.backup) || (planMode == modeTFTP && !supported.tftp) {
			entry.Status = string(planStatusUnsupportedOperation)
			continue
		}
		if planMode == modeTFTP {
			target, e := o.Target(r)
			if e != nil {
				return Plan{}, e
			}
			if target.Server == "" || target.Path == "" {
				return Plan{}, ErrInvalidPlan
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
					p.entries[i].Status = string(planStatusSampleLimit)
				} else {
					arbitration = append(arbitration, i)
				}
			}
		}
	}
	if o.LimitPerVendor == 0 {
		for i, e := range p.entries {
			if planStatus(e.Status) == "" {
				arbitration = append(arbitration, i)
			}
		}
	}
	targets := map[[2]string]int{}
	if planMode == modeTFTP {
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
		if planMode == modeTFTP && targets[[2]string{target.Server, target.Path}] != i && !reuseDestination {
			e.Status = string(planStatusRemoteFilenameCollision)
			continue
		}
		e.Status = string(planStatusReady)
		p.tasks = append(p.tasks, Task{Index: i, Device: e.Device, Target: target})
	}
	return p, nil
}
