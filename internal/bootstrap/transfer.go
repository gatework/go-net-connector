package bootstrap

import (
	"time"

	"github.com/gatework/go-net-connector/config"
	"github.com/gatework/go-net-connector/transfer"
)

func (s *Services) TFTPPolicy() (*transfer.Policy, error) {
	if s.tftpPolicy != nil {
		return s.tftpPolicy, nil
	}
	return s.buildTFTPPolicy()
}

func (s *Services) buildTFTPPolicy() (*transfer.Policy, error) {
	p := s.policy
	servers, e := stringMap(p, "tftp.servers")
	if e != nil {
		return nil, e
	}
	roots, e := stringMap(p, "tftp.verification_roots")
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
	interfaces, e := get("tftp.source_interfaces")
	if e != nil {
		return nil, e
	}
	vrfs, e := get("tftp.vrfs")
	if e != nil {
		return nil, e
	}
	return transfer.NewPolicy(transfer.PolicyOptions{Server: stringValue(p, "tftp.server"), Root: stringValue(p, "tftp.verification_root"), Servers: servers, Roots: roots, SourceFiles: files, SourceInterfaces: interfaces, VRFs: vrfs}, s.registry)
}

// ArchivePassword resolves the archive credential only during online preparation.
func (s *Services) ArchivePassword() string { return s.secret("NC_TFTP_SSH_PASSWORD") }

// TFTPUploadTimeout bounds device upload commands in CLI batches.
func (s *Services) TFTPUploadTimeout() time.Duration {
	return s.settings.tftpUploadTimeout
}
