package fleet

import (
	"fmt"
	"testing"

	"github.com/gatework/go-net-connector/inventory"
	"github.com/gatework/go-net-connector/vendors"
)

// Include vendor resolution and capability lookup in the timed workload.
func BenchmarkNewPlan(b *testing.B) {
	for _, count := range []int{1000, 10000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			rows := make([]inventory.Device, count)
			for i := range rows {
				rows[i] = inventory.Device{IP: fmt.Sprintf("10.%d.%d.%d", i>>16, (i>>8)&255, i&255), Vendor: "Cisco", OS: "ios"}
				if i%3 == 0 {
					rows[i].Vendor, rows[i].OS = "H3C", "comware"
				}
			}
			rules, err := NewRules(RuleOptions{}, vendors.Profile)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			for b.Loop() {
				plan, err := NewPlan(rows, rules, PlanOptions{Mode: "backup"})
				if err != nil || len(plan.tasks) != count {
					b.Fatal(err)
				}
			}
		})
	}
}
