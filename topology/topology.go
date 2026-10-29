package topology

import (
	"context"
	"errors"
	"slices"
	"strconv"

	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/internal/terminal"
	"github.com/gatework/go-net-connector/vendors"
)

// Topology binds stateless dialect rules to a device. Constructing it performs no I/O.
type Topology struct {
	device  *connector.Device
	profile connector.Profile
}

func New(device *connector.Device) (*Topology, error) {
	if device == nil {
		return nil, ErrInvalid
	}
	p, e := vendors.Profile(device.ProfileName())
	if e != nil {
		return nil, e
	}
	p.Capabilities = device.Capabilities()
	p.ConfigCommands = device.ConfigCommands()
	p.SaveCommands = device.SaveCommands()
	return &Topology{device, p}, nil
}

var neighborCommands = map[string]string{"h3c": "display lldp neighbor-information list", "h3c_wireless": "display lldp neighbor-information list", "huawei": "display lldp neighbor brief", "cisco_ios": "show cdp neighbors detail", "cisco_nxos": "show cdp neighbors detail", "hillstone": "show lldp neighbor-information", "palo_alto": "show lldp neighbors all", "juniper": "show lldp neighbors detail | no-more"}

func (t *Topology) supportsNeighbors() error {
	if !t.profile.Capabilities.Neighbors {
		return failure("neighbor_discovery_unsupported", "discover", ErrUnsupported)
	}
	return nil
}
func (t *Topology) supportsDescriptions() error {
	if !t.profile.Capabilities.Descriptions {
		return failure("description_unsupported", "discover", ErrUnsupported)
	}
	return nil
}
func (t *Topology) supportsWrite() error {
	if e := writable(t.profile.Name); e != nil {
		return e
	}
	if !t.profile.Capabilities.TopologyWrite {
		return failure("description_unsupported", "plan", ErrUnsupported)
	}
	return writable(t.profile.Name)
}
func (t *Topology) Neighbors(ctx context.Context) (peers []Neighbor, result Result, err error) {
	if err = t.supportsNeighbors(); err != nil {
		return
	}
	err = t.device.WithOperation(ctx, func(ctx context.Context, op *connector.Operation) error {
		var e error
		peers, e = t.neighbors(ctx, op, &result)
		return e
	})
	return
}
func (t *Topology) Descriptions(ctx context.Context) (descriptions map[string]string, result Result, err error) {
	if err = t.supportsDescriptions(); err != nil {
		return
	}
	err = t.device.WithOperation(ctx, func(ctx context.Context, op *connector.Operation) error {
		var e error
		descriptions, e = t.descriptions(ctx, op, &result)
		return e
	})
	return
}
func (t *Topology) Discover(ctx context.Context) (snapshot Snapshot, err error) {
	if err = t.supportsNeighbors(); err != nil {
		return
	}
	if err = t.supportsDescriptions(); err != nil {
		return
	}
	err = t.device.WithOperation(ctx, func(ctx context.Context, op *connector.Operation) error {
		var e error
		snapshot, e = t.discover(ctx, op)
		return e
	})
	return
}
func (t *Topology) discover(ctx context.Context, op *connector.Operation) (s Snapshot, err error) {
	r := Result{}
	defer func() { s.Receipts = r.Receipts }()
	s.Neighbors, err = t.neighbors(ctx, op, &r)
	if err != nil {
		return
	}
	s.Descriptions, err = t.descriptions(ctx, op, &r)
	return
}
func (t *Topology) neighbors(ctx context.Context, op *connector.Operation, result *Result) ([]Neighbor, error) {
	if e := t.supportsNeighbors(); e != nil {
		return nil, e
	}
	command := neighborCommands[t.profile.Name]
	if t.profile.Name == "fortinet" {
		r, e := op.ExecuteCommand(ctx, connector.Command{Text: "get system status"}, connector.ExecuteOptions{})
		result.Receipts = append(result.Receipts, r.Steps()...)
		if e != nil {
			return nil, e
		}
		command, e = fortinetCommand(r.Output(), op.CurrentPrompt())
		if e != nil {
			return nil, e
		}
	}
	r, e := op.ExecuteCommand(ctx, connector.Command{Text: command}, connector.ExecuteOptions{})
	result.Receipts = append(result.Receipts, r.Steps()...)
	if e != nil {
		return nil, e
	}
	return ParseNeighbors(t.profile.Name, r.Output())
}
func fortinetCommand(status, prompt string) (string, error) {
	var renderErr error
	status, renderErr = terminal.Render([]byte(status), true)
	if renderErr != nil {
		return "", parseError()
	}
	version := re(`(?im)^Version:[^\r\n]*\bv(\d+)\.(\d+)\.(\d+)\b`).FindStringSubmatch(status)
	if len(version) != 4 {
		return "", failure("unrecognized_output", "discover", ErrParse)
	}
	release := [3]int{}
	for i := range release {
		n, e := strconv.Atoi(version[i+1])
		if e != nil {
			return "", parseError()
		}
		release[i] = n
	}
	current := re(`(?im)^Current virtual domain:[ \t]*(\S+)[ \t]*$`).FindStringSubmatch(status)
	view := re(`\(([^()\r\n]+)\)[ \t]*[#\$][ \t]*$`).FindStringSubmatch(prompt)
	if len(current) > 1 && len(view) > 1 && current[1] != view[1] {
		return "", failure("vdom_context_mismatch", "discover", ErrParse)
	}
	if release[0] > 7 || (release[0] == 7 && (release[1] > 6 || (release[1] == 6 && release[2] >= 3))) {
		return "diagnose lldp rx neighbor", nil
	}
	return "diagnose lldprx neighbor", nil
}
func (t *Topology) descriptions(ctx context.Context, op *connector.Operation, result *Result) (map[string]string, error) {
	if e := t.supportsDescriptions(); e != nil {
		return nil, e
	}
	r, e := op.RunningConfig(ctx)
	result.Receipts = append(result.Receipts, r.Steps()...)
	if e != nil {
		return nil, e
	}
	return ParseDescriptions(t.profile.Name, r.Output())
}

// Plan collects a snapshot under one operation lease, then builds exact stages.
func (t *Topology) Plan(ctx context.Context, opts FormatOptions) (plan Plan, err error) {
	if err = t.supportsNeighbors(); err != nil {
		return
	}
	if err = t.supportsWrite(); err != nil {
		return
	}
	err = t.device.WithOperation(ctx, func(ctx context.Context, op *connector.Operation) error {
		s, e := t.discover(ctx, op)
		plan.Receipts = s.Receipts
		if e != nil {
			return e
		}
		p, e := buildPlan(t.device.Host(), t.profile, s.Neighbors, s.Descriptions, opts)
		p.Receipts = s.Receipts
		plan = p
		return e
	})
	return
}
func (t *Topology) Audit(ctx context.Context, opts FormatOptions) (audit Audit, err error) {
	if err = t.supportsNeighbors(); err != nil {
		return
	}
	if err = t.supportsDescriptions(); err != nil {
		return
	}
	err = t.device.WithOperation(ctx, func(ctx context.Context, op *connector.Operation) error {
		s, e := t.discover(ctx, op)
		audit.Receipts = s.Receipts
		if e != nil {
			return e
		}
		a, e := buildAudit(t.device.Host(), t.profile, s.Neighbors, s.Descriptions, opts)
		a.Receipts = s.Receipts
		audit = a
		return e
	})
	return
}

// Apply requires explicit confirmation and refuses stale evidence before the
// first configuration command. It never rolls back or replays uncertain writes.
func (t *Topology) Apply(ctx context.Context, plan Plan, opts ApplyOptions) (result Result, err error) {
	if !opts.Confirmed {
		return result, failure("confirmation_required", "apply", ErrConfirmation)
	}
	plan = clonePlan(plan)
	if err = validatePlan(plan, t.device.Host(), t.profile); err != nil {
		return
	}
	if opts.Execute.MaxOutputBytes < 0 {
		return result, ErrInvalid
	}
	if len(plan.Changes) == 0 {
		return
	}
	if err = t.supportsWrite(); err != nil {
		return
	}
	err = t.device.WithOperation(ctx, func(ctx context.Context, op *connector.Operation) error { return t.apply(ctx, op, plan, opts, &result) })
	return
}

// Sync is explicit intent to change descriptions. One lease covers discovery,
// plan construction, revalidation, changes, readback, and confirmed persistence.
func (t *Topology) Sync(ctx context.Context, opts FormatOptions) (result Result, err error) {
	if err = t.supportsWrite(); err != nil {
		return
	}
	err = t.device.WithOperation(ctx, func(ctx context.Context, op *connector.Operation) error {
		s, e := t.discover(ctx, op)
		result.Receipts = append(result.Receipts, s.Receipts...)
		if e != nil {
			return e
		}
		p, e := buildPlan(t.device.Host(), t.profile, s.Neighbors, s.Descriptions, opts)
		if e != nil {
			return e
		}
		if len(p.Changes) == 0 {
			return nil
		}
		return t.apply(ctx, op, p, ApplyOptions{Confirmed: true}, &result)
	})
	return
}
func commandScript(texts []string) connector.Script {
	s := connector.Script{Name: "interface descriptions"}
	for _, text := range texts {
		s.Commands = append(s.Commands, connector.Command{Text: text})
	}
	return s
}
func (t *Topology) apply(ctx context.Context, op *connector.Operation, p Plan, opts ApplyOptions, result *Result) error {
	s, e := stagesFor(t.profile, p.Changes)
	if e != nil {
		return e
	}
	snapshot, e := t.discover(ctx, op)
	result.Receipts = append(result.Receipts, snapshot.Receipts...)
	if e != nil {
		return e
	}
	if e = revalidate(p, snapshot); e != nil {
		return e
	}
	r, e := op.ExecuteScript(ctx, commandScript(s.change), opts.Execute)
	result.Receipts = append(result.Receipts, r.Steps()...)
	if e != nil {
		return e
	}
	if !slices.Equal(s.verify, t.device.ConfigCommands()) {
		return failure("verification_plan_changed", "verify", ErrVerification)
	}
	before := len(result.Receipts)
	updated, e := t.descriptions(ctx, op, result)
	if e != nil {
		return e
	}
	actual := []string{}
	for _, step := range result.Receipts[before:] {
		actual = append(actual, step.Command().Text)
	}
	if !slices.Equal(actual, s.verify) {
		return failure("verification_plan_changed", "verify", ErrVerification)
	}
	for _, c := range p.Changes {
		v, ok := updated[key(p.Vendor, c.Interface)]
		if !ok || v != c.NewDescription {
			return failure("description_unconfirmed", "verify", ErrVerification)
		}
	}
	result.Verified = true
	r, e = op.SaveConfig(ctx, opts.Execute)
	result.Receipts = append(result.Receipts, r.Steps()...)
	if e != nil {
		return failure("persistence_unconfirmed", "persist", errors.Join(ErrPersistence, e))
	}
	result.Persisted = true
	return nil
}
