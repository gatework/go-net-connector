package fleet

import (
	"sort"
	"testing"

	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/inventory"
	"github.com/gatework/go-net-connector/vendors"
	"github.com/gatework/go-net-connector/vendors/dialect"
)

type profileRegistry map[string]connector.Terminal

type changingRegistry struct {
	profiles profileRegistry
	calls    map[string]int
	resolver PlatformResolver
}

type inventoryResolvingRegistry struct{ profileRegistry }

func (inventoryResolvingRegistry) ResolvePlatform(identity dialect.InventoryIdentity) (string, bool) {
	if identity.Vendor == "Lab Platform" {
		return "fixture", true
	}
	return "", false
}

func TestRulesUseOptionalRegistryInventoryResolver(t *testing.T) {
	registry := inventoryResolvingRegistry{profileRegistry{"fixture": {Name: "fixture"}}}
	rules, err := NewRules(RuleOptions{}, registry)
	if err != nil {
		t.Fatal(err)
	}
	if got := rules.Resolve(inventory.Device{IP: "192.0.2.9", Vendor: "Lab Platform"}); got != "fixture" {
		t.Fatalf("resolved platform = %q, want fixture", got)
	}
}

func TestRulesKeepConfiguredIdentityPrecedence(t *testing.T) {
	operatingSystem := "ios"
	rules, err := NewRules(RuleOptions{
		HostOverrides:   map[string]string{"192.0.2.1": "fortinet"},
		VendorOverrides: map[string]string{"cisco": "huawei", "arista": "juniper"},
		Mappings:        []Mapping{{Vendor: "cisco", Connector: "hillstone", OS: &operatingSystem}, {Vendor: "arista", Connector: "centec"}},
	}, vendors.NewBuiltinRegistry())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		device inventory.Device
		want   string
	}{
		{inventory.Device{IP: "192.0.2.1", Vendor: "Cisco", OS: "IOS"}, "fortinet"},
		{inventory.Device{IP: "192.0.2.2", Vendor: "Cisco", OS: "IOS"}, "hillstone"},
		{inventory.Device{IP: "192.0.2.3", Vendor: "Arista"}, "centec"},
		{inventory.Device{IP: "192.0.2.4", Vendor: "Cisco Systems", OS: "IOS XE"}, "cisco_ios"},
	} {
		if got := rules.Resolve(tc.device); got != tc.want {
			t.Fatalf("Resolve(%+v) = %q, want %q", tc.device, got, tc.want)
		}
	}
}

func (r *changingRegistry) Names() []string { return r.profiles.Names() }
func (r *changingRegistry) ResolvePlatform(identity dialect.InventoryIdentity) (string, bool) {
	if r.resolver == nil {
		return "", false
	}
	return r.resolver.ResolvePlatform(identity)
}
func (r *changingRegistry) Lookup(name string) (dialect.Platform, error) {
	r.calls[name]++
	return r.profiles.Lookup(name)
}

func (r profileRegistry) Names() []string {
	names := make([]string, 0, len(r))
	for name := range r {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (r profileRegistry) Lookup(name string) (dialect.Platform, error) {
	profile, ok := r[name]
	if !ok {
		return nil, connector.ErrUnsupported
	}
	return testProfileDialect{connector.StaticDriver{Grammar: profile}}, nil
}

type testProfileDialect struct{ connector.StaticDriver }

func (d testProfileDialect) Name() string                                { return d.Grammar.Name }
func (testProfileDialect) MatchInventory(dialect.InventoryIdentity) bool { return false }
