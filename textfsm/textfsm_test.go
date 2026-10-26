package textfsm

import (
	"errors"
	"strings"
	"testing"
)

func TestParseIndexedFixtures(t *testing.T) {
	cases := []struct{ vendor, command, text, key, want string }{
		{"h3c", "display lldp neighbor-information list", "Local Interface Chassis ID Port ID System Name\nGE1/0/1 0011-2233-4455 GE1/0/2 peer\n", "NEIGHBOR_NAME", "peer"},
		{"h3c_wireless", "display lldp neighbor-information list", "System Name Local Interface Chassis ID Port ID\npeer GE1/0/1 0011-2233-4455 GE1/0/2\n", "LOCAL_INTERFACE", "GE1/0/1"},
		{"cisco_ios", "show ip interface brief", "Interface IP-Address OK? Method Status Protocol\r\nGigabitEthernet0/1 unassigned YES unset administratively down down\r\nrouter#", "STATUS", "administratively down"},
		{"cisco_ios", "show running-config", "interface Ethernet1\n description uplink\n!\n", "DESCRIPTION", "uplink"},
		{"cisco_nxos", "show running-config", "interface Ethernet1\n description uplink\n\n", "DESCRIPTION", "uplink"},
		{"h3c", "dis cur", "interface Ethernet1\n description uplink\n#\n", "DESCRIPTION", "uplink"},
		{"huawei", "display current-configuration", "interface Ethernet1\n description uplink\n#\n", "DESCRIPTION", "uplink"},
		{"hillstone", "show configuration running", "interface Ethernet1\n description uplink\nexit\n", "DESCRIPTION", "uplink"},
		{"palo_alto", "show", "set network interface ethernet Ethernet1 comment uplink\n", "DESCRIPTION", "uplink"},
		{"radware", "/cfg/dump", "/cfg/port 1/name uplink", "DESCRIPTION", "uplink"},
		{"fortinet", "show full-configuration", "config system interface\n    edit Ethernet1\n        set description uplink\n    next\nend\n", "DESCRIPTION", "uplink"},
		{"juniper", "show configuration | display set | no-more", "set interfaces Ethernet1 description uplink\n", "DESCRIPTION", "uplink"},
	}

	cases = append(cases, []struct{ vendor, command, text, key, want string }{
		{"cisco_nxos", "show ip interface brief vrf management", "IP Interface Status for VRF \"management\"(2)\nmgmt0 192.0.2.1 protocol-up/link-up/admin-up\n", "VRF", "management"},
		{"cisco_ios", "show cdp neighbors detail", "Device ID: core\nInterface: Gi0/1, Port ID (outgoing port): Gi0/2\n", "NEIGHBOR_NAME", "core"},
		{"cisco_nxos", "show cdp neighbors detail", "Device-ID: core\nInterface: Ethernet1/1, Port ID (outgoing port): Ethernet1/2\n", "LOCAL_INTERFACE", "Ethernet1/1"},
		{"hillstone", "show lldp neighbor-information", "core ethernet0/1 001122334455 Ethernet1/2\n", "NEIGHBOR_NAME", "core"},
		{"huawei", "display lldp neighbor brief", "Local Intf Neighbor Dev Neighbor Intf Exptime\nGE0/0/1 core GE0/0/2 120\n", "NEIGHBOR_NAME", "core"},
		{"palo_alto", "show lldp neighbors all", "Local information:\nLocal interface: ethernet1/1\nChassis ID: 001122334455\nPort ID: Ethernet1/2\nSystem name: core\n", "LOCAL_INTERFACE", "ethernet1/1"},
		{"fortinet", "diagnose lldp rx neighbor", "1 port 'wan1' 5 mac 00:11:22:33:44:55 chassis 00:11:22:33:44:66 port 'Te1/0/1' system 'core01'\n", "NEIGHBOR_NAME", "core01"},
		{"juniper", "show lldp neighbors detail | no-more", "Local Interface : ge-0/0/0\nChassis ID : 00:11:22:33:44:55\nPort ID : Ethernet1/1\nSystem name : core\nManagement Info\n  Port ID : 1\n", "NEIGHBOR_INTERFACE", "Ethernet1/1"},
	}...)
	for _, c := range cases {
		t.Run(c.vendor+c.command, func(t *testing.T) {
			r, e := Parse(c.vendor, c.command, c.text)
			if e != nil {
				t.Fatal(e)
			}
			if len(r) != 1 || r[0][c.key] != c.want {
				t.Fatalf("rows = %#v", r)
			}
		})
	}
}
func TestParseMissingAndExactIndex(t *testing.T) {
	for _, c := range []struct{ v, c string }{{"cisco_ios_other", "show ip interface brief"}, {"cisco_ios", "prefix show ip interface brief"}, {"cisco_ios", "show ip interface brief\n"}} {
		if _, e := Parse(c.v, c.c, ""); !errors.Is(e, ErrTemplateMissing) {
			t.Fatalf("error %v", e)
		}
	}
}
func TestParseTemplateFreshAndStrict(t *testing.T) {
	template := "Value Required NAME (\\S+)\nValue STATE (up|down)\n\nStart\n  ^${NAME}\\s+${STATE}$$ -> Record\n"
	for i := 0; i < 12; i++ {
		t.Run(string(rune('a'+i)), func(t *testing.T) {
			t.Parallel()
			r, e := ParseTemplate(template, "\x1b[32mport1 up\x1b[0m\r\n")
			if e != nil || len(r) != 1 || r[0]["NAME"] != "port1" {
				t.Fatalf("%#v %v", r, e)
			}
			r, e = ParseTemplate(template, "other output\n")
			if e != nil || len(r) != 0 {
				t.Fatalf("state leaked: %#v %v", r, e)
			}
		})
	}
	if _, e := ParseTemplate(template, string([]byte{255})); !errors.Is(e, ErrEncoding) {
		t.Fatalf("encoding: %v", e)
	}
}
func TestParseTemplateSafeErrorsAndLongLines(t *testing.T) {
	template := "Value Required NAME (\\S+)\n\nStart\n  ^${NAME} -> Error \"unexpected input\"\n"
	if _, e := ParseTemplate(template, "private-password\n"); !errors.Is(e, ErrParse) || strings.Contains(e.Error(), "private-password") {
		t.Fatalf("parse: %v", e)
	}
	if _, e := ParseTemplate("private-template-invalid", ""); !errors.Is(e, ErrTemplateInvalid) || strings.Contains(e.Error(), "private-template") {
		t.Fatalf("template: %v", e)
	}
	template = "Value Required NAME (.+)\n\nStart\n  ^${NAME}$$ -> Record\n"
	text := strings.Repeat("x", 128<<10)
	r, e := ParseTemplate(template, text)
	if e != nil || len(r) != 1 || r[0]["NAME"] != text {
		t.Fatalf("large line: rows %d, %v", len(r), e)
	}
	if _, e = ParseTemplate(template, strings.Repeat("x", (32<<20)+1)); !errors.Is(e, ErrIncomplete) {
		t.Fatalf("limit: %v", e)
	}
}

func TestParseTemplateLineBound(t *testing.T) {
	template := "#" + strings.Repeat("x", 32<<20) + "\nValue NAME (.+)\n\nStart\n  ^${NAME}$$ -> Record\n"
	if _, e := ParseTemplate(template, "port1"); !errors.Is(e, ErrTemplateInvalid) {
		t.Fatalf("oversized template line: %v", e)
	}
}
func TestEmbeddedTemplateAndIncompleteRows(t *testing.T) {
	rows, e := ParseTemplate("cisco/show_running_config.textfsm", "interface Gi0/0\n description uplink\n!\ninterface Gi0/1\n shutdown\n!\n")
	if e != nil || len(rows) != 2 || rows[0]["DESCRIPTION"] != "uplink" || rows[1]["DESCRIPTION"] != "" {
		t.Fatalf("offline: %#v %v", rows, e)
	}
	if _, e = ParseTemplate("missing.textfsm", ""); !errors.Is(e, ErrTemplateMissing) {
		t.Fatal(e)
	}
	if _, e = Parse("fortinet", "show full-configuration", "config system interface\n    edit wan1\n        set description\n    next\nend\n"); !errors.Is(e, ErrParse) {
		t.Fatalf("invalid description: %v", e)
	}
}
