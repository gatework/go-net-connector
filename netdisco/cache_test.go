package netdisco

import (
	"bytes"
	"context"
	"errors"
	"github.com/gatework/go-net-connector/inventory"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCacheOfflineAndRefreshFallback(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache")
	calls := 0
	now := time.Unix(1000, 0)
	ttl := time.Hour
	source := SourceFunc(func(context.Context) ([]inventory.Device, error) {
		calls++
		return []inventory.Device{{IP: "192.0.2.1"}}, nil
	})
	c, e := NewCache(source, CacheOptions{Directory: dir, Identity: "identity", TTL: &ttl, Now: func() time.Time { return now }})
	if e != nil {
		t.Fatal(e)
	}
	r, e := c.Fetch(t.Context(), "prefer")
	if e != nil || r.Status != "refreshed" {
		t.Fatal(r, e)
	}
	r, e = c.Fetch(t.Context(), "prefer")
	if e != nil || r.Status != "fresh_cache" || calls != 1 {
		t.Fatal(r, e, calls)
	}
	offline, _ := NewCache(nil, CacheOptions{Directory: dir, Identity: "identity", TTL: &ttl, Now: func() time.Time { return now.Add(2 * time.Hour) }})
	r, e = offline.Fetch(t.Context(), "only")
	if e != nil || r.Status != "stale_cache" || calls != 1 {
		t.Fatal(r, e)
	}
	failed, _ := NewCache(SourceFunc(func(context.Context) ([]inventory.Device, error) { return nil, errors.New("secret error") }), CacheOptions{Directory: dir, Identity: "identity", TTL: &ttl, Now: func() time.Time { return now.Add(2 * time.Hour) }})
	r, e = failed.Fetch(t.Context(), "refresh")
	if e != nil || r.Status != "stale_cache" || r.RefreshErrorCode != "inventory_error" {
		t.Fatal(r, e)
	}
}
func TestCacheWriteFailureKeepsFreshResult(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache")
	var c *Cache
	calls := 0
	source := SourceFunc(func(context.Context) ([]inventory.Device, error) {
		calls++
		if e := os.Mkdir(c.Path(), 0700); e != nil {
			t.Fatal(e)
		}
		return []inventory.Device{{IP: "192.0.2.2"}}, nil
	})
	c, _ = NewCache(source, CacheOptions{Directory: dir, Identity: "failure"})
	r, e := c.Fetch(t.Context(), "refresh")
	if e != nil || len(r.Rows) != 1 || r.WriteErrorType == "" || calls != 1 {
		t.Fatalf("%+v %v %d", r, e, calls)
	}
}

func TestCacheEmptyInventoryRoundTrip(t *testing.T) {
	calls := 0
	opts := CacheOptions{Directory: filepath.Join(t.TempDir(), "cache"), Identity: "empty"}
	c, e := NewCache(SourceFunc(func(context.Context) ([]inventory.Device, error) { calls++; return []inventory.Device{}, nil }), opts)
	if e != nil {
		t.Fatal(e)
	}
	for _, mode := range []string{"refresh", "prefer", "only"} {
		r, e := c.Fetch(t.Context(), mode)
		if e != nil || r.Rows == nil || len(r.Rows) != 0 || r.WriteErrorType != "" || calls != 1 {
			t.Fatalf("%s: %+v %v calls=%d", mode, r, e, calls)
		}
	}
	offline, e := NewCache(nil, opts)
	if e != nil {
		t.Fatal(e)
	}
	r, e := offline.Fetch(t.Context(), "only")
	if e != nil || r.Rows == nil || len(r.Rows) != 0 || r.Status != "fresh_cache" {
		t.Fatalf("offline: %+v %v", r, e)
	}
	body, e := os.ReadFile(c.Path())
	if e != nil || !bytes.Contains(body, []byte(`"rows":[]`)) {
		t.Fatalf("cache does not contain empty array: %v", e)
	}
}

func TestCacheOversizedEntryRecoveryAndSafety(t *testing.T) {
	for _, mode := range []string{"prefer", "refresh", "only"} {
		for _, kind := range []string{"private", "public", "symlink", "hardlink"} {
			t.Run(mode+"/"+kind, func(t *testing.T) {
				dir := filepath.Join(t.TempDir(), "cache")
				if e := os.Mkdir(dir, 0700); e != nil {
					t.Fatal(e)
				}
				calls := 0
				c, e := NewCache(SourceFunc(func(context.Context) ([]inventory.Device, error) { calls++; return []inventory.Device{}, nil }), CacheOptions{Directory: dir, Identity: "large", Limits: Limits{MaxInventoryBytes: 300}})
				if e != nil {
					t.Fatal(e)
				}
				path := c.Path()
				if kind == "symlink" || kind == "hardlink" {
					path = filepath.Join(dir, "target")
				}
				if e = os.WriteFile(path, bytes.Repeat([]byte("x"), 301), 0600); e != nil {
					t.Fatal(e)
				}
				if kind == "public" {
					e = os.Chmod(path, 0644)
				}
				if kind == "symlink" {
					e = os.Symlink(path, c.Path())
				}
				if kind == "hardlink" {
					e = os.Link(path, c.Path())
				}
				if e != nil {
					t.Fatal(e)
				}
				r, e := c.Fetch(t.Context(), mode)
				if kind == "private" && mode != "only" {
					if e != nil || r.Status != "refreshed" || r.WriteErrorType != "" || calls != 1 {
						t.Fatalf("recover: %+v %v calls=%d", r, e, calls)
					}
					if _, e = c.Fetch(t.Context(), "only"); e != nil {
						t.Fatal(e)
					}
				} else if errorCode(e) != "cache_unavailable" || calls != 0 {
					t.Fatalf("reject/offline: %+v %v calls=%d", r, e, calls)
				}
			})
		}
	}
}
