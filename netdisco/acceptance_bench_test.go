package netdisco

import (
	"context"
	"fmt"
	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/inventory"
	"github.com/gatework/go-net-connector/storage"
	"github.com/gatework/go-net-connector/vendors"
	"testing"
)

func BenchmarkMockFleet(b *testing.B) {
	for _, count := range []int{1000, 10000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			rows := make([]inventory.Device, count)
			for i := range rows {
				rows[i] = inventory.Device{IP: fmt.Sprintf("10.%d.%d.%d", i>>16, (i>>8)&255, i&255), Vendor: "cisco", OS: "ios"}
			}
			rules, e := NewRules(RuleOptions{}, vendors.Profile)
			if e != nil {
				b.Fatal(e)
			}
			plan, e := NewPlan(rows, rules, PlanOptions{Mode: "backup"})
			if e != nil {
				b.Fatal(e)
			}
			b.ReportAllocs()
			for b.Loop() {
				result, e := Run(b.Context(), plan, FleetOptions{Workers: 4, Run: func(context.Context, Task) (Artifact, error) {
					return Artifact{Backup: &connector.BackupReceipt{DeviceCompleted: true, File: storage.Receipt{State: "durable"}}}, nil
				}})
				if e != nil || !result.Success() {
					b.Fatal(e)
				}
			}
		})
	}
}
