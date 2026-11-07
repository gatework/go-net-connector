// Package vendors exposes the immutable catalog of supported network device
// dialects. Platform behavior lives in family-scoped internal packages.
package vendors

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/vendors/centec"
	"github.com/gatework/go-net-connector/vendors/cisco"
	"github.com/gatework/go-net-connector/vendors/dialect"
	"github.com/gatework/go-net-connector/vendors/fortinet"
	"github.com/gatework/go-net-connector/vendors/h3c"
	"github.com/gatework/go-net-connector/vendors/hillstone"
	"github.com/gatework/go-net-connector/vendors/huawei"
	"github.com/gatework/go-net-connector/vendors/juniper"
	"github.com/gatework/go-net-connector/vendors/paloalto"
	"github.com/gatework/go-net-connector/vendors/radware"
	"github.com/gatework/go-net-connector/vendors/topsec"
)

// Registry is an immutable catalog of supported CLI dialects.
type Registry struct {
	entries        map[string]dialect.Platform
	inventoryOrder []dialect.Platform
}

// inventoryMatcher belongs to the registry that consumes platform matchers.
type inventoryMatcher interface {
	MatchInventory(dialect.InventoryIdentity) bool
}

// NewBuiltinRegistry returns the process-wide immutable catalog. Each
// dialect returns a detached connector profile for runtime use.
func NewBuiltinRegistry() *Registry { return builtinRegistry() }

var builtinRegistry = sync.OnceValue(func() *Registry {
	// This order preserves the former fleet first-match behavior for
	// identities that satisfy more than one vendor rule. The lookup map is
	// derived from the same declarations so canonical keys cannot drift.
	ordered := []dialect.Platform{
		h3c.Wireless(), h3c.New(), cisco.NXOS(), cisco.IOS(), radware.New(),
		paloalto.New(), huawei.New(), hillstone.New(), centec.New(), topsec.New(),
		fortinet.New(), juniper.New(),
	}
	entries := make(map[string]dialect.Platform, len(ordered))
	for _, platform := range ordered {
		name := platform.Name()
		if name == "" {
			panic("vendors: platform has an empty name")
		}
		if _, exists := entries[name]; exists {
			panic("vendors: duplicate platform name: " + name)
		}
		entries[name] = platform
	}
	return &Registry{entries: entries, inventoryOrder: ordered}
})

// Names returns a sorted, detached list of canonical platform keys.
func (r *Registry) Names() []string {
	if r == nil {
		return nil
	}
	names := make([]string, 0, len(r.entries))
	for name := range r.entries {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Lookup returns the immutable dialect object registered for name.
func (r *Registry) Lookup(name string) (dialect.Platform, error) {
	if r != nil {
		if d, ok := r.entries[name]; ok {
			return d, nil
		}
	}
	return nil, fmt.Errorf("vendors: unknown vendor: %w", connector.ErrUnsupported)
}

// ResolvePlatform applies built-in vendor, OS and model recognition. User
// overrides are applied by fleet before this fallback is consulted.
func (r *Registry) ResolvePlatform(identity dialect.InventoryIdentity) (string, bool) {
	if r == nil {
		return "", false
	}
	identity = normalizeInventoryIdentity(identity)
	for _, platform := range r.inventoryOrder {
		matcher, ok := platform.(inventoryMatcher)
		if ok && matcher.MatchInventory(identity) {
			return platform.Name(), true
		}
	}
	return "", false
}

var inventoryIdentitySeparators = regexp.MustCompile(`[^a-z0-9]+`)

func normalizeInventoryIdentity(identity dialect.InventoryIdentity) dialect.InventoryIdentity {
	identity.Vendor = normalizeInventoryText(identity.Vendor)
	identity.OS = normalizeInventoryText(identity.OS)
	identity.Model = normalizeInventoryText(identity.Model)
	return identity
}

func normalizeInventoryText(value string) string {
	return inventoryIdentitySeparators.ReplaceAllString(strings.ToLower(strings.TrimSpace(value)), " ")
}
