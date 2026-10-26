package vendors

import (
	"errors"
	"testing"

	"github.com/gatework/go-net-connector/connector"
)

func TestConfigCleaningRetainsVendorSemantics(t *testing.T) {
	header := "show running-config\nBuilding configuration...\n\nCurrent configuration : 123 bytes\n!\n"
	volatile := "! Last configuration change at 12:00\n! NVRAM config last updated at 12:00\n"
	body := "hostname switch\nbanner motd @\n" + volatile + "show running-config\nswitch#\n@\nend\nswitch#"
	for _, v := range []string{"cisco_ios", "cisco_nxos"} {
		s := &configStrategy{vendor: v}
		got, e := s.clean(header+volatile+body, "switch#")
		if e != nil || got != header+body {
			t.Fatalf("%s: %q %v", v, got, e)
		}
	}
	centec := "hostname Switch\nbanner motd ^\nSwitch#\nshow running-config\n^\n"
	got, e := (&configStrategy{vendor: "centec"}).clean("show running-config\r\n"+centec+"Switch#", "Switch#")
	if e != nil || got != centec {
		t.Fatalf("centec: %q %v", got, e)
	}
	hill := "show configuration running\r\nhostname fw\n\x00\b\b  \b\bfoo\nfw#"
	got, e = (&configStrategy{vendor: "hillstone"}).clean(hill, "fw#")
	if e != nil || got != "show configuration running\r\nhostname fw\nfoo\nfw#" {
		t.Fatalf("hillstone %q %v", got, e)
	}
	got, e = (&configStrategy{vendor: "h3c"}).clean("dis cur\r\nsysname core\r\n<core>", "<core>")
	if e != nil || got != "dis cur\nsysname core\n<core>" {
		t.Fatalf("h3c %q %v", got, e)
	}
}
func TestConfigPrompts(t *testing.T) {
	s := &configStrategy{vendor: "radware"}
	if got := s.Prompt("/cfg/dump", ">> Standalone ADC - Main# "); got != ">> Standalone ADC - Configuration#" {
		t.Fatal(got)
	}
	s = &configStrategy{vendor: "palo_alto"}
	if s.Prompt("configure", "admin@fw>") != "admin@fw#" || s.Prompt("exit", "admin@fw#") != "admin@fw>" {
		t.Fatal("PAN view")
	}
}
func TestPANSetQuotes(t *testing.T) {
	valid := []string{"set description \"first  \nsecond\"", "set description \"first\nsecond\"\nset system host-name firewall", "set description 'first\\\nsecond'\n\nset system host-name firewall", "set description \"escaped \\\"quote\\\"\nsecond\"", "set description 'backslash\\'\nset system host-name firewall", "set description escaped\\\"quote\nset description \\\\"}
	for _, text := range valid {
		got, e := (&configStrategy{vendor: "palo_alto"}).clean(text+"\n[edit]\nadmin@fw#", "admin@fw#")
		if e != nil || got != text {
			t.Fatalf("valid: %q got %q %v", text, got, e)
		}
	}
	for _, text := range []string{"", "<config/>", "set description \"first\nsecond", "set description 'first\nsecond", "set description \"first\nsecond\"\nnot-a-set-command", "set description 'first\\'\nnot-a-set-command"} {
		if _, e := (&configStrategy{vendor: "palo_alto"}).clean(text, "admin@fw#"); !errors.Is(e, connector.ErrIncomplete) {
			t.Fatalf("invalid: %q %v", text, e)
		}
	}
}
