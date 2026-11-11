package bootstrap

import (
	"net/url"
	"strings"

	"github.com/gatework/go-net-connector/config"
	"github.com/gatework/go-net-connector/netdisco"
)

// validatePolicy checks the inventory and topology settings that the
// configuration schema cannot express.
func (s *Services) validatePolicy() error {
	if e := s.validateLimits(); e != nil {
		return e
	}
	if e := s.validateSource(); e != nil {
		return e
	}
	return s.validateDatabase()
}

// validateLimits checks the inventory limits and topology bounds.
func (s *Services) validateLimits() error {
	p := s.policy
	for _, key := range []string{"page_size", "max_pages", "max_devices", "max_response_bytes", "max_inventory_bytes"} {
		if v := p.Value("netdisco." + key); v != nil {
			n, ok := v.(int)
			if !ok || n < 1 {
				return config.ErrInvalid
			}
		}
	}
	if v := p.Value("netdisco.inventory_timeout"); v != nil && durationValue(p, "netdisco.inventory_timeout") <= 0 {
		return config.ErrInvalid
	}
	if durationValue(p, "netdisco.cache_ttl") < 0 {
		return config.ErrInvalid
	}
	if p.Value("topology.parallel") != nil {
		if n := integerValue(p, "topology.parallel"); n < 1 || n > 500 {
			return config.ErrInvalid
		}
	}
	if n := integerValue(p, "topology.limit"); n < 0 {
		return config.ErrInvalid
	}
	return nil
}

// validateSource checks the settings of the selected inventory source.
func (s *Services) validateSource() error {
	p := s.policy
	switch stringValue(p, "netdisco.source") {
	case "http":
		if p.Value("netdisco.query") != nil || p.Value("netdisco.query_params") != nil {
			return config.ErrInvalid
		}
		if raw := stringValue(p, "netdisco.url"); raw != "" {
			u, e := url.Parse(raw)
			if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Scheme != "https" && !(u.Scheme == "http" && boolValue(p, "netdisco.allow_insecure_http")) {
				return config.ErrInvalid
			}
		}
		return nil
	case "postgres":
		params, _ := p.Value("netdisco.query_params").([]any)
		_, e := netdisco.NewPostgres(netdisco.PostgresOptions{Connection: "host=localhost user=preflight dbname=preflight sslmode=disable", Query: stringValue(p, "netdisco.query"), Parameters: params, Limits: s.limits()})
		return e
	default:
		return config.ErrInvalid
	}
}

// validateDatabase checks the database connection settings.
func (s *Services) validateDatabase() error {
	p := s.policy
	if n := integerValue(p, "netdisco.db_port"); n < 1 || n > 65535 {
		return config.ErrInvalid
	}
	if n := integerValue(p, "netdisco.db_connect_timeout"); n < 1 {
		return config.ErrInvalid
	}
	mode := stringValue(p, "netdisco.db_sslmode")
	if mode != "" && !strings.Contains("|disable|allow|prefer|require|verify-ca|verify-full|", "|"+mode+"|") {
		return config.ErrInvalid
	}
	return nil
}
