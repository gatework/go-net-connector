package fleet

import (
	"testing"

	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/inventory"
	"github.com/gatework/go-net-connector/vendors"
)

func TestPlanLooksUpCapabilitiesOncePerVendorPerPlan(t *testing.T) {
	calls := make(map[string]int)
	supported := true
	rules, err := NewRules(RuleOptions{}, func(v string) (connector.Profile, error) {
		calls[v]++
		p, err := vendors.Profile(v)
		if !supported {
			p.ConfigCommands = nil
		}
		return p, err
	})
	if err != nil {
		t.Fatal(err)
	}
	rows := []inventory.Device{{IP: "192.0.2.1", Vendor: "Cisco", OS: "ios"}, {IP: "192.0.2.2", Vendor: "Cisco", OS: "ios"}, {IP: "192.0.2.3", Vendor: "H3C"}}
	plan, err := NewPlan(rows, rules, PlanOptions{Mode: "backup"})
	if err != nil || len(plan.Tasks()) != 3 || calls["cisco_ios"] != 1 || calls["h3c"] != 1 {
		t.Fatalf("calls=%v err=%v", calls, err)
	}
	supported = false
	plan, err = NewPlan(rows, rules, PlanOptions{Mode: "backup"})
	if err != nil || len(plan.Tasks()) != 0 || calls["cisco_ios"] != 2 || calls["h3c"] != 2 {
		t.Fatalf("capabilities leaked between plans: %v %v", calls, err)
	}
}
