package netdisco

import (
	"context"
	"testing"

	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/inventory"
	"github.com/gatework/go-net-connector/transfer"
	"github.com/gatework/go-net-connector/vendors"
)

func TestPlanRulesSamplingAndDuplicates(t *testing.T) {
	rules, e := NewRules(RuleOptions{}, vendors.Profile)
	if e != nil {
		t.Fatal(e)
	}
	rows := []inventory.Device{{IP: "192.0.2.2", Vendor: "H3C", Name: "core"}, {IP: "192.0.2.1", Vendor: "H3C"}, {IP: "bad", Vendor: "H3C"}, {IP: "192.0.2.3", Vendor: "Cisco", OS: "ios"}, {IP: "192.0.2.3", Vendor: "Cisco", OS: "ios"}}
	plan, e := NewPlan(rows, rules, PlanOptions{Mode: "backup", LimitPerVendor: 1})
	if e != nil {
		t.Fatal(e)
	}
	tasks := plan.Tasks()
	if len(tasks) != 1 || tasks[0].Device.Host != "192.0.2.1" {
		t.Fatal(tasks)
	}
	entries := plan.Entries()
	if entries[0].Status != "sample_limit" || entries[2].Status != "invalid_address" || entries[3].Status != "duplicate_host" || entries[4].Status != "duplicate_host" {
		t.Fatal(entries)
	}
	tasks[0].Device.Host = "changed"
	if plan.Tasks()[0].Device.Host == "changed" {
		t.Fatal("plan shares mutable slice")
	}
}
func TestPlanTFTPCollisionBeforeCredentials(t *testing.T) {
	rules, _ := NewRules(RuleOptions{}, vendors.Profile)
	rows := []inventory.Device{{IP: "192.0.2.1", Vendor: "Palo Alto"}, {IP: "192.0.2.2", Vendor: "Palo Alto"}}
	target := func(Record) (Target, error) { return Target{Server: "192.0.2.9", Path: "running-config.xml"}, nil }
	p, e := NewPlan(rows, rules, PlanOptions{Mode: "tftp", Target: target})
	if e != nil {
		t.Fatal(e)
	}
	if len(p.Tasks()) != 1 || p.Entries()[1].Status != "remote_filename_collision" {
		t.Fatal(p.Entries())
	}
	p, e = NewPlan(rows, rules, PlanOptions{Mode: "tftp", Target: target, AllowFixedNameReuse: true})
	if e != nil || len(p.Tasks()) != 2 {
		t.Fatal(p, e)
	}
	if _, e = NewRules(RuleOptions{HostOverrides: map[string]string{"192.0.2.1": "unknown"}}, func(string) (connector.Profile, error) { return connector.Profile{}, connector.ErrUnsupported }); e == nil {
		t.Fatal("bad override accepted")
	}
}

func TestPlanSampledTFTPCollisionWinnerAndInventoryOrder(t *testing.T) {
	rules, err := NewRules(RuleOptions{}, vendors.Profile)
	if err != nil {
		t.Fatal(err)
	}
	rows := []inventory.Device{{IP: "192.0.2.3", Vendor: "Palo Alto"}, {IP: "192.0.2.1", Vendor: "Palo Alto"}, {IP: "192.0.2.2", Vendor: "Palo Alto"}, {IP: "192.0.2.4", Vendor: "H3C"}}
	target := func(r Record) (Target, error) {
		path := "running-config.xml"
		if r.Vendor == "h3c" {
			path = "h3c.txt"
		}
		return Target{Server: "192.0.2.9", Path: path}, nil
	}
	for _, tc := range []struct {
		name     string
		limit    int
		winner   string
		statuses []string
	}{{"sampled", 5, "192.0.2.1", []string{"remote_filename_collision", "ready", "remote_filename_collision", "ready"}}, {"all", 0, "192.0.2.3", []string{"ready", "remote_filename_collision", "remote_filename_collision", "ready"}}} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := NewPlan(rows, rules, PlanOptions{Mode: "tftp", LimitPerVendor: tc.limit, Target: target})
			if err != nil {
				t.Fatal(err)
			}
			if len(p.Tasks()) != 2 || p.Tasks()[0].Device.Host != tc.winner || p.Tasks()[1].Index != 3 {
				t.Fatalf("tasks %#v", p.Tasks())
			}
			for i, e := range p.Entries() {
				if e.Device.SourceIP != rows[i].IP || e.Status != tc.statuses[i] {
					t.Fatalf("entry %d: %#v", i, e)
				}
			}
			b, err := Run(t.Context(), p, FleetOptions{Workers: 2, Run: func(context.Context, Task) (Artifact, error) {
				return Artifact{Transfer: &transfer.Receipt{Verification: "device_reported"}}, nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			for i, o := range b.Outcomes() {
				if o.Device.SourceIP != rows[i].IP {
					t.Fatal("outcomes lost inventory order")
				}
			}
		})
	}
}
