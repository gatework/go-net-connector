package transfer

import (
	"net/netip"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gatework/go-net-connector/vendors/dialect"
)

type PolicyOptions struct {
	Server, Root                        string
	Servers, Roots                      map[string]string
	SourceFiles, SourceInterfaces, VRFs map[string]any
}
type Target struct {
	TFTPOptions
	Root string
}
type vendorValue struct {
	fallback string
	hosts    map[string]string
}
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
		for _, part := range strings.Split(u.Path, "/") {
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
	if real, e := filepath.EvalSymlinks(p); e == nil {
		p = real
	}
	return p, nil
}
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
		platform, ok := declaration.(TFTPPolicy)
		if ok {
			p.platforms[name] = platform
		}
	}
	var e error
	if o.Server != "" {
		p.server, e = serverName(o.Server)
		if e != nil {
			return nil, e
		}
	}
	for host, server := range o.Servers {
		h, e := exactAddress(host)
		if e != nil {
			return nil, e
		}
		s, e := serverName(server)
		if e != nil {
			return nil, e
		}
		if _, ok := p.servers[h]; ok {
			return nil, ErrInvalid
		}
		p.servers[h] = s
	}
	for server, root := range o.Roots {
		s, e := serverName(server)
		if e != nil {
			return nil, e
		}
		r, e := VerificationRoot(root)
		if e != nil {
			return nil, e
		}
		if _, ok := p.roots[s]; ok {
			return nil, ErrInvalid
		}
		p.roots[s] = r
	}
	if o.Root != "" {
		root, e := VerificationRoot(o.Root)
		if e != nil {
			return nil, e
		}
		if p.server != "" {
			if r, ok := p.roots[p.server]; ok && r != root {
				return nil, ErrInvalid
			}
			p.roots[p.server] = root
		}
	}
	seen := map[string]bool{}
	for _, root := range p.roots {
		if seen[root] {
			return nil, ErrInvalid
		}
		seen[root] = true
	}
	p.files, e = vendorValues(o.SourceFiles, p.platforms, func(platform TFTPPolicy, v string) error {
		if v == "" {
			return nil
		}
		if ValidateSource(v) != nil {
			return ErrInvalid
		}
		return platform.ValidateOptions("192.0.2.1", TFTPOptions{Server: "preflight.invalid", SourceFile: v})
	})
	if e != nil {
		return nil, e
	}
	p.interfaces, e = vendorValues(o.SourceInterfaces, p.platforms, func(platform TFTPPolicy, value string) error {
		return platform.ValidateOptions("192.0.2.1", TFTPOptions{Server: "preflight.invalid", SourceInterface: value})
	})
	if e != nil {
		return nil, e
	}
	p.vrfs, e = vendorValues(o.VRFs, p.platforms, func(platform TFTPPolicy, v string) error {
		return platform.ValidateOptions("192.0.2.1", TFTPOptions{Server: "preflight.invalid", VRF: v})
	})
	if e != nil {
		return nil, e
	}
	return p, nil
}
func vendorValues(values map[string]any, platforms map[string]TFTPPolicy, validate func(TFTPPolicy, string) error) (map[string]vendorValue, error) {
	out := map[string]vendorValue{}
	for vendor, raw := range values {
		platform, ok := platforms[vendor]
		if !ok {
			return nil, ErrInvalid
		}
		value := vendorValue{hosts: map[string]string{}}
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
		if m, ok := raw.(map[string]any); ok {
			for k := range m {
				if k != "default" && k != "hosts" {
					return nil, ErrInvalid
				}
			}
			var e error
			value.fallback, e = normalize(m["default"])
			if e != nil {
				return nil, e
			}
			if raw, ok := m["hosts"]; ok {
				hosts, ok := raw.(map[string]any)
				if !ok {
					return nil, ErrInvalid
				}
				for host, raw := range hosts {
					h, e := exactAddress(host)
					if e != nil {
						return nil, e
					}
					if _, ok := value.hosts[h]; ok {
						return nil, ErrInvalid
					}
					s, e := normalize(raw)
					if e != nil {
						return nil, e
					}
					value.hosts[h] = s
				}
			}
		} else {
			var e error
			value.fallback, e = normalize(raw)
			if e != nil {
				return nil, e
			}
		}
		out[vendor] = value
	}
	return out, nil
}
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
