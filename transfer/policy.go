package transfer

import (
	"net/netip"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gatework/go-net-connector/vendors/dialect"
)

// PolicyOptions configures the TFTP servers, verification roots and per-
// vendor source settings.
type PolicyOptions struct {
	Server, Root                        string
	Servers, Roots                      map[string]string
	SourceFiles, SourceInterfaces, VRFs map[string]any
}

// Target is the resolved TFTP options for one device and the verification
// root of its server.
type Target struct {
	TFTPOptions
	Root string
}
type vendorValue struct {
	fallback string
	hosts    map[string]string
}

// Policy resolves per-device TFTP targets.
type Policy struct {
	server                  string
	servers, roots          map[string]string
	files, interfaces, vrfs map[string]vendorValue
	platforms               map[string]TFTPPolicy
}

func exactAddress(host string) (string, error) {
	a, e := netip.ParseAddr(host)
	if e != nil || a.Zone() != "" {
		return "", ErrInvalid
	}
	return a.String(), nil
}
func serverName(host string) (string, error) {
	if ValidateServer(host) != nil {
		return "", ErrInvalid
	}
	if a, e := netip.ParseAddr(host); e == nil {
		return a.String(), nil
	}
	return strings.ToLower(host), nil
}

// VerificationRoot validates a local path or a password-free SSH archive URI.
// It does not establish a connection or create a directory.
func VerificationRoot(root string) (string, error) {
	if root == "" || strings.ContainsAny(root, "\r\n\x00") {
		return "", ErrInvalid
	}
	if strings.HasPrefix(root, "ssh://") {
		u, e := url.Parse(root)
		if e != nil || u.Scheme != "ssh" || u.User == nil || !vrfPattern.MatchString(u.User.Username()) || ValidateServer(u.Hostname()) != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || !strings.HasPrefix(u.Path, "/") || strings.ContainsAny(u.Path, "\r\n\x00") {
			return "", ErrInvalid
		}
		if _, ok := u.User.Password(); ok {
			return "", ErrInvalid
		}
		if u.Port() != "" {
			n, e := strconv.Atoi(u.Port())
			if e != nil || n < 1 || n > 65535 {
				return "", ErrInvalid
			}
		}
		for part := range strings.SplitSeq(u.Path, "/") {
			if part == "." || part == ".." {
				return "", ErrInvalid
			}
		}
		return root, nil
	}
	p, e := filepath.Abs(root)
	if e != nil {
		return "", ErrInvalid
	}
	if resolved, e := filepath.EvalSymlinks(p); e == nil {
		p = resolved
	}
	return p, nil
}

// NewPolicy validates o against the TFTP-capable platforms in registry and
// returns the policy that resolves per-device transfer targets.
func NewPolicy(o PolicyOptions, registry dialect.Registry) (*Policy, error) {
	if registry == nil {
		return nil, ErrInvalid
	}
	names := registry.Names()
	if len(names) == 0 {
		return nil, ErrInvalid
	}
	p := &Policy{servers: map[string]string{}, roots: map[string]string{}, platforms: make(map[string]TFTPPolicy)}
	for _, name := range names {
		declaration, err := registry.Lookup(name)
		if err != nil {
			return nil, ErrInvalid
		}
		if platform, ok := declaration.(TFTPPolicy); ok {
			p.platforms[name] = platform
		}
	}
	if e := p.setServers(o); e != nil {
		return nil, e
	}
	if e := p.setRoots(o); e != nil {
		return nil, e
	}
	if e := p.setVendorValues(o); e != nil {
		return nil, e
	}
	return p, nil
}

// For resolves the target for host on vendor's platform.
func (p *Policy) For(host, vendor string) (Target, error) {
	h, e := exactAddress(host)
	if e != nil {
		return Target{}, e
	}
	server := p.server
	if s, ok := p.servers[h]; ok {
		server = s
	}
	get := func(m map[string]vendorValue) string {
		v := m[vendor]
		if s, ok := v.hosts[h]; ok {
			return s
		}
		return v.fallback
	}
	vrf := get(p.vrfs)
	platform, ok := p.platforms[vendor]
	if !ok {
		return Target{}, ErrUnsupported
	}
	if vrf == "" {
		vrf = platform.DefaultVRF()
	}
	root := p.roots[server]
	if platform.RequiresVerificationRoot() && server != "" && root == "" {
		return Target{}, ErrInvalid
	}
	return Target{TFTPOptions: TFTPOptions{Server: server, SourceFile: get(p.files), SourceInterface: get(p.interfaces), VRF: vrf}, Root: root}, nil
}

// setServers records the default server and the per-host servers.
func (p *Policy) setServers(o PolicyOptions) error {
	if o.Server != "" {
		var e error
		if p.server, e = serverName(o.Server); e != nil {
			return e
		}
	}
	for host, server := range o.Servers {
		h, e := exactAddress(host)
		if e != nil {
			return e
		}
		s, e := serverName(server)
		if e != nil {
			return e
		}
		if _, ok := p.servers[h]; ok {
			return ErrInvalid
		}
		p.servers[h] = s
	}
	return nil
}

// setRoots records the verification root of each server, including the
// default root for the default server, and rejects roots shared by two
// servers.
func (p *Policy) setRoots(o PolicyOptions) error {
	for server, root := range o.Roots {
		s, e := serverName(server)
		if e != nil {
			return e
		}
		r, e := VerificationRoot(root)
		if e != nil {
			return e
		}
		if _, ok := p.roots[s]; ok {
			return ErrInvalid
		}
		p.roots[s] = r
	}
	if o.Root != "" {
		root, e := VerificationRoot(o.Root)
		if e != nil {
			return e
		}
		if p.server != "" {
			if r, ok := p.roots[p.server]; ok && r != root {
				return ErrInvalid
			}
			p.roots[p.server] = root
		}
	}
	seen := map[string]bool{}
	for _, root := range p.roots {
		if seen[root] {
			return ErrInvalid
		}
		seen[root] = true
	}
	return nil
}

// vendorValues decodes per-vendor settings: each value is a string, or a
// map with an optional "default" and "hosts" keyed by exact address.
func vendorValues(values map[string]any, platforms map[string]TFTPPolicy, validate func(TFTPPolicy, string) error) (map[string]vendorValue, error) {
	out := map[string]vendorValue{}
	for vendor, raw := range values {
		platform, ok := platforms[vendor]
		if !ok {
			return nil, ErrInvalid
		}
		normalize := func(raw any) (string, error) {
			if raw == nil {
				return "", nil
			}
			s, ok := raw.(string)
			if !ok || s == "" {
				return "", ErrInvalid
			}
			return s, validate(platform, s)
		}
		value, e := decodeVendorValue(raw, normalize)
		if e != nil {
			return nil, e
		}
		out[vendor] = value
	}
	return out, nil
}

// decodeVendorValue decodes one vendor's setting with normalize.
func decodeVendorValue(raw any, normalize func(any) (string, error)) (vendorValue, error) {
	value := vendorValue{hosts: map[string]string{}}
	m, ok := raw.(map[string]any)
	if !ok {
		var e error
		value.fallback, e = normalize(raw)
		return value, e
	}
	for k := range m {
		if k != "default" && k != "hosts" {
			return vendorValue{}, ErrInvalid
		}
	}
	var e error
	if value.fallback, e = normalize(m["default"]); e != nil {
		return vendorValue{}, e
	}
	rawHosts, ok := m["hosts"]
	if !ok {
		return value, nil
	}
	hosts, ok := rawHosts.(map[string]any)
	if !ok {
		return vendorValue{}, ErrInvalid
	}
	for host, raw := range hosts {
		h, e := exactAddress(host)
		if e != nil {
			return vendorValue{}, e
		}
		if _, ok := value.hosts[h]; ok {
			return vendorValue{}, ErrInvalid
		}
		if value.hosts[h], e = normalize(raw); e != nil {
			return vendorValue{}, e
		}
	}
	return value, nil
}

// setVendorValues records the per-vendor source files, source interfaces
// and VRFs, each validated by its platform.
func (p *Policy) setVendorValues(o PolicyOptions) error {
	preflight := func(platform TFTPPolicy, options TFTPOptions) error {
		options.Server = "preflight.invalid"
		return platform.ValidateOptions("192.0.2.1", options)
	}
	var e error
	p.files, e = vendorValues(o.SourceFiles, p.platforms, func(platform TFTPPolicy, v string) error {
		if v == "" {
			return nil
		}
		if ValidateSource(v) != nil {
			return ErrInvalid
		}
		return preflight(platform, TFTPOptions{SourceFile: v})
	})
	if e != nil {
		return e
	}
	p.interfaces, e = vendorValues(o.SourceInterfaces, p.platforms, func(platform TFTPPolicy, v string) error {
		return preflight(platform, TFTPOptions{SourceInterface: v})
	})
	if e != nil {
		return e
	}
	p.vrfs, e = vendorValues(o.VRFs, p.platforms, func(platform TFTPPolicy, v string) error {
		return preflight(platform, TFTPOptions{VRF: v})
	})
	return e
}
