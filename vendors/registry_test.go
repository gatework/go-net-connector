package vendors

import (
	"errors"
	"reflect"
	"testing"

	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/topology"
	"github.com/gatework/go-net-connector/transfer"
	"github.com/gatework/go-net-connector/vendors/dialect"
)

func TestBuiltinRegistryReturnsDetachedDialectProfiles(t *testing.T) {
	registry := NewBuiltinRegistry()
	want := []string{"centec", "cisco_ios", "cisco_nxos", "fortinet", "h3c", "h3c_wireless", "hillstone", "huawei", "juniper", "palo_alto", "radware", "topsec"}
	if got := registry.Names(); !reflect.DeepEqual(got, want) {
		t.Fatalf("registry names = %v, want %v", got, want)
	}

	dialect, err := registry.Lookup("cisco_ios")
	if err != nil || dialect.Name() != "cisco_ios" {
		t.Fatalf("lookup cisco_ios = %v, %v", dialect, err)
	}
	profile := dialect.Terminal()
	if !reflect.DeepEqual(profile.ConfigCommands, []string{"terminal length 0", "show running-config"}) {
		t.Fatalf("connector profile commands = %v", profile.ConfigCommands)
	}
	profile.ConfigCommands[0] = "changed"
	again, err := registry.Lookup("cisco_ios")
	if err != nil {
		t.Fatal(err)
	}
	if got := again.Terminal().ConfigCommands[0]; got != "terminal length 0" {
		t.Fatalf("registry profile shared a mutable command slice: %q", got)
	}
	if again.NewConfigCollector() == again.NewConfigCollector() {
		t.Fatal("registry returned a shared configuration strategy")
	}
}

func profileForTest(tb testing.TB, name string) dialect.Platform {
	tb.Helper()
	platform, err := NewBuiltinRegistry().Lookup(name)
	if err != nil {
		tb.Fatal(err)
	}
	return platform
}

func TestBuiltinRegistryKeepsVendorFamilyOverridesAsDistinctObjects(t *testing.T) {
	registry := NewBuiltinRegistry()
	h3c, err := registry.Lookup("h3c")
	if err != nil {
		t.Fatal(err)
	}
	wireless, err := registry.Lookup("h3c_wireless")
	if err != nil {
		t.Fatal(err)
	}
	if reflect.TypeOf(h3c) == reflect.TypeOf(wireless) {
		t.Fatalf("H3C and wireless overrides share one concrete dialect type %T", h3c)
	}
	ios, err := registry.Lookup("cisco_ios")
	if err != nil {
		t.Fatal(err)
	}
	nxos, err := registry.Lookup("cisco_nxos")
	if err != nil {
		t.Fatal(err)
	}
	if reflect.TypeOf(ios) == reflect.TypeOf(nxos) {
		t.Fatalf("IOS and NX-OS overrides share one concrete dialect type %T", ios)
	}
}

func TestBuiltinRegistryRejectsUnknownPlatform(t *testing.T) {
	if _, err := NewBuiltinRegistry().Lookup("cisco_unknown"); !errors.Is(err, connector.ErrUnsupported) {
		t.Fatalf("unknown platform error = %v, want unsupported", err)
	}
}

func TestBuiltinRegistryOwnsTransferAndTopologyExceptions(t *testing.T) {
	registry := NewBuiltinRegistry()
	huawei, _ := registry.Lookup("huawei")
	huaweiTFTP, ok := huawei.(transfer.TFTPPolicy)
	if !ok || !huaweiTFTP.RequiresSourceFile() {
		t.Fatal("Huawei source-file requirement is not declared by its TFTP dialect")
	}

	paloAlto, _ := registry.Lookup("palo_alto")
	paloTFTP, ok := paloAlto.(transfer.TFTPPolicy)
	if !ok || !paloTFTP.AllowsConcurrentDestinationReuse() || !paloTFTP.RequiresVerificationRoot() {
		t.Fatal("Palo Alto destination policies are not declared by its TFTP dialect")
	}
	paloTopology, ok := paloAlto.(topology.WriteRestriction)
	if !ok || paloTopology.UnavailableCode() != "candidate_isolation_unavailable" {
		t.Fatal("Palo Alto topology write boundary is not declared by its dialect")
	}

	juniper, _ := registry.Lookup("juniper")
	if _, ok := juniper.(transfer.RemoteBackupPolicy); !ok {
		t.Fatal("Juniper remote backup is not exposed as an optional dialect capability")
	}
}

func TestBuiltinRegistryResolvesInventoryIdentity(t *testing.T) {
	registry := NewBuiltinRegistry()
	resolver, ok := any(registry).(interface {
		ResolvePlatform(identity dialect.InventoryIdentity) (string, bool)
	})
	if !ok {
		t.Fatal("built-in registry does not resolve inventory identities")
	}

	for _, tc := range []struct {
		name     string
		identity dialect.InventoryIdentity
		want     string
	}{
		{"H3C wireless OS", dialect.InventoryIdentity{Vendor: "H3C", OS: "Wireless"}, "h3c_wireless"},
		{"H3C wireless model", dialect.InventoryIdentity{Vendor: "H3C", Model: "WX5600"}, "h3c_wireless"},
		{"H3C classic", dialect.InventoryIdentity{Vendor: "H3C", OS: "Comware", Model: "S7500"}, "h3c"},
		{"Cisco IOS XE", dialect.InventoryIdentity{Vendor: "Cisco", OS: "IOS XE"}, "cisco_ios"},
		{"Cisco NX-OS", dialect.InventoryIdentity{Vendor: "Cisco", OS: "NX-OS"}, "cisco_nxos"},
		{"Cisco Nexus model", dialect.InventoryIdentity{Vendor: "Cisco", Model: "Nexus 93180"}, "cisco_nxos"},
		{"FortiGate label", dialect.InventoryIdentity{Vendor: "FortiGate"}, "fortinet"},
		{"Fortinet without OS", dialect.InventoryIdentity{Vendor: "Fortinet"}, ""},
		{"Juniper Junos", dialect.InventoryIdentity{Vendor: "Juniper", OS: "Junos"}, "juniper"},
		{"Juniper unknown OS", dialect.InventoryIdentity{Vendor: "Juniper", OS: "ScreenOS"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, found := resolver.ResolvePlatform(tc.identity)
			if got != tc.want || found != (tc.want != "") {
				t.Fatalf("ResolvePlatform(%+v) = %q, %v; want %q, %v", tc.identity, got, found, tc.want, tc.want != "")
			}
		})
	}
}

func TestEveryPlatformClassifiesReadOnlyCommands(t *testing.T) {
	registry := NewBuiltinRegistry()
	for _, name := range registry.Names() {
		platform, _ := registry.Lookup(name)
		classifier, ok := platform.(interface{ IsReadOnly(command string) bool })
		if !ok {
			t.Fatalf("%s has no read-only classifier", name)
		}
		if classifier.IsReadOnly("reload") || classifier.IsReadOnly("configure terminal") || classifier.IsReadOnly("system-view") {
			t.Fatalf("%s treats a write command as read-only", name)
		}
	}
}
