// Package bootstrap is the only dependency injection composition root.
package bootstrap

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/gatework/go-net-connector/archive"
	"github.com/gatework/go-net-connector/config"
	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/fleet"
	"github.com/gatework/go-net-connector/inventory"
	"github.com/gatework/go-net-connector/netdisco"
	"github.com/gatework/go-net-connector/transfer"
	"github.com/gatework/go-net-connector/vendors"
)

var ErrCredentials = fleet.ErrMissingCredentials

type Secret func(string) string
type DeviceFactory func(connector.Profile, connector.ConnectionConfig) (*connector.Device, error)
type Services struct {
	observer   func(context.Context, connector.SessionEvent) error
	policy     config.Snapshot
	secret     Secret
	rules      *fleet.Rules
	factory    DeviceFactory
	templates  map[string]connector.ConnectionConfig
	settings   resolvedSettings
	tftpPolicy *transfer.Policy
	registry   *vendors.Registry
}

// resolvedSettings freezes frequently used scalar settings once at the
// composition root. Policy remains available for provenance and less common
// values, but runtime workflows do not repeatedly unpack these fields.
type resolvedSettings struct {
	parallel        int
	limitPerVendor  int
	backupDirectory string
	backupLayout    string
	logDirectory    string
	archiveTimeouts archive.Timeouts
	inventoryLimits netdisco.Limits
}

func New(policy config.Snapshot, secret Secret) (*Services, error) {
	if secret == nil {
		secret = policy.Secret
	}
	o, e := ruleOptions(policy)
	if e != nil {
		return nil, e
	}
	registry := vendors.NewBuiltinRegistry()
	rules, e := fleet.NewRules(o, registry)
	if e != nil {
		return nil, e
	}
	s := &Services{policy: policy, secret: secret, rules: rules, registry: registry, factory: func(p connector.Profile, c connector.ConnectionConfig) (*connector.Device, error) {
		return connector.New(p, c)
	}, templates: make(map[string]connector.ConnectionConfig)}
	for _, vendor := range registry.Names() {
		d, _ := registry.Lookup(vendor)
		p := d.Profile()
		c, e := s.compileConnection(vendor)
		if e != nil {
			return nil, e
		}
		if e = p.Validate(); e != nil {
			return nil, e
		}
		s.templates[vendor] = c
		if e = c.ValidateOptions(); e != nil {
			return nil, e
		}
	}
	s.settings, e = resolveSettings(policy)
	if e != nil {
		return nil, e
	}
	if mode := stringValue(policy, "netdisco.cache_mode"); mode != "prefer" && mode != "only" && mode != "refresh" {
		return nil, config.ErrInvalid
	}
	if e = s.validatePolicy(); e != nil {
		return nil, e
	}
	s.tftpPolicy, e = s.buildTFTPPolicy()
	if e != nil {
		return nil, e
	}
	return s, nil
}

func resolveSettings(policy config.Snapshot) (resolvedSettings, error) {
	settings := resolvedSettings{
		parallel:        integerValue(policy, "backup.parallel"),
		limitPerVendor:  integerValue(policy, "backup.limit_per_vendor"),
		backupDirectory: stringValue(policy, "backup.directory"),
		backupLayout:    stringValue(policy, "backup.layout"),
		logDirectory:    stringValue(policy, "ssh.log_directory"),
		inventoryLimits: netdisco.Limits{
			PageSize:          integerValue(policy, "netdisco.page_size"),
			MaxPages:          integerValue(policy, "netdisco.max_pages"),
			MaxDevices:        integerValue(policy, "netdisco.max_devices"),
			MaxResponseBytes:  int64(integerValue(policy, "netdisco.max_response_bytes")),
			MaxInventoryBytes: int64(integerValue(policy, "netdisco.max_inventory_bytes")),
			Timeout:           durationValue(policy, "netdisco.inventory_timeout"),
		},
	}
	if settings.backupLayout != "batch" && settings.backupLayout != "flat" || settings.parallel < 1 || settings.parallel > 500 || settings.limitPerVendor < 0 || settings.limitPerVendor > 5 {
		return resolvedSettings{}, config.ErrInvalid
	}
	if policy.Value("tftp.upload_timeout") != nil {
		values := []*time.Duration{&settings.archiveTimeouts.Prepare, &settings.archiveTimeouts.Upload, &settings.archiveTimeouts.Verify, &settings.archiveTimeouts.Cleanup}
		for i, key := range []string{"archive_prepare_timeout", "upload_timeout", "archive_verify_timeout", "archive_cleanup_timeout"} {
			var seconds float64
			switch v := policy.Value("tftp." + key).(type) {
			case int:
				seconds = float64(v)
			case float64:
				seconds = v
			default:
				return resolvedSettings{}, &config.FieldError{Field: "tftp." + key, Problem: "required"}
			}
			if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds <= 0 || seconds >= float64(time.Duration(1<<63-1))/float64(time.Second) || seconds*float64(time.Second) < 1 {
				return resolvedSettings{}, &config.FieldError{Field: "tftp." + key, Problem: "invalid"}
			}
			*values[i] = time.Duration(seconds * float64(time.Second))
		}
		if err := settings.archiveTimeouts.Validate(); err != nil {
			return resolvedSettings{}, err
		}
	}
	return settings, nil
}

// WithSessionObserver binds an operation-local observer without mutating shared templates.
func (s *Services) WithSessionObserver(observer func(context.Context, connector.SessionEvent) error) *Services {
	copy := *s
	copy.observer = observer
	return &copy
}
func (s *Services) Policy() config.Snapshot     { return s.policy }
func (s *Services) Rules() *fleet.Rules         { return s.rules }
func (s *Services) Registry() *vendors.Registry { return s.registry }
func (s *Services) WithDeviceFactory(factory DeviceFactory) *Services {
	copy := *s
	copy.factory = factory
	return &copy
}
func stringValue(snapshot config.Snapshot, path string) string {
	if v, ok := snapshot.Value(path).(string); ok {
		return v
	}
	return ""
}
func integerValue(snapshot config.Snapshot, path string) int {
	switch v := snapshot.Value(path).(type) {
	case int:
		return v
	case float64:
		return int(v)
	}
	return 0
}
func boolValue(snapshot config.Snapshot, path string) bool {
	if v, ok := snapshot.Value(path).(bool); ok {
		return v
	}
	return false
}
func durationValue(snapshot config.Snapshot, path string) time.Duration {
	switch v := snapshot.Value(path).(type) {
	case int:
		return time.Duration(v) * time.Second
	case float64:
		return time.Duration(v * float64(time.Second))
	}
	return 0
}
func stringList(snapshot config.Snapshot, path string) []string {
	out := []string{}
	if a, ok := snapshot.Value(path).([]any); ok {
		for _, v := range a {
			if s, ok := v.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}
func stringMap(snapshot config.Snapshot, path string) (map[string]string, error) {
	out := map[string]string{}
	if value := snapshot.Value(path); value != nil {
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
func ruleOptions(p config.Snapshot) (fleet.RuleOptions, error) {
	o := fleet.RuleOptions{Selection: inventory.SelectionOptions{IncludeHosts: stringList(p, "inventory.include_hosts"), ExcludeHosts: stringList(p, "inventory.exclude_hosts"), IncludeNames: stringList(p, "inventory.include_names"), ExcludeNames: stringList(p, "inventory.exclude_names"), IncludeVendors: stringList(p, "inventory.include_vendors"), ExcludeVendors: stringList(p, "inventory.exclude_vendors"), IncludeModels: stringList(p, "inventory.include_models"), ExcludeModels: stringList(p, "inventory.exclude_models"), NameMatch: stringValue(p, "inventory.name_match"), ModelMatch: stringValue(p, "inventory.model_match")}}
	var e error
	o.VendorOverrides, e = stringMap(p, "inventory.vendor_overrides")
	if e != nil {
		return o, e
	}
	o.HostOverrides, e = stringMap(p, "inventory.host_overrides")
	if e != nil {
		return o, e
	}
	if a, ok := p.Value("inventory.device_rules").([]any); ok {
		for _, value := range a {
			m, ok := value.(map[string]any)
			if !ok {
				return o, config.ErrInvalid
			}
			rule := fleet.Mapping{}
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
func (s *Services) compileConnection(vendor string) (connector.ConnectionConfig, error) {
	p := s.policy
	c := connector.ConnectionConfig{LogLevel: stringValue(p, "ssh.log_level"), Protocol: connector.Protocol(stringValue(p, "ssh.protocol")), KnownHosts: stringValue(p, "ssh.known_hosts"), HostKeyPolicy: connector.HostKeyPolicy(stringValue(p, "ssh.host_key_policy")), MaxScriptOutputBytes: int64(integerValue(p, "ssh.max_script_output_bytes"))}
	protocols, e := stringMap(p, "ssh.vendor_protocols")
	if e != nil {
		return c, e
	}
	for name, v := range protocols {
		if _, e := s.registry.Lookup(name); e != nil || v != "ssh" && v != "telnet" {
			return c, config.ErrInvalid
		}
	}
	if v := protocols[vendor]; v != "" {
		c.Protocol = connector.Protocol(v)
	}
	return c, nil
}
func (s *Services) Device(ctx context.Context, r fleet.Record) (*connector.Device, error) {
	return s.DeviceWithLogger(ctx, r, nil)
}
func (s *Services) DeviceWithLogger(ctx context.Context, r fleet.Record, logger io.Writer) (*connector.Device, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	d, e := s.registry.Lookup(r.Vendor)
	if e != nil {
		return nil, e
	}
	p := d.Profile()
	c, ok := s.templates[r.Vendor]
	if !ok {
		return nil, config.ErrInvalid
	}
	c.Host = r.Host
	c.Logger = logger
	c.OnSessionEvent = s.observer
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
	return s.settings.inventoryLimits
}
func (s *Services) Inventory(ctx context.Context, mode string) (netdisco.CacheResult, error) {
	p := s.policy
	if mode == "" {
		mode = stringValue(p, "netdisco.cache_mode")
	}
	limits := s.limits()
	fetch := netdisco.FetchOptions{Mode: mode}
	if boolValue(p, "netdisco.cache_enabled") {
		ttl := durationValue(p, "netdisco.cache_ttl")
		fetch.Cache = &netdisco.CachePolicy{Directory: stringValue(p, "netdisco.cache_directory"), Namespace: stringValue(p, "netdisco.cache_namespace"), TTL: &ttl}
	} else if mode == "only" {
		return netdisco.CacheResult{}, config.ErrInvalid
	}
	switch stringValue(p, "netdisco.source") {
	case "http":
		raw := stringValue(p, "netdisco.url")
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") || (!boolValue(p, "netdisco.allow_insecure_http") && u.Scheme != "https") {
			return netdisco.CacheResult{}, config.ErrInvalid
		}
		user, key := s.secret("ND_USERNAME"), s.secret("ND_API_KEY")
		if key != "" && fetch.Cache != nil && strings.TrimSpace(user) == "" && strings.TrimSpace(fetch.Cache.Namespace) == "" {
			return netdisco.CacheResult{}, &netdisco.InventoryError{Code: "cache_namespace_required"}
		}
		if key == "" {
			if user == "" {
				return netdisco.CacheResult{}, ErrCredentials
			}
		}
		options := netdisco.HTTPOptions{URL: u.String(), Username: user, APIKey: key, RequireHTTPS: !boolValue(p, "netdisco.allow_insecure_http"), Limits: limits}
		if mode != "only" {
			options.Password = s.secret("ND_PASSWORD")
		}
		return netdisco.FetchHTTP(ctx, options, fetch)
	case "postgres":
		keys := []struct{ name, env string }{{"host", "ND_DB_HOST"}, {"dbname", "ND_DB_NAME"}, {"user", "ND_DB_USER"}}
		dsn := []string{}
		for _, k := range keys {
			v := s.secret(k.env)
			if v == "" || strings.ContainsRune(v, 0) {
				return netdisco.CacheResult{}, ErrCredentials
			}
			dsn = append(dsn, k.name+"="+quoteDSNValue(v))
		}
		for _, k := range []string{"port", "sslmode", "sslrootcert", "connect_timeout"} {
			v := p.Value("netdisco.db_" + k)
			if v != nil {
				value := fmt.Sprint(v)
				dsn = append(dsn, k+"="+quoteDSNValue(value))
			}
		}
		query := stringValue(p, "netdisco.query")
		params, _ := p.Value("netdisco.query_params").([]any)
		if params == nil {
			params = []any{}
		}
		if mode != "only" {
			password := s.secret("ND_DB_PASS")
			if password == "" {
				return netdisco.CacheResult{}, ErrCredentials
			}
			dsn = append(dsn, "password="+quoteDSNValue(password))
		}
		return netdisco.FetchPostgres(ctx, netdisco.PostgresOptions{Connection: strings.Join(dsn, " "), Query: query, Parameters: params, Limits: limits}, fetch)
	default:
		return netdisco.CacheResult{}, config.ErrInvalid
	}
}
func quoteDSNValue(s string) string {
	return "'" + strings.ReplaceAll(strings.ReplaceAll(s, "\\", "\\\\"), "'", "\\'") + "'"
}
func (s *Services) Workers() int      { return s.settings.parallel }
func (s *Services) Limit() int        { return s.settings.limitPerVendor }
func (s *Services) Directory() string { return s.settings.backupDirectory }
func (s *Services) String() string    { return "bootstrap.Services" }
func (s *Services) GoString() string  { return s.String() }

// LogDirectory resolves the frozen command-mode policy once at the composition
// root. Root backup/TFTP and ordinary batch backup have no implicit session log;
// batch TFTP and topology fall back to their batch directory's logs subdirectory.
func (s *Services) LogDirectory(mode, directory string) string {
	if explicit := s.settings.logDirectory; explicit != "" {
		return explicit
	}
	if mode == "topology" || mode == "tftp" {
		return filepath.Join(directory, "logs")
	}
	return ""
}
