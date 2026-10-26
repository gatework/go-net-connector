// Package bootstrap is the only dependency injection composition root.
package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/gatework/go-net-connector/config"
	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/inventory"
	"github.com/gatework/go-net-connector/netdisco"
	"github.com/gatework/go-net-connector/vendors"
	"github.com/samber/do/v2"
)

var ErrCredentials = netdisco.ErrMissingCredentials

type Secret func(string) string
type DeviceFactory func(connector.Profile, connector.ConnectionConfig) (*connector.Device, error)
type Services struct {
	policy  config.Snapshot
	secret  Secret
	rules   *netdisco.Rules
	factory DeviceFactory
	scope   *do.RootScope
}

func New(policy config.Snapshot, secret Secret) (*Services, error) {
	if secret == nil {
		secret = policy.Secret
	}
	scope := do.New()
	do.ProvideValue(scope, policy)
	do.ProvideValue(scope, secret)
	do.ProvideValue[netdisco.ProfileLookup](scope, vendors.Profile)
	do.Provide(scope, func(i do.Injector) (*netdisco.Rules, error) {
		p, e := do.Invoke[config.Snapshot](i)
		if e != nil {
			return nil, e
		}
		lookup, e := do.Invoke[netdisco.ProfileLookup](i)
		if e != nil {
			return nil, e
		}
		o, e := ruleOptions(p)
		if e != nil {
			return nil, e
		}
		return netdisco.NewRules(o, lookup)
	})
	rules, e := do.Invoke[*netdisco.Rules](scope)
	if e != nil {
		return nil, e
	}
	s := &Services{policy: policy, secret: secret, rules: rules, scope: scope, factory: func(p connector.Profile, c connector.ConnectionConfig) (*connector.Device, error) {
		return connector.New(p, c)
	}}
	for _, vendor := range vendors.Names() {
		p, _ := vendors.Profile(vendor)
		c, e := s.connection(vendor)
		if e != nil {
			return nil, e
		}
		c.Host = "192.0.2.1"
		if _, e = connector.New(p, c); e != nil {
			return nil, e
		}
	}
	workers := number(policy, "backup.parallel", 4)
	if workers < 1 || workers > 500 {
		return nil, config.ErrInvalid
	}
	limit := number(policy, "backup.limit_per_vendor", 0)
	if limit < 0 || limit > 5 {
		return nil, config.ErrInvalid
	}
	if mode := str(policy, "netdisco.cache_mode", "prefer"); mode != "prefer" && mode != "only" && mode != "refresh" {
		return nil, config.ErrInvalid
	}
	if e = s.validatePolicy(); e != nil {
		return nil, e
	}
	return s, nil
}
func (s *Services) Close() {
	if s != nil && s.scope != nil {
		s.scope.Shutdown()
	}
}
func (s *Services) Policy() config.Snapshot { return s.policy }
func (s *Services) Rules() *netdisco.Rules  { return s.rules }
func (s *Services) WithDeviceFactory(factory DeviceFactory) *Services {
	copy := *s
	copy.factory = factory
	return &copy
}
func str(p config.Snapshot, k, fallback string) string {
	if v, ok := p.Value(k).(string); ok {
		return v
	}
	return fallback
}
func number(p config.Snapshot, k string, fallback int) int {
	switch v := p.Value(k).(type) {
	case int:
		return v
	case float64:
		return int(v)
	}
	return fallback
}
func boolean(p config.Snapshot, k string, fallback bool) bool {
	if v, ok := p.Value(k).(bool); ok {
		return v
	}
	return fallback
}
func duration(p config.Snapshot, k string, fallback time.Duration) time.Duration {
	switch v := p.Value(k).(type) {
	case int:
		return time.Duration(v) * time.Second
	case float64:
		return time.Duration(v * float64(time.Second))
	}
	return fallback
}
func list(p config.Snapshot, k string) []string {
	out := []string{}
	if a, ok := p.Value(k).([]any); ok {
		for _, v := range a {
			if s, ok := v.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}
func mapping(p config.Snapshot, k string) (map[string]string, error) {
	out := map[string]string{}
	if value := p.Value(k); value != nil {
		m, ok := value.(map[string]any)
		if !ok {
			return nil, config.ErrInvalid
		}
		for k, v := range m {
			s, ok := v.(string)
			if !ok {
				return nil, config.ErrInvalid
			}
			out[k] = s
		}
	}
	return out, nil
}
func ruleOptions(p config.Snapshot) (netdisco.RuleOptions, error) {
	o := netdisco.RuleOptions{Selection: inventory.SelectionOptions{IncludeHosts: list(p, "inventory.include_hosts"), ExcludeHosts: list(p, "inventory.exclude_hosts"), IncludeNames: list(p, "inventory.include_names"), ExcludeNames: list(p, "inventory.exclude_names"), IncludeVendors: list(p, "inventory.include_vendors"), ExcludeVendors: list(p, "inventory.exclude_vendors"), IncludeModels: list(p, "inventory.include_models"), ExcludeModels: list(p, "inventory.exclude_models"), NameMatch: str(p, "inventory.name_match", "contains"), ModelMatch: str(p, "inventory.model_match", "contains")}}
	var e error
	o.VendorOverrides, e = mapping(p, "inventory.vendor_overrides")
	if e != nil {
		return o, e
	}
	o.HostOverrides, e = mapping(p, "inventory.host_overrides")
	if e != nil {
		return o, e
	}
	if a, ok := p.Value("inventory.device_rules").([]any); ok {
		for _, value := range a {
			m, ok := value.(map[string]any)
			if !ok {
				return o, config.ErrInvalid
			}
			rule := netdisco.Mapping{}
			for k, v := range m {
				if v == nil && (k == "os" || k == "model_prefix") {
					continue
				}
				s, ok := v.(string)
				if !ok {
					return o, config.ErrInvalid
				}
				switch k {
				case "vendor":
					rule.Vendor = s
				case "connector":
					rule.Connector = s
				case "os":
					rule.OS = &s
				case "model_prefix":
					rule.ModelPrefix = &s
				default:
					return o, config.ErrInvalid
				}
			}
			o.Mappings = append(o.Mappings, rule)
		}
	}
	return o, nil
}
func (s *Services) connection(vendor string) (connector.ConnectionConfig, error) {
	p := s.policy
	c := connector.ConnectionConfig{LogLevel: str(p, "ssh.log_level", "info"), Protocol: connector.Protocol(str(p, "ssh.protocol", "ssh")), KnownHosts: str(p, "ssh.known_hosts", ""), HostKeyPolicy: connector.HostKeyPolicy(str(p, "ssh.host_key_policy", "strict")), MaxScriptOutputBytes: int64(number(p, "ssh.max_script_output_bytes", 0))}
	protocols, e := mapping(p, "ssh.vendor_protocols")
	if e != nil {
		return c, e
	}
	for name, v := range protocols {
		if _, e := vendors.Profile(name); e != nil || v != "ssh" && v != "telnet" {
			return c, config.ErrInvalid
		}
	}
	if v := protocols[vendor]; v != "" {
		c.Protocol = connector.Protocol(v)
	}
	return c, nil
}
func (s *Services) Device(ctx context.Context, r netdisco.Record) (*connector.Device, error) {
	return s.DeviceWithLogger(ctx, r, nil)
}
func (s *Services) DeviceWithLogger(ctx context.Context, r netdisco.Record, logger io.Writer) (*connector.Device, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	p, e := vendors.Profile(r.Vendor)
	if e != nil {
		return nil, e
	}
	c, e := s.connection(r.Vendor)
	if e != nil {
		return nil, e
	}
	c.Host = r.Host
	c.Logger = logger
	prefix := "NC_" + strings.ToUpper(r.Vendor) + "_"
	get := func(suffix, fallback string) string {
		if v := s.secret(prefix + suffix); strings.TrimSpace(v) != "" {
			return v
		}
		return s.secret(fallback)
	}
	c.Username = get("USERNAME", "NC_DEVICE_USERNAME")
	if strings.TrimSpace(c.Username) == "" {
		return nil, ErrCredentials
	}
	c.Password = get("PASSWORD", "NC_DEVICE_PASSWORD")
	c.EnablePassword = get("ENABLE_PASSWORD", "NC_ENABLE_PASSWORD")
	return s.factory(p, c)
}
func (s *Services) limits() netdisco.Limits {
	p := s.policy
	return netdisco.Limits{PageSize: number(p, "netdisco.page_size", 500), MaxPages: number(p, "netdisco.max_pages", 10000), MaxDevices: number(p, "netdisco.max_devices", 100000), MaxResponseBytes: int64(number(p, "netdisco.max_response_bytes", 16<<20)), MaxInventoryBytes: int64(number(p, "netdisco.max_inventory_bytes", 128<<20)), Timeout: duration(p, "netdisco.inventory_timeout", 300*time.Second)}
}
func (s *Services) Inventory(ctx context.Context, mode string) (netdisco.CacheResult, error) {
	p := s.policy
	if mode == "" {
		mode = str(p, "netdisco.cache_mode", "prefer")
	}
	limits := s.limits()
	var source netdisco.Source
	var identity []byte
	var e error
	switch str(p, "netdisco.source", "http") {
	case "http":
		raw := str(p, "netdisco.url", "")
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") || (!boolean(p, "netdisco.allow_insecure_http", false) && u.Scheme != "https") {
			return netdisco.CacheResult{}, config.ErrInvalid
		}
		u.Scheme = strings.ToLower(u.Scheme)
		u.Host = strings.ToLower(u.Host)
		user, password, key := s.secret("ND_USERNAME"), "", s.secret("ND_API_KEY")
		auth := map[string]string{}
		if key != "" {
			auth["api_key"] = key
		} else {
			if user == "" {
				return netdisco.CacheResult{}, ErrCredentials
			}
			auth["username"] = user
		}
		identity, _ = json.Marshal([]any{"http", u.String(), auth})
		if mode != "only" {
			password = s.secret("ND_PASSWORD")
			transport := http.DefaultTransport.(*http.Transport).Clone()
			defer transport.CloseIdleConnections()
			source, e = netdisco.NewHTTP(netdisco.HTTPOptions{Client: &http.Client{Transport: transport}, URL: u.String(), Username: user, Password: password, APIKey: key, RequireHTTPS: !boolean(p, "netdisco.allow_insecure_http", false), Limits: limits})
		}
	case "postgres":
		keys := []struct{ name, env string }{{"host", "ND_DB_HOST"}, {"dbname", "ND_DB_NAME"}, {"user", "ND_DB_USER"}}
		values := map[string]any{}
		dsn := []string{}
		for _, k := range keys {
			v := s.secret(k.env)
			if v == "" || strings.ContainsRune(v, 0) {
				return netdisco.CacheResult{}, ErrCredentials
			}
			values[k.name] = v
			dsn = append(dsn, k.name+"="+pgQuote(v))
		}
		for _, k := range []string{"port", "sslmode", "sslrootcert", "connect_timeout"} {
			v := p.Value("netdisco.db_" + k)
			if v != nil {
				value := fmt.Sprint(v)
				values[k] = v
				dsn = append(dsn, k+"="+pgQuote(value))
			}
		}
		query := str(p, "netdisco.query", "")
		params, _ := p.Value("netdisco.query_params").([]any)
		if params == nil {
			params = []any{}
		}
		ordered, e := orderedConnectionIdentity(values)
		if e != nil {
			return netdisco.CacheResult{}, e
		}
		identity, _ = json.Marshal([]any{"postgres", ordered, map[string]any{"query": query, "query_params": params}})
		if mode != "only" {
			password := s.secret("ND_DB_PASS")
			if password == "" {
				return netdisco.CacheResult{}, ErrCredentials
			}
			dsn = append(dsn, "password="+pgQuote(password))
			source, e = netdisco.NewPostgres(netdisco.PostgresOptions{Connection: strings.Join(dsn, " "), Query: query, Parameters: params, Limits: limits})
			if e != nil {
				return netdisco.CacheResult{}, e
			}
		}
	default:
		return netdisco.CacheResult{}, config.ErrInvalid
	}
	if e != nil {
		return netdisco.CacheResult{}, e
	}
	if !boolean(p, "netdisco.cache_enabled", true) {
		if mode == "only" {
			return netdisco.CacheResult{}, config.ErrInvalid
		}
		rows, e := source.Devices(ctx)
		return netdisco.CacheResult{Rows: rows, Status: "disabled"}, e
	}
	ttl := duration(p, "netdisco.cache_ttl", 72*time.Hour)
	cache, e := netdisco.NewCache(source, netdisco.CacheOptions{Directory: str(p, "netdisco.cache_directory", ""), Identity: netdisco.Identity(identity), TTL: &ttl, Limits: limits})
	if e != nil {
		return netdisco.CacheResult{}, e
	}
	return cache.Fetch(ctx, mode)
}
func pgQuote(s string) string {
	return "'" + strings.ReplaceAll(strings.ReplaceAll(s, "\\", "\\\\"), "'", "\\'") + "'"
}
func (s *Services) Workers() int      { return number(s.policy, "backup.parallel", 4) }
func (s *Services) Limit() int        { return number(s.policy, "backup.limit_per_vendor", 0) }
func (s *Services) Directory() string { return str(s.policy, "backup.directory", "") }
func (s *Services) String() string    { return "bootstrap.Services" }
func (s *Services) GoString() string  { return s.String() }

// LogDirectory resolves the frozen command-mode policy once at the composition
// root. Root backup/TFTP and ordinary batch backup have no implicit session log;
// batch TFTP and topology fall back to their batch directory's logs subdirectory.
func (s *Services) LogDirectory(program, mode, directory string) string {
	if explicit := str(s.policy, "ssh.log_directory", ""); explicit != "" {
		return explicit
	}
	if program == "net-topology" || program != "net-backup" && mode == "tftp" {
		return filepath.Join(directory, "logs")
	}
	return ""
}
