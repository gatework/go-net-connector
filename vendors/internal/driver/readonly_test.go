package driver

import "testing"

func TestReadOnlyCommandsRejectWritesAndRedirection(t *testing.T) {
	cisco := ReadOnlyCommands{Verbs: []string{"show", "ping"}}
	for command, want := range map[string]bool{
		"show running-config":                true,
		"  SHOW version  ":                   true,
		"show run | include interface":       true,
		"show run | i hostname | exclude !":  true,
		"configure terminal":                 false,
		"show run | redirect flash:copy.txt": false,
		"show run | tee flash:copy.txt":      false,
		"show run | append flash:copy.txt":   false,
		"show run > flash:copy.txt":          false,
		"show run ; reload":                  false,
		"show run |":                         false,
		"copy running-config startup-config": false,
		"":                                   false,
	} {
		if got := cisco.IsReadOnly(command); got != want {
			t.Errorf("%q = %t, want %t", command, got, want)
		}
	}
	junos := ReadOnlyCommands{Verbs: []string{"show"}}
	for command, want := range map[string]bool{
		"show configuration | display set | no-more": true,
		"show configuration | save /var/tmp/x":       false,
		"show configuration | display omit":          false,
	} {
		if got := junos.IsReadOnly(command); got != want {
			t.Errorf("junos %q = %t, want %t", command, got, want)
		}
	}
	radware := ReadOnlyCommands{PathPrefixes: []string{"/info", "/stats"}, Verbs: []string{"/cfg/dump"}}
	for command, want := range map[string]bool{"/info/sys": true, "/stats/port 1": true, "/cfg/dump": true, "/cfg/sys/apply": false, "/information": false} {
		if got := radware.IsReadOnly(command); got != want {
			t.Errorf("radware %q = %t, want %t", command, got, want)
		}
	}
}
