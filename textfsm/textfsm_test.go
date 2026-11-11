package textfsm

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestParseTemplateFixtures(t *testing.T) {
	cases := []struct{ template, text, key, want string }{
		{"h3c/display_lldp_neighbor_information_list.textfsm", "Local Interface Chassis ID Port ID System Name\nGE1/0/1 0011-2233-4455 GE1/0/2 peer\n", "NEIGHBOR_NAME", "peer"},
		{"h3c/display_lldp_neighbor_information_list.textfsm", "System Name Local Interface Chassis ID Port ID\npeer GE1/0/1 0011-2233-4455 GE1/0/2\n", "LOCAL_INTERFACE", "GE1/0/1"},
		{"cisco/show_running_config.textfsm", "interface Ethernet1\n description uplink\n!\n", "DESCRIPTION", "uplink"},
		{"cisco_nxos/show_running_config.textfsm", "interface Ethernet1\n description uplink\n\n", "DESCRIPTION", "uplink"},
		{"h3c/display_current_configuration.textfsm", "interface Ethernet1\n description uplink\n#\n", "DESCRIPTION", "uplink"},
		{"huawei/display_current_configuration.textfsm", "interface Ethernet1\n description uplink\n#\n", "DESCRIPTION", "uplink"},
		{"hillstone/show_configuration_running.textfsm", "interface Ethernet1\n description uplink\nexit\n", "DESCRIPTION", "uplink"},
		{"palo_alto/show.textfsm", "set network interface ethernet Ethernet1 comment uplink\n", "DESCRIPTION", "uplink"},
		{"radware/cfg_dump.textfsm", "/cfg/port 1/name uplink", "DESCRIPTION", "uplink"},
		{"fortinet/show_full_configuration.textfsm", "config system interface\n    edit Ethernet1\n        set description uplink\n    next\nend\n", "DESCRIPTION", "uplink"},
		{"juniper/show_configuration_display_set.textfsm", "set interfaces Ethernet1 description uplink\n", "DESCRIPTION", "uplink"},
		{"cisco/show_cdp_neighbors_detail.textfsm", "Device ID: core\nInterface: Gi0/1, Port ID (outgoing port): Gi0/2\n", "NEIGHBOR_NAME", "core"},
		{"cisco_nxos/show_cdp_neighbors_detail.textfsm", "Device-ID: core\nInterface: Ethernet1/1, Port ID (outgoing port): Ethernet1/2\n", "LOCAL_INTERFACE", "Ethernet1/1"},
		{"hillstone/show_lldp_neighbor_information.textfsm", "core ethernet0/1 001122334455 Ethernet1/2\n", "NEIGHBOR_NAME", "core"},
		{"huawei/display_lldp_neighbor_brief.textfsm", "Local Intf Neighbor Dev Neighbor Intf Exptime\nGE0/0/1 core GE0/0/2 120\n", "NEIGHBOR_NAME", "core"},
		{"palo_alto/show_lldp_neighbors_all.textfsm", "Local information:\nLocal interface: ethernet1/1\nChassis ID: 001122334455\nPort ID: Ethernet1/2\nSystem name: core\n", "LOCAL_INTERFACE", "ethernet1/1"},
		{"fortinet/diagnose_lldp_rx_neighbor.textfsm", "1 port 'wan1' 5 mac 00:11:22:33:44:55 chassis 00:11:22:33:44:66 port 'Te1/0/1' system 'core01'\n", "NEIGHBOR_NAME", "core01"},
		{"juniper/show_lldp_neighbors_detail.textfsm", "Local Interface : ge-0/0/0\nChassis ID : 00:11:22:33:44:55\nPort ID : Ethernet1/1\nSystem name : core\nManagement Info\n  Port ID : 1\n", "NEIGHBOR_INTERFACE", "Ethernet1/1"},
	}
	for _, c := range cases {
		t.Run(c.template, func(t *testing.T) {
			r, e := ParseTemplate(c.template, c.text)
			if e != nil {
				t.Fatal(e)
			}
			if len(r) != 1 || r[0][c.key] != c.want {
				t.Fatalf("rows = %#v", r)
			}
		})
	}
}

func TestParseTemplateMissing(t *testing.T) {
	for _, name := range []string{"cisco/absent.textfsm", "../cisco/show_running_config.textfsm"} {
		if _, e := ParseTemplate(name, ""); !errors.Is(e, ErrTemplateMissing) {
			t.Fatalf("%q: error %v", name, e)
		}
	}
}

func TestParseTemplateFreshAndStrict(t *testing.T) {
	t.Parallel()
	template := "Value Required NAME (\\S+)\nValue STATE (up|down)\n\nStart\n  ^${NAME}\\s+${STATE}$$ -> Record\n"
	for i := range 12 {
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
	if _, e = ParseTemplate("fortinet/show_full_configuration.textfsm", "config system interface\n    edit wan1\n        set description\n    next\nend\n"); !errors.Is(e, ErrParse) {
		t.Fatalf("invalid description: %v", e)
	}
}

func TestEmbeddedTemplateDoesNotRecompileEachCall(t *testing.T) {
	parse := func() {
		rows, err := ParseTemplate("cisco/show_running_config.textfsm", "interface Ethernet1\n description uplink\n!\n")
		if err != nil || len(rows) != 1 {
			t.Fatal(err)
		}
	}
	parse()
	if n := testing.AllocsPerRun(10, parse); n > 100 {
		t.Fatalf("steady parse allocations=%v, want <=100", n)
	}
}

// filldownTemplate carries Filldown state across records within one parse.
const filldownTemplate = `Value Filldown VRF (\S+)
Value Required INTERFACE (\S+)

Start
  ^IP Interface Status for VRF "${VRF}"
  ^${INTERFACE}\s+\S+\s+\S+ -> Record
`

func TestTemplateResultsAreIndependent(t *testing.T) {
	t.Parallel()
	for i := range 16 {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			t.Parallel()
			for range 10 {
				rows, err := ParseTemplate(filldownTemplate, "IP Interface Status for VRF \"management\"(2)\nmgmt0 192.0.2.1 protocol-up/link-up/admin-up\n")
				if err != nil || len(rows) != 1 || rows[0]["VRF"] != "management" {
					t.Fatalf("filldown %v %v", rows, err)
				}
				rows[0]["VRF"] = "caller mutation"
				rows, err = ParseTemplate(filldownTemplate, "mgmt0 192.0.2.1 protocol-up/link-up/admin-up\n")
				if err != nil || len(rows) != 1 || rows[0]["VRF"] != "" {
					t.Fatalf("state leaked: %v %v", rows, err)
				}
			}
		})
	}
}
