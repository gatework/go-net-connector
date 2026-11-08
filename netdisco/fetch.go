package netdisco

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
)

// FetchOptions selects the cache mode for one complete inventory collection.
// An empty Mode means prefer. A nil Cache disables caching; only then fails.
type FetchOptions struct {
	Mode  string
	Cache *CachePolicy
}

// CachePolicy controls storage; source identity and limits belong to the source.
// Namespace identifies an API-key account without depending on key rotation.
// Without it, HTTP API keys use Username when present, otherwise a private key
// commitment. That commitment is never exposed separately from the final digest.
type CachePolicy struct {
	Directory, Namespace string
	TTL                  *time.Duration
	LockTimeout          time.Duration
	Now                  func() time.Time
}

// FetchHTTP collects complete rows with their cache and write-failure evidence.
// It closes only its own idle HTTP connections, never an injected Client.
// Cache-only reads need the source principal but do not need its password.
func FetchHTTP(ctx context.Context, source HTTPOptions, options FetchOptions) (CacheResult, error) {
	mode, err := options.mode()
	if err != nil {
		return CacheResult{}, err
	}
	u, limits, err := httpSettings(source, mode != "only")
	if err != nil {
		return CacheResult{}, err
	}
	identity, err := httpIdentity(source, u.String(), limits, options.namespace())
	if err != nil {
		return CacheResult{}, err
	}
	var input Source
	if mode != "only" {
		httpSource, err := NewHTTP(source)
		if err != nil {
			return CacheResult{}, err
		}
		defer httpSource.CloseIdleConnections()
		input = httpSource
	}
	return fetchInventory(ctx, input, identity, limits, mode, options.Cache)
}

// FetchPostgres applies the same cache policy to a bounded PostgreSQL query.
// Its identity uses the effective parsed connection, query and parameters; a
// password change does not change identity. Each online fetch owns its connection.
func FetchPostgres(ctx context.Context, source PostgresOptions, options FetchOptions) (CacheResult, error) {
	mode, err := options.mode()
	if err != nil {
		return CacheResult{}, err
	}
	postgres, err := NewPostgres(source)
	if err != nil {
		return CacheResult{}, err
	}
	identity, err := postgresIdentity(postgres, options.namespace())
	if err != nil {
		return CacheResult{}, err
	}
	return fetchInventory(ctx, postgres, identity, postgres.limits, mode, options.Cache)
}

func (o FetchOptions) mode() (string, error) {
	mode := o.Mode
	if mode == "" {
		mode = "prefer"
	}
	if mode != "prefer" && mode != "refresh" && mode != "only" {
		return "", newInventoryError("invalid_cache_mode", nil)
	}
	if mode == "only" && o.Cache == nil {
		return "", newInventoryError("cache_disabled", nil)
	}
	return mode, nil
}

func (o FetchOptions) namespace() string {
	if o.Cache == nil {
		return ""
	}
	return o.Cache.Namespace
}

func fetchInventory(ctx context.Context, source Source, identity string, limits Limits, mode string, policy *CachePolicy) (CacheResult, error) {
	if err := ctx.Err(); err != nil {
		return CacheResult{}, err
	}
	if policy == nil {
		rows, err := source.Devices(ctx)
		return CacheResult{Rows: rows, Status: CacheStatusDisabled}, err
	}
	cache, err := newCache(source, CacheOptions{
		Directory: policy.Directory, TTL: policy.TTL,
		LockTimeout: policy.LockTimeout, Now: policy.Now, Limits: limits,
	}, identity)
	if err != nil {
		return CacheResult{}, err
	}
	return cache.Fetch(ctx, mode)
}

// The version is part of the canonical identity, not the legacy cache schema.
// No legacy key is consulted: only a new refresh can assert this source identity.
const sourceIdentityVersion = 2

func sourceIdentity(source string, descriptor any) (string, error) {
	body, err := json.Marshal(struct {
		Version int    `json:"version"`
		Source  string `json:"source"`
		Details any    `json:"details"`
	}{sourceIdentityVersion, source, descriptor})
	if err != nil {
		return "", newInventoryError("invalid_identity", nil)
	}
	// Only this final opaque digest reaches CacheOptions or persistent storage.
	return Identity(body), nil
}

func httpIdentity(o HTTPOptions, url string, limits Limits, namespace string) (string, error) {
	authentication, principal, principalKind := "basic", o.Username, "username"
	if o.APIKey != "" {
		authentication = "api_key"
		switch {
		case namespace != "":
			principal, principalKind = namespace, "namespace"
		case o.Username != "":
		default:
			commitment := sha256.Sum256([]byte("netdisco-api-principal\x00" + o.APIKey))
			principal, principalKind = hex.EncodeToString(commitment[:]), "key_commitment"
		}
	}
	return sourceIdentity("http", struct {
		URL, Authentication, Principal, PrincipalKind, Namespace string
		Endpoint, Fields, FallbackQuery                          string
		PageSize                                                 int
		FallbackSeeAllColumns                                    bool
	}{url, authentication, principal, principalKind, namespace, httpInventoryEndpoint, httpInventoryFields, httpFallbackQuery, limits.PageSize, true})
}

type postgresEndpointIdentity struct {
	Host                          string
	Port                          uint16
	TLS, SkipVerify, CustomVerify bool
	ServerName                    string
}

func postgresEndpoint(host string, port uint16, config *tls.Config) postgresEndpointIdentity {
	// Unix socket directory names are case-sensitive; DNS names are not.
	if !strings.HasPrefix(host, "/") {
		host = canonicalHost(host)
	}
	endpoint := postgresEndpointIdentity{Host: host, Port: port}
	if config != nil {
		endpoint.TLS, endpoint.ServerName = true, config.ServerName
		endpoint.SkipVerify = config.InsecureSkipVerify
		endpoint.CustomVerify = config.VerifyPeerCertificate != nil || config.VerifyConnection != nil
	}
	return endpoint
}

func postgresIdentity(source *Postgres, namespace string) (string, error) {
	config := source.connection
	endpoints := []postgresEndpointIdentity{postgresEndpoint(config.Host, config.Port, config.TLSConfig)}
	for _, fallback := range config.Fallbacks {
		endpoints = append(endpoints, postgresEndpoint(fallback.Host, fallback.Port, fallback.TLSConfig))
	}
	return sourceIdentity("postgres", struct {
		Namespace, User, Database, Query string
		Endpoints                        []postgresEndpointIdentity
		Session                          map[string]string
		Parameters                       []any
		PageSize                         int
		ConnectTimeout                   time.Duration
	}{namespace, config.User, config.Database, source.query, endpoints, config.RuntimeParams,
		append([]any{}, source.parameters...), source.limits.PageSize, config.ConnectTimeout})
}
