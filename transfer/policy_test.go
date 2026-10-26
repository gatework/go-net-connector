package transfer

import "testing"

func TestPolicyHostOverridesAndNullableVendorValues(t *testing.T) {
	p, e := NewPolicy(PolicyOptions{Server: "SERVER", Servers: map[string]string{"2001:db8::1": "other"}, SourceFiles: map[string]any{"huawei": map[string]any{"default": "flash:/startup.cfg", "hosts": map[string]any{"2001:db8::1": nil}}}, VRFs: map[string]any{"cisco_nxos": "blue"}})
	if e != nil {
		t.Fatal(e)
	}
	a, e := p.For("192.0.2.1", "huawei")
	if e != nil || a.Server != "server" || a.SourceFile != "flash:/startup.cfg" {
		t.Fatal(a, e)
	}
	b, e := p.For("2001:db8::1", "huawei")
	if e != nil || b.Server != "other" || b.SourceFile != "" {
		t.Fatal(b, e)
	}
}
func TestPolicyRejectsSharedRootsAndUnsafeUnusedPolicies(t *testing.T) {
	for _, o := range []PolicyOptions{
		{Roots: map[string]string{"a": "/tmp/root", "b": "/tmp/root"}},
		{Servers: map[string]string{"192.0.2.1/24": "a"}},
		{VRFs: map[string]any{"cisco_ios": "default"}},
		{SourceInterfaces: map[string]any{"radware": "bad"}},
		{Root: "ssh://root:secret@server/path"},
	} {
		if _, e := NewPolicy(o); e == nil {
			t.Fatal("accepted invalid policy")
		}
	}
}
