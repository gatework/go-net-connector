package topology

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/gatework/go-net-connector/internal/ifname"
	"github.com/gatework/go-net-connector/vendors"
)

func TestBuildPlanPureAndExactStages(t *testing.T) {
	for _, vendor := range []string{"h3c", "h3c_wireless", "cisco_ios", "cisco_nxos", "hillstone"} {
		t.Run(vendor, func(t *testing.T) {
			peer := Neighbor{LocalInterface: "GE1/0/1", NeighborName: "peer", NeighborInterface: "GE1/0/2", Protocol: "lldp"}
			p, e := BuildPlan(vendors.NewBuiltinRegistry(), "192.0.2.1", vendor, []Neighbor{peer}, map[string]string{"gigabitethernet1/0/1": "original"}, FormatOptions{})
			if e != nil {
				t.Fatal(e)
			}
			if len(p.Changes) != 1 || p.Changes[0].NewDescription != "To_peer_Gi1/0/2" {
				t.Fatalf("plan: %#v", p)
			}
			if e = ValidatePlan(vendors.NewBuiltinRegistry(), p, p.Host, vendor); e != nil {
				t.Fatal(e)
			}
			p.Commands = append(p.Commands, "save again")
			if !errors.Is(ValidatePlan(vendors.NewBuiltinRegistry(), p, p.Host, vendor), ErrInvalid) {
				t.Fatal("forged commands accepted")
			}
		})
	}
}
func TestNameAndDescriptionIdentity(t *testing.T) {
	for _, tc := range []struct{ name, a, b string }{{"speed", "XGE1/0/1", "TenGigabitEthernet1/0/1"}, {"aggregation", "BAGG1", "Bridge-Aggregation1"}, {"25G", "Twenty-FiveGigE1/1", "Twe1/1"}} {
		t.Run(tc.name, func(t *testing.T) {
			if InterfaceKey(tc.a) != InterfaceKey(tc.b) {
				t.Fatal(tc)
			}
		})
	}
	if !EquivalentDescription("to_PEER_Ten-GigabitEthernet1/0/2", "To_peer_Te1/0/2") {
		t.Fatal("alias differs")
	}
	if EquivalentDescription("To_peer_Gi1/0/2", "To_peer_Te1/0/2") {
		t.Fatal("different speed equal")
	}
	if got := ifname.ConfigName("Te1/0/2"); got != "TenGigabitEthernet1/0/2" {
		t.Fatalf("configuration interface = %q", got)
	}
}
func TestPlanRejectsIncompleteEvidenceAndUnsafeCommands(t *testing.T) {
	peer := Neighbor{LocalInterface: "Gi1/1", NeighborName: "peer", NeighborInterface: "Gi1/2"}
	for _, tc := range []struct {
		name    string
		peers   []Neighbor
		desc    map[string]string
		options FormatOptions
	}{{"missing", []Neighbor{peer}, nil, FormatOptions{}}, {"duplicate", []Neighbor{peer, peer}, map[string]string{"gigabitethernet1/1": ""}, FormatOptions{}}, {"injection", []Neighbor{peer}, map[string]string{"gigabitethernet1/1": ""}, FormatOptions{Formatter: func(Neighbor) (string, error) { return "bad\nend", nil }}}} {
		t.Run(tc.name, func(t *testing.T) {
			if _, e := BuildPlan(vendors.NewBuiltinRegistry(), "h", "cisco_ios", tc.peers, tc.desc, tc.options); e == nil {
				t.Fatal("accepted")
			}
		})
	}
	for _, v := range []string{"huawei", "fortinet", "juniper", "palo_alto", "radware", "topsec", "centec"} {
		t.Run(v, func(t *testing.T) {
			if _, e := BuildPlan(vendors.NewBuiltinRegistry(), "h", v, nil, nil, FormatOptions{}); !errors.Is(e, ErrUnsupported) {
				t.Fatalf("%v", e)
			}
		})
	}
}
func TestAuditPreservesVerifiedSubsetAndReadOnlyFindings(t *testing.T) {
	peers := []Neighbor{{LocalInterface: "Gi1/1", NeighborName: "peer", NeighborInterface: "Gi1/2"}, {LocalInterface: "Gi1/3", NeighborName: "unknown", NeighborInterface: "Gi1/4"}}
	a, e := BuildAudit(vendors.NewBuiltinRegistry(), "h", "cisco_ios", peers, map[string]string{"gigabitethernet1/1": "", "gigabitethernet1/3": ""}, FormatOptions{})
	if e != nil || a.Plan == nil || len(a.Plan.Changes) != 1 || a.Plan.Scope != "interfaces" || len(a.Issues) != 1 {
		t.Fatalf("%#v %v", a, e)
	}
	a, e = BuildAudit(vendors.NewBuiltinRegistry(), "h", "juniper", peers, map[string]string{"gigabitethernet1/1": ""}, FormatOptions{})
	if e != nil || a.Plan != nil || !a.ManualRequired {
		t.Fatalf("%#v %v", a, e)
	}
	p, e := BuildPlan(vendors.NewBuiltinRegistry(), "h", "cisco_ios", peers[:1], map[string]string{"gigabitethernet1/1": "to_PEER_GigabitEthernet1/2"}, FormatOptions{})
	if e != nil || len(p.Commands) != 0 || !reflect.DeepEqual(p.Changes, []Change{}) {
		t.Fatalf("no-op %#v %v", p, e)
	}
}

func TestBuildAuditRejectsPlatformsWithoutTopologyDialect(t *testing.T) {
	peer := Neighbor{LocalInterface: "Gi1/1", NeighborName: "peer", NeighborInterface: "Gi1/2"}
	for _, vendor := range []string{"centec", "topsec"} {
		t.Run(vendor, func(t *testing.T) {
			_, err := BuildAudit(vendors.NewBuiltinRegistry(), "h", vendor, []Neighbor{peer}, map[string]string{"gigabitethernet1/1": "old"}, FormatOptions{})
			if !errors.Is(err, ErrUnsupported) {
				t.Fatalf("expected unsupported topology dialect, got %v", err)
			}
		})
	}
}

func TestFormatterFailureRetainsPrimaryErrorWithoutDiagnosticLeak(t *testing.T) {
	primary := errors.New("private-marker")
	_, e := BuildPlan(vendors.NewBuiltinRegistry(), "h", "cisco_ios", []Neighbor{{LocalInterface: "Gi1/1", NeighborName: "peer", NeighborInterface: "Gi1/2"}}, map[string]string{"gigabitethernet1/1": ""}, FormatOptions{Formatter: func(Neighbor) (string, error) { return "", primary }})
	if !errors.Is(e, primary) {
		t.Fatalf("primary error lost: %v", e)
	}
}

func TestValidatePlanRejectsAliasDuplicateAndForgedEvidence(t *testing.T) {
	peer := Neighbor{LocalInterface: "Gi1/1", NeighborName: "peer", NeighborInterface: "Gi1/2"}
	p, e := BuildPlan(vendors.NewBuiltinRegistry(), "h", "cisco_ios", []Neighbor{peer}, map[string]string{"gigabitethernet1/1": "old"}, FormatOptions{})
	if e != nil {
		t.Fatal(e)
	}
	p.Changes = append(p.Changes, p.Changes[0])
	p.Changes[1].Interface = "Gi1/1"
	p.Commands = append(p.Commands[:4], append([]string{"interface Gi1/1", "description To_peer_Gi1/2", "exit"}, p.Commands[4:]...)...)
	if e = ValidatePlan(vendors.NewBuiltinRegistry(), p, "h", "cisco_ios"); !errors.Is(e, ErrInvalid) {
		t.Fatalf("alias duplicate accepted: %v", e)
	}
}

func TestValidatePlanRejectsNonCanonicalInterfaceAlias(t *testing.T) {
	peer := Neighbor{LocalInterface: "Gi1/1", NeighborName: "peer", NeighborInterface: "Gi1/2"}
	p, err := BuildPlan(vendors.NewBuiltinRegistry(), "h", "cisco_ios", []Neighbor{peer}, map[string]string{"gigabitethernet1/1": "old"}, FormatOptions{})
	if err != nil {
		t.Fatal(err)
	}
	p.Changes[0].Interface = "Gi1/1"
	p.Commands[1] = "interface Gi1/1"
	if err = ValidatePlan(vendors.NewBuiltinRegistry(), p, "h", "cisco_ios"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("non-canonical alias accepted: %v", err)
	}
}

func TestBuildAuditFormatterOperationalFailure(t *testing.T) {
	primary := errors.New("private formatter unavailable")
	peer := Neighbor{LocalInterface: "Gi1/1", NeighborName: "peer", NeighborInterface: "Gi1/2"}
	a, err := BuildAudit(vendors.NewBuiltinRegistry(), "h", "cisco_ios", []Neighbor{peer}, map[string]string{"gigabitethernet1/1": "old"}, FormatOptions{Formatter: func(Neighbor) (string, error) { return "", primary }})
	var domain *Error
	if !errors.Is(err, primary) || !errors.As(err, &domain) || domain.Code != "formatter_failed" || a.Plan != nil {
		t.Fatalf("operational formatter failure lost: %#v %v", a, err)
	}
	if strings.Contains(fmt.Sprintf("%v %#v", err, err), "private") {
		t.Fatal("formatter diagnostic leaked cause")
	}
}
func TestBuildAuditInvalidFormatterDescriptionIsIssue(t *testing.T) {
	peer := Neighbor{LocalInterface: "Gi1/1", NeighborName: "peer", NeighborInterface: "Gi1/2"}
	a, err := BuildAudit(vendors.NewBuiltinRegistry(), "h", "cisco_ios", []Neighbor{peer}, map[string]string{"gigabitethernet1/1": "old"}, FormatOptions{Formatter: func(Neighbor) (string, error) { return "bad\nend", nil }})
	if err != nil || len(a.Issues) != 1 || a.Issues[0].Code != "unsafe_description" || a.Plan == nil || len(a.Plan.Changes) != 0 {
		t.Fatalf("invalid description: %#v %v", a, err)
	}
}
