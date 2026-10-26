package connector

import "testing"

func TestReplaceRequiresExplicitKnownHosts(t *testing.T) {
	for _, path := range []string{"", "   "} {
		if _, err := New(fixtureProfile(), ConnectionConfig{Host: "fixture", HostKeyPolicy: "replace", KnownHosts: path}); err == nil {
			t.Errorf("accepted implicit replace path %q", path)
		}
	}
	for _, policy := range []string{"strict", "accept_new", "replace"} {
		path := ""
		if policy == "replace" {
			path = t.TempDir() + "/known_hosts"
		}
		if _, err := New(fixtureProfile(), ConnectionConfig{Host: "fixture", HostKeyPolicy: HostKeyPolicy(policy), KnownHosts: path}); err != nil {
			t.Fatal(policy, err)
		}
	}
}
