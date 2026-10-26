package bootstrap

import (
	"bytes"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/gatework/go-net-connector/config"
	"github.com/gatework/go-net-connector/netdisco"
)

func orderedConnectionIdentity(values map[string]any) (json.RawMessage, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	first := true
	for _, key := range []string{"port", "connect_timeout", "sslmode", "sslrootcert", "host", "dbname", "user"} {
		v, ok := values[key]
		if !ok {
			continue
		}
		data, e := json.Marshal(v)
		if e != nil {
			return nil, e
		}
		k, _ := json.Marshal(key)
		if !first {
			b.WriteByte(',')
		}
		first = false
		b.Write(k)
		b.WriteByte(':')
		b.Write(data)
	}
	b.WriteByte('}')
	return json.RawMessage(b.Bytes()), nil
}
func (s *Services) validatePolicy() error {
	p := s.policy
	for _, key := range []string{"page_size", "max_pages", "max_devices", "max_response_bytes", "max_inventory_bytes"} {
		if v := p.Value("netdisco." + key); v != nil {
			n, ok := v.(int)
			if !ok || n < 1 {
				return config.ErrInvalid
			}
		}
	}
	if v := p.Value("netdisco.inventory_timeout"); v != nil && duration(p, "netdisco.inventory_timeout", 0) <= 0 {
		return config.ErrInvalid
	}
	if duration(p, "netdisco.cache_ttl", 0) < 0 {
		return config.ErrInvalid
	}
	if n := number(p, "topology.parallel", 4); n < 1 || n > 500 {
		return config.ErrInvalid
	}
	if n := number(p, "topology.limit", 0); n < 0 {
		return config.ErrInvalid
	}
	switch str(p, "netdisco.source", "http") {
	case "http":
		if p.Value("netdisco.query") != nil || p.Value("netdisco.query_params") != nil {
			return config.ErrInvalid
		}
		if raw := str(p, "netdisco.url", ""); raw != "" {
			u, e := url.Parse(raw)
			if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Scheme != "https" && !(u.Scheme == "http" && boolean(p, "netdisco.allow_insecure_http", false)) {
				return config.ErrInvalid
			}
		}
	case "postgres":
		params, _ := p.Value("netdisco.query_params").([]any)
		if _, e := netdisco.NewPostgres(netdisco.PostgresOptions{Connection: "host=localhost user=preflight dbname=preflight sslmode=disable", Query: str(p, "netdisco.query", ""), Parameters: params, Limits: s.limits()}); e != nil {
			return e
		}
	default:
		return config.ErrInvalid
	}
	if n := number(p, "netdisco.db_port", 5432); n < 1 || n > 65535 {
		return config.ErrInvalid
	}
	if n := number(p, "netdisco.db_connect_timeout", 10); n < 1 {
		return config.ErrInvalid
	}
	mode := str(p, "netdisco.db_sslmode", "")
	if mode != "" && !strings.Contains("|disable|allow|prefer|require|verify-ca|verify-full|", "|"+mode+"|") {
		return config.ErrInvalid
	}
	_, e := s.TFTPPolicy()
	return e
}
