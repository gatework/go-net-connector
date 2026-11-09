package vendors_test

import (
	"testing"

	"github.com/gatework/go-net-connector/topology"
	"github.com/gatework/go-net-connector/vendors/centec"
	"github.com/gatework/go-net-connector/vendors/cisco"
	"github.com/gatework/go-net-connector/vendors/dialect"
	"github.com/gatework/go-net-connector/vendors/fortinet"
	"github.com/gatework/go-net-connector/vendors/h3c"
	"github.com/gatework/go-net-connector/vendors/hillstone"
	"github.com/gatework/go-net-connector/vendors/huawei"
	"github.com/gatework/go-net-connector/vendors/juniper"
	"github.com/gatework/go-net-connector/vendors/paloalto"
	"github.com/gatework/go-net-connector/vendors/radware"
	"github.com/gatework/go-net-connector/vendors/topsec"
)

func TestFamilyPackagesExposeDialectConstructors(t *testing.T) {
	platforms := []struct {
		name    string
		dialect dialect.Platform
	}{
		{name: "centec", dialect: centec.New()},
		{name: "cisco_ios", dialect: cisco.IOS()},
		{name: "cisco_nxos", dialect: cisco.NXOS()},
		{name: "fortinet", dialect: fortinet.New()},
		{name: "h3c", dialect: h3c.New()},
		{name: "h3c_wireless", dialect: h3c.Wireless()},
		{name: "hillstone", dialect: hillstone.New()},
		{name: "huawei", dialect: huawei.New()},
		{name: "juniper", dialect: juniper.New()},
		{name: "palo_alto", dialect: paloalto.New()},
		{name: "radware", dialect: radware.New()},
		{name: "topsec", dialect: topsec.New()},
	}
	var _ topology.DescriptionReader = h3c.New()
	var _ topology.DescriptionWriter = h3c.New()
	if got := topology.InterfaceIdentity(h3c.New()).ConfigName("Te1/0/2"); got != "TenGigabitEthernet1/0/2" {
		t.Fatalf("configuration interface = %q", got)
	}

	for _, platform := range platforms {
		t.Run(platform.name, func(t *testing.T) {
			if got := platform.dialect.Name(); got != platform.name {
				t.Fatalf("dialect name = %q, want %q", got, platform.name)
			}
		})
	}
}
