package topology

import (
	"errors"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/vendors"
)

func key(vendor, name string) string {
	if vendor == "huawei" {
		name = strings.NewReplacer(" ", "", "\t", "").Replace(name)
	}
	if vendor == "fortinet" {
		return name
	}
	return InterfaceKey(name)
}
func writable(vendor string) error {
	p, e := vendors.Profile(vendor)
	if e != nil {
		return failure("description_unsupported", "plan", ErrUnsupported)
	}
	if !p.Capabilities.TopologyWrite {
		code := "description_unsupported"
		if vendor == "palo_alto" || vendor == "juniper" {
			code = "candidate_isolation_unavailable"
		}
		return failure(code, "plan", ErrUnsupported)
	}
	return nil
}
func format(peer Neighbor, opts FormatOptions) (string, error) {
	if opts.Formatter != nil {
		v, e := opts.Formatter(peer)
		if e != nil {
			return "", failure("formatter_failed", "plan", errors.Join(ErrInvalid, e))
		}
		return v, ValidateDescription(v)
	}
	return FormatDescription(peer, opts)
}
func evidenceFor(vendor string, peers []Neighbor, descriptions map[string]string) (map[string]Evidence, error) {
	out := map[string]Evidence{}
	for _, peer := range peers {
		k := key(vendor, peer.LocalInterface)
		if _, ok := out[k]; ok {
			return nil, failure("ambiguous_neighbor", "plan", ErrParse)
		}
		if code := peer.IdentityIssue(); code != "" {
			return nil, failure(code, "plan", ErrParse)
		}
		old, ok := descriptions[k]
		if !ok {
			return nil, failure("interface_missing", "plan", ErrParse)
		}
		out[k] = Evidence{peer.NeighborName, peer.NeighborInterface, old}
	}
	return out, nil
}

// BuildPlan is pure: it opens no connections and resolves no credentials.
func BuildPlan(host, vendor string, peers []Neighbor, descriptions map[string]string, opts FormatOptions) (Plan, error) {
	p, e := vendors.Profile(vendor)
	if e != nil {
		return Plan{}, e
	}
	return buildPlan(host, p, peers, descriptions, opts)
}
func buildPlan(host string, profile connector.Profile, peers []Neighbor, descriptions map[string]string, opts FormatOptions) (Plan, error) {
	if e := writable(profile.Name); e != nil {
		return Plan{}, e
	}
	evidence, e := evidenceFor(profile.Name, peers, descriptions)
	if e != nil {
		return Plan{}, e
	}
	p := Plan{Host: host, Vendor: profile.Name, Evidence: evidence, Changes: []Change{}, Commands: []string{}, Scope: "device"}
	for _, peer := range peers {
		old := descriptions[key(p.Vendor, peer.LocalInterface)]
		proposed, e := format(peer, opts)
		if e != nil {
			return Plan{}, e
		}
		if EquivalentDescription(old, proposed) {
			continue
		}
		p.Changes = append(p.Changes, Change{ConfigurationInterface(peer.LocalInterface), old, proposed, peer})
	}
	if len(p.Changes) > 0 {
		s, e := stagesFor(profile, p.Changes)
		if e != nil {
			return Plan{}, e
		}
		p.Commands = s.commands()
	}
	return p, nil
}

// BuildAudit preserves evidence issues separately and plans only verified interfaces.
func BuildAudit(host, vendor string, peers []Neighbor, descriptions map[string]string, opts FormatOptions) (Audit, error) {
	p, e := vendors.Profile(vendor)
	if e != nil {
		return Audit{}, e
	}
	return buildAudit(host, p, peers, descriptions, opts)
}
func buildAudit(host string, profile connector.Profile, peers []Neighbor, descriptions map[string]string, opts FormatOptions) (Audit, error) {
	a := Audit{Comparisons: []Comparison{}, Issues: []Issue{}}
	p := Plan{Host: host, Vendor: profile.Name, Scope: "interfaces", Evidence: map[string]Evidence{}, Changes: []Change{}, Commands: []string{}}
	groups := map[string][]Neighbor{}
	order := []string{}
	for _, n := range peers {
		k := key(profile.Name, n.LocalInterface)
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], n)
	}
	for _, k := range order {
		group := groups[k]
		n := group[0]
		old, exists := descriptions[k]
		row := Comparison{Interface: n.LocalInterface, Neighbor: n.NeighborName, Port: n.NeighborInterface, Status: "unverified"}
		if exists {
			row.Current = &old
		}
		code := ""
		switch {
		case len(group) != 1:
			code = "ambiguous_neighbor"
		case n.IdentityIssue() != "":
			code = n.IdentityIssue()
		case !exists:
			code = "interface_missing"
		}
		if code == "" {
			proposed, e := format(n, opts)
			var formatterError *Error
			if errors.As(e, &formatterError) && formatterError.Code == "formatter_failed" {
				return a, e
			}
			local := ConfigurationInterface(n.LocalInterface)
			if profile.Name == "fortinet" {
				local = n.LocalInterface
			}
			if e != nil || ValidateInterface(local) != nil {
				code = "unsafe_description"
			} else {
				row.Expected = &proposed
				match := EquivalentDescription(old, proposed)
				if opts.DescriptionMatcher != nil {
					match = opts.DescriptionMatcher(old, proposed, n)
				}
				row.Status = "content_mismatch"
				if match {
					row.Status = "matches"
				} else if old == "" {
					row.Status = "missing_description"
				}
				p.Evidence[k] = Evidence{n.NeighborName, n.NeighborInterface, old}
				if !match {
					p.Changes = append(p.Changes, Change{local, old, proposed, n})
				}
			}
		}
		if code != "" {
			row.Reason = code
			a.Issues = append(a.Issues, Issue{n.LocalInterface, code})
		}
		a.Comparisons = append(a.Comparisons, row)
	}
	if profile.Capabilities.TopologyWrite && writable(profile.Name) == nil {
		if len(p.Changes) > 0 {
			s, e := stagesFor(profile, p.Changes)
			if e != nil {
				return a, e
			}
			p.Commands = s.commands()
		}
		a.Plan = &p
	} else {
		for _, r := range a.Comparisons {
			if r.Status != "matches" {
				a.ManualRequired = true
			}
		}
	}
	return a, nil
}

type stages struct{ change, verify, persist []string }

func (s stages) commands() []string {
	all := append([]string{}, s.change...)
	all = append(all, s.verify...)
	return append(all, s.persist...)
}
func stagesFor(profile connector.Profile, changes []Change) (stages, error) {
	if e := writable(profile.Name); e != nil {
		return stages{}, e
	}
	s := stages{verify: slices.Clone(profile.ConfigCommands), persist: slices.Clone(profile.SaveCommands)}
	enter, leave, exit := "configure terminal", "end", "exit"
	switch profile.Name {
	case "h3c", "h3c_wireless":
		enter, leave, exit = "system-view", "return", "quit"
	case "hillstone":
		enter, leave = "configure", "exit"
	}
	s.change = []string{enter}
	seen := map[string]bool{}
	for _, c := range changes {
		k := key(profile.Name, c.Interface)
		if ValidateInterface(c.Interface) != nil || ValidateDescription(c.NewDescription) != nil || seen[k] {
			return stages{}, ErrInvalid
		}
		seen[k] = true
		s.change = append(s.change, "interface "+c.Interface, "description "+c.NewDescription, exit)
	}
	s.change = append(s.change, leave)
	if len(s.verify) == 0 || len(s.persist) == 0 {
		return stages{}, failure("description_stages_unsupported", "plan", ErrUnsupported)
	}
	for _, v := range s.commands() {
		script, e := connector.ParseScript(v, "topology")
		if e != nil || len(script.Commands) != 1 || script.Commands[0].Text != v {
			return stages{}, ErrInvalid
		}
	}
	return s, nil
}
func ValidatePlan(p Plan, host, vendor string) error {
	profile, e := vendors.Profile(vendor)
	if e != nil {
		return e
	}
	return validatePlan(p, host, profile)
}
func validatePlan(p Plan, host string, profile connector.Profile) error {
	if p.Host != host || p.Vendor != profile.Name || (p.Scope != "device" && p.Scope != "interfaces") {
		return ErrInvalid
	}
	if len(p.Changes) == 0 {
		if len(p.Commands) != 0 {
			return ErrInvalid
		}
		return nil
	}
	s, e := stagesFor(profile, p.Changes)
	if e != nil {
		return e
	}
	if !slices.Equal(p.Commands, s.commands()) {
		return ErrInvalid
	}
	for _, c := range p.Changes {
		k := key(p.Vendor, c.Interface)
		if k != key(p.Vendor, c.Neighbor.LocalInterface) || c.Neighbor.IdentityIssue() != "" || p.Evidence[k] != (Evidence{c.Neighbor.NeighborName, c.Neighbor.NeighborInterface, c.OldDescription}) {
			return ErrInvalid
		}
	}
	return nil
}
func clonePlan(p Plan) Plan {
	p.Evidence = maps.Clone(p.Evidence)
	p.Changes = slices.Clone(p.Changes)
	p.Commands = slices.Clone(p.Commands)
	p.Receipts = slices.Clone(p.Receipts)
	return p
}
func revalidate(p Plan, snapshot Snapshot) error {
	peers := snapshot.Neighbors
	descs := snapshot.Descriptions
	if p.Scope == "interfaces" {
		peers = nil
		for _, n := range snapshot.Neighbors {
			if _, ok := p.Evidence[key(p.Vendor, n.LocalInterface)]; ok {
				peers = append(peers, n)
			}
		}
	}
	evidence, e := evidenceFor(p.Vendor, peers, descs)
	if e != nil || !reflect.DeepEqual(evidence, p.Evidence) {
		return failure("stale_plan", "apply", ErrStale)
	}
	byKey := map[string]Neighbor{}
	for _, n := range peers {
		byKey[key(p.Vendor, n.LocalInterface)] = n
	}
	for _, c := range p.Changes {
		if byKey[key(p.Vendor, c.Interface)] != c.Neighbor {
			return failure("stale_plan", "apply", ErrStale)
		}
	}
	return nil
}
