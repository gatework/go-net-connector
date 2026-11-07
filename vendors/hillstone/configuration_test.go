package hillstone

import "testing"

func TestConfigurationCleaningRemovesPagerBackspaces(t *testing.T) {
	input := "show configuration running\r\nhostname fw\n\x00\b\b  \b\bfoo\nfw#"
	got, err := cleanOutput(input)
	if err != nil || got != "show configuration running\r\nhostname fw\nfoo\nfw#" {
		t.Fatalf("hillstone %q %v", got, err)
	}
}
