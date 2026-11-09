package vendors

import "testing"

func TestConfigurationPromptTransitions(t *testing.T) {
	for _, tc := range []struct{ vendor, command, current, next string }{
		{"radware", "/cfg/dump", ">> Standalone ADC - Main# ", ">> Standalone ADC - Configuration#"},
		{"palo_alto", "configure", "admin@fw>", "admin@fw#"},
		{"palo_alto", "exit", "admin@fw#", "admin@fw>"},
	} {
		t.Run(tc.vendor+"/"+tc.command, func(t *testing.T) {
			profile := profileForTest(t, tc.vendor)
			pattern, err := profile.NewConfigCollector().MatchPrompt(tc.command, tc.current)
			if err != nil || !matchesPattern(pattern, tc.next) {
				t.Fatalf("prompt transition to %q failed: %v", tc.next, err)
			}
		})
	}
}
