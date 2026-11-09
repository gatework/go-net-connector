package netdisco

// CacheMode selects how inventory reads use the local cache. Its values are
// part of the configuration and CLI contracts.
type CacheMode string

const (
	// CachePrefer uses a fresh cache entry and otherwise recollects.
	CachePrefer CacheMode = "prefer"
	// CacheRefresh always recollects and replaces the cache entry.
	CacheRefresh CacheMode = "refresh"
	// CacheOnly reads the cache without contacting the inventory source.
	CacheOnly CacheMode = "only"
)

// CacheModes returns every accepted cache mode in documentation order.
func CacheModes() []string {
	return []string{string(CachePrefer), string(CacheOnly), string(CacheRefresh)}
}

// Valid reports whether m is a known cache mode.
func (m CacheMode) Valid() bool {
	return m == CachePrefer || m == CacheRefresh || m == CacheOnly
}
