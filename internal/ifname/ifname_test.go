package ifname

import "testing"

func TestShortUsesSharedInterfaceAliases(t *testing.T) {
	for _, test := range []struct {
		name string
		want string
		ok   bool
	}{
		{name: "Ten-GigabitEthernet1/0/2", want: "Te1/0/2", ok: true},
		{name: "GigabitEthernet0/0/1", want: "Gi0/0/1", ok: true},
		{name: "Loopback12", want: "Loopback12", ok: false},
	} {
		got, ok := Short(test.name)
		if got != test.want || ok != test.ok {
			t.Errorf("Short(%q) = %q, %t; want %q, %t", test.name, got, ok, test.want, test.ok)
		}
	}
}

func TestConfigNameExpandsCommandPrefixes(t *testing.T) {
	for _, test := range []struct {
		name string
		want string
	}{
		{name: "Te1/0/2", want: "TenGigabitEthernet1/0/2"},
		{name: "xge1/0/2", want: "Ten-GigabitEthernet1/0/2"},
		{name: "Loopback12", want: "Loopback12"},
	} {
		if got := ConfigName(test.name); got != test.want {
			t.Errorf("ConfigName(%q) = %q; want %q", test.name, got, test.want)
		}
	}
}
