package bootstrap

import (
	"github.com/gatework/go-net-connector/config"
	"github.com/gatework/go-net-connector/transfer"
)

func (s *Services) TFTPPolicy() (*transfer.Policy, error) {
	p := s.policy
	servers, e := mapping(p, "tftp.servers")
	if e != nil {
		return nil, e
	}
	roots, e := mapping(p, "tftp.verification_roots")
	if e != nil {
		return nil, e
	}
	get := func(key string) (map[string]any, error) {
		v := p.Value(key)
		if v == nil {
			return map[string]any{}, nil
		}
		m, ok := v.(map[string]any)
		if !ok {
			return nil, config.ErrInvalid
		}
		return m, nil
	}
	files, e := get("tftp.source_files")
	if e != nil {
		return nil, e
	}
	for _, vendor := range []string{"h3c", "h3c_wireless", "huawei"} {
		if _, exists := files[vendor]; exists {
			continue
		}
		if v := str(p, "tftp."+vendor+"_source_file", ""); v != "" {
			files[vendor] = v
		} else if vendor == "huawei" {
			files[vendor] = "flash:/startup.cfg"
		}
	}
	interfaces, e := get("tftp.source_interfaces")
	if e != nil {
		return nil, e
	}
	vrfs, e := get("tftp.vrfs")
	if e != nil {
		return nil, e
	}
	return transfer.NewPolicy(transfer.PolicyOptions{Server: str(p, "tftp.server", ""), Root: str(p, "tftp.verification_root", ""), Servers: servers, Roots: roots, SourceFiles: files, SourceInterfaces: interfaces, VRFs: vrfs})
}

// ArchivePassword resolves the archive credential only during online preparation.
func (s *Services) ArchivePassword() string { return s.secret("NC_TFTP_SSH_PASSWORD") }
