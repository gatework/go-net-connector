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

	"github.com/gatework/go-net-connector/internal/batchreport"

	"github.com/gatework/go-net-connector/archive"
	"github.com/gatework/go-net-connector/config"
	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/fleet"
	"github.com/gatework/go-net-connector/inventory"
	"github.com/gatework/go-net-connector/netdisco"
	"github.com/gatework/go-net-connector/transfer"
	"github.com/gatework/go-net-connector/vendors"
)

// ErrCredentials reports missing device or inventory credentials.
var ErrCredentials = fleet.ErrMissingCredentials

// Secret looks up a secret by environment variable name.
type Secret func(string) string

// DeviceFactory constructs an unconnected device for a platform driver.
type DeviceFactory func(connector.Driver, connector.ConnectionConfig) (*connector.Device, error)

// Services is the composition root: validated settings, rules, the vendor
// registry and device construction.
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

// New validates policy and builds the services. A nil secret reads from
// policy.
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
	s := &Services{policy: policy, secret: secret, rules: rules, registry: registry, factory: func(d connector.Driver, c connector.ConnectionConfig) (*connector.Device, error) {
		return connector.New(d, c)
	}, templates: make(map[string]connector.ConnectionConfig)}
	for _, vendor := range registry.Names() {
		d, _ := registry.Lookup(vendor)
		p := d.Terminal()
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
	if !netdisco.CacheMode(stringValue(policy, "netdisco.cache_mode")).Valid() {
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
	if !batchreport.Layout(settings.backupLayout).Valid() || settings.parallel < 1 || settings.parallel > 500 || settings.limitPerVendor < 0 || settings.limitPerVendor > 5 {
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
	clone := *s
	clone.observer = observer
	return &clone
}

// Policy returns the configuration snapshot.
func (s *Services) Policy() config.Snapshot { return s.policy }

// Rules returns the inventory rules.
func (s *Services) Rules() *fleet.Rules { return s.rules }

// Registry returns the vendor registry.
func (s *Services) Registry() *vendors.Registry { return s.registry }

// WithDeviceFactory returns a copy that builds devices with factory.
func (s *Services) WithDeviceFactory(factory DeviceFactory) *Services {
	clone := *s
	clone.factory = factory
	return &clone
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

// DeviceWithLogger builds an unconnected device for r with its credentials,
// logging to logger.
func (s *Services) DeviceWithLogger(ctx context.Context, r fleet.Record, logger io.Writer) (*connector.Device, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	d, e := s.registry.Lookup(r.Vendor)
	if e != nil {
		return nil, e
	}
	c, ok := s.templates[r.Vendor]
	if !ok {
		return nil, config.ErrInvalid
	}
	c.Host = r.Host
	c.Logger = logger
	c.OnSessionEvent = s.observer
	keys := credentialKeys(r.Vendor)
	// A blank specific secret falls through; the shared fallback is returned
	// as stored, so a deliberately blank-looking password is preserved.
	get := func(names []string) string {
		last := len(names) - 1
		for _, name := range names[:last] {
			if v := s.secret(name); strings.TrimSpace(v) != "" {
				return v
			}
		}
		return s.secret(names[last])
	}
	c.Username = get(keys.username)
	if strings.TrimSpace(c.Username) == "" {
		return nil, ErrCredentials
	}
	c.Password = get(keys.password)
	c.EnablePassword = get(keys.enablePassword)
	return s.factory(d, c)
}

// Inventory fetches the device inventory from the configured Netdisco
// source through the cache policy; an empty mode uses the configured one.
func (s *Services) Inventory(ctx context.Context, mode netdisco.CacheMode) (netdisco.CacheResult, error) {
	p := s.policy
	if mode == "" {
		mode = netdisco.CacheMode(stringValue(p, "netdisco.cache_mode"))
	}
	fetch := netdisco.FetchOptions{Mode: mode}
	if boolValue(p, "netdisco.cache_enabled") {
		ttl := durationValue(p, "netdisco.cache_ttl")
		fetch.Cache = &netdisco.CachePolicy{Directory: stringValue(p, "netdisco.cache_directory"), Namespace: stringValue(p, "netdisco.cache_namespace"), TTL: &ttl}
	} else if mode == netdisco.CacheOnly {
		return netdisco.CacheResult{}, config.ErrInvalid
	}
	switch stringValue(p, "netdisco.source") {
	case "http":
		options, err := s.httpOptions(mode, fetch)
		if err != nil {
			return netdisco.CacheResult{}, err
		}
		return netdisco.FetchHTTP(ctx, options, fetch)
	case "postgres":
		options, err := s.postgresOptions(mode)
		if err != nil {
			return netdisco.CacheResult{}, err
		}
		return netdisco.FetchPostgres(ctx, options, fetch)
	default:
		return netdisco.CacheResult{}, config.ErrInvalid
	}
}

// Workers returns the configured parallelism.
func (s *Services) Workers() int { return s.settings.parallel }

// Limit returns the per-vendor sample limit.
func (s *Services) Limit() int { return s.settings.limitPerVendor }

// Directory returns the backup directory.
func (s *Services) Directory() string { return s.settings.backupDirectory }
func (s *Services) String() string    { return "bootstrap.Services" }
func quoteDSNValue(s string) string {
	return "'" + strings.ReplaceAll(strings.ReplaceAll(s, "\\", "\\\\"), "'", "\\'") + "'"
}

// GoString returns the same text as String.
func (s *Services) GoString() string { return s.String() }

// LogDirectory resolves the frozen command-mode policy once at the composition
// root. Root backup/TFTP and ordinary batch backup have no implicit session log;
// batch TFTP and topology fall back to their batch directory's logs subdirectory.
func (s *Services) LogDirectory(mode, directory string) string {
	if explicit := s.settings.logDirectory; explicit != "" {
		return explicit
	}
	if mode == "topology" || mode == "tftp" || mode == "deploy" || mode == "exec" {
		return filepath.Join(directory, "logs")
	}
	return ""
}

// httpOptions builds the Netdisco HTTP source options. A cache-only fetch
// needs no password.
func (s *Services) httpOptions(mode netdisco.CacheMode, fetch netdisco.FetchOptions) (netdisco.HTTPOptions, error) {
	p := s.policy
	insecure := boolValue(p, "netdisco.allow_insecure_http")
	u, err := url.Parse(stringValue(p, "netdisco.url"))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") || (!insecure && u.Scheme != "https") {
		return netdisco.HTTPOptions{}, config.ErrInvalid
	}
	user, key := s.secret("ND_USERNAME"), s.secret("ND_API_KEY")
	if key != "" && fetch.Cache != nil && strings.TrimSpace(user) == "" && strings.TrimSpace(fetch.Cache.Namespace) == "" {
		return netdisco.HTTPOptions{}, &netdisco.InventoryError{Code: "cache_namespace_required"}
	}
	if key == "" && user == "" {
		return netdisco.HTTPOptions{}, ErrCredentials
	}
	options := netdisco.HTTPOptions{URL: u.String(), Username: user, APIKey: key, RequireHTTPS: !insecure, Limits: s.limits()}
	if mode != netdisco.CacheOnly {
		options.Password = s.secret("ND_PASSWORD")
	}
	return options, nil
}

// postgresOptions builds the Netdisco database source options. A
// cache-only fetch needs no password.
func (s *Services) postgresOptions(mode netdisco.CacheMode) (netdisco.PostgresOptions, error) {
	p := s.policy
	dsn := []string{}
	for _, k := range []struct{ name, env string }{{"host", "ND_DB_HOST"}, {"dbname", "ND_DB_NAME"}, {"user", "ND_DB_USER"}} {
		v := s.secret(k.env)
		if v == "" || strings.ContainsRune(v, 0) {
			return netdisco.PostgresOptions{}, ErrCredentials
		}
		dsn = append(dsn, k.name+"="+quoteDSNValue(v))
	}
	for _, k := range []string{"port", "sslmode", "sslrootcert", "connect_timeout"} {
		if v := p.Value("netdisco.db_" + k); v != nil {
			dsn = append(dsn, k+"="+quoteDSNValue(fmt.Sprint(v)))
		}
	}
	params, _ := p.Value("netdisco.query_params").([]any)
	if params == nil {
		params = []any{}
	}
	if mode != netdisco.CacheOnly {
		password := s.secret("ND_DB_PASS")
		if password == "" {
			return netdisco.PostgresOptions{}, ErrCredentials
		}
		dsn = append(dsn, "password="+quoteDSNValue(password))
	}
	return netdisco.PostgresOptions{Connection: strings.Join(dsn, " "), Query: stringValue(p, "netdisco.query"), Parameters: params, Limits: s.limits()}, nil
}
func (s *Services) compileConnection(vendor string) (connector.ConnectionConfig, error) {
	p := s.policy
	c := connector.ConnectionConfig{LogLevel: stringValue(p, "ssh.log_level"), Protocol: connector.Protocol(stringValue(p, "ssh.protocol")), KnownHosts: stringValue(p, "ssh.known_hosts"), JumpHost: stringValue(p, "ssh.jump_host"), ConnectRetries: integerValue(p, "ssh.connect_retries"), HostKeyPolicy: connector.HostKeyPolicy(stringValue(p, "ssh.host_key_policy")), MaxScriptOutputBytes: int64(integerValue(p, "ssh.max_script_output_bytes"))}
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

func (s *Services) limits() netdisco.Limits {
	return s.settings.inventoryLimits
}

// deviceCredentialKeys lists secret names in lookup order: the most specific
// name first, then the shared fallback.
type deviceCredentialKeys struct{ username, password, enablePassword []string }

// credentialKeys is the single place that names device credential secrets:
// NC_<VENDOR>_<KIND> overrides NC_DEVICE_* (NC_ENABLE_PASSWORD for enable).
func credentialKeys(vendor string) deviceCredentialKeys {
	prefix := "NC_" + strings.ToUpper(vendor) + "_"
	return deviceCredentialKeys{
		username:       []string{prefix + "USERNAME", "NC_DEVICE_USERNAME"},
		password:       []string{prefix + "PASSWORD", "NC_DEVICE_PASSWORD"},
		enablePassword: []string{prefix + "ENABLE_PASSWORD", "NC_ENABLE_PASSWORD"},
	}
}
