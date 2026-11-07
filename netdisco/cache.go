package netdisco

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gatework/go-net-connector/inventory"
	"github.com/gatework/go-net-connector/storage"

	"golang.org/x/sys/unix"
)

type CacheOptions struct {
	Directory, Identity string
	TTL                 *time.Duration
	LockTimeout         time.Duration
	Limits              Limits
	Now                 func() time.Time
}

const (
	CacheStatusRefreshed = "refreshed"
	CacheStatusFresh     = "fresh_cache"
	CacheStatusStale     = "stale_cache"
	CacheStatusDisabled  = "disabled"
)

type Cache struct {
	source                              Source
	directory, identity, path, lockPath string
	ttl, lockTimeout                    time.Duration
	limits                              Limits
	now                                 func() time.Time
}

// CacheResult contains complete rows. Status records whether data was
// refreshed, read fresh/stale from cache, or fetched with caching disabled.
// Cache-only Fetch reports freshness; source failures use Go errors unless a
// validated stale entry is available.
type CacheResult struct {
	Rows                                     []inventory.Device
	Status, RefreshErrorCode, WriteErrorType string
	FetchedAt                                time.Time
}
type cacheEntry struct {
	Schema    int                `json:"schema"`
	Identity  string             `json:"identity"`
	FetchedAt float64            `json:"fetched_at"`
	Rows      []inventory.Device `json:"rows"`
}

func NewCache(source Source, o CacheOptions) (*Cache, error) {
	if o.Directory == "" || o.Identity == "" || strings.ContainsRune(o.Directory, 0) {
		return nil, newInventoryError("invalid_cache", nil)
	}
	limits, e := o.Limits.normalize()
	if e != nil {
		return nil, e
	}
	ttl := 72 * time.Hour
	if o.TTL != nil {
		ttl = *o.TTL
	}
	if ttl < 0 || o.LockTimeout < 0 {
		return nil, newInventoryError("invalid_cache", nil)
	}
	lockTimeout := o.LockTimeout
	if lockTimeout == 0 {
		lockTimeout = 5 * time.Second
	}
	now := o.Now
	if now == nil {
		now = time.Now
	}
	directory, e := filepath.Abs(o.Directory)
	if e != nil {
		return nil, e
	}
	sum := sha256.Sum256([]byte(o.Identity))
	identity := hex.EncodeToString(sum[:])
	return &Cache{source, directory, identity, filepath.Join(directory, identity+".json"), filepath.Join(directory, identity+".lock"), ttl, lockTimeout, limits, now}, nil
}
func (c *Cache) Path() string { return c.path }
func (c *Cache) Devices(ctx context.Context) ([]inventory.Device, error) {
	mode := "prefer"
	if c.source == nil {
		mode = "only"
	}
	r, e := c.Fetch(ctx, mode)
	return r.Rows, e
}
func (c *Cache) Fetch(ctx context.Context, mode string) (result CacheResult, err error) {
	if mode != "prefer" && mode != "refresh" && mode != "only" {
		return result, newInventoryError("invalid_cache_mode", nil)
	}
	if c.source == nil && mode != "only" {
		return result, newInventoryError("cache_unavailable", nil)
	}
	if e := ctx.Err(); e != nil {
		return result, e
	}
	if mode != "only" {
		if e := os.MkdirAll(c.directory, 0700); e != nil {
			return result, e
		}
	}
	directory, e := os.Lstat(c.directory)
	if e != nil || !isPrivateCacheEntry(directory, true) {
		return result, newInventoryError("cache_unavailable", nil)
	}
	defer func() {
		after, e := os.Lstat(c.directory)
		if err == nil && (e != nil || !os.SameFile(directory, after)) {
			err = newInventoryError("cache_directory_changed", nil)
		}
	}()
	previous, e := c.read(ctx)
	if e != nil {
		return result, e
	}
	fresh := func(entry *cacheEntry) bool {
		return entry != nil && c.now().Sub(time.Unix(0, int64(entry.FetchedAt*1e9))) < c.ttl
	}
	deliver := func(entry *cacheEntry, status string) CacheResult {
		return CacheResult{Rows: append([]inventory.Device{}, entry.Rows...), Status: status, FetchedAt: time.Unix(0, int64(entry.FetchedAt*1e9)).UTC()}
	}
	if mode == "only" {
		if previous == nil {
			return result, newInventoryError("cache_unavailable", nil)
		}
		status := CacheStatusStale
		if fresh(previous) {
			status = CacheStatusFresh
		}
		return deliver(previous, status), nil
	}
	if mode == "prefer" && fresh(previous) {
		return deliver(previous, CacheStatusFresh), nil
	}
	lock, e := storage.AcquireLockFile(ctx, c.lockPath, c.lockTimeout)
	if e != nil {
		return result, e
	}
	defer func() {
		if e := lock.Close(); err == nil && e != nil {
			err = e
		}
	}()
	previous, e = c.read(ctx)
	if e != nil {
		return result, e
	}
	if mode == "prefer" && fresh(previous) {
		return deliver(previous, CacheStatusFresh), nil
	}
	rows, e := c.source.Devices(ctx)
	if e == nil {
		e = c.validateRows(rows)
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if e != nil {
		if previous == nil {
			return result, e
		}
		result = deliver(previous, CacheStatusStale)
		result.RefreshErrorCode = errorCode(e)
		return result, nil
	}
	entry := cacheEntry{Schema: 1, Identity: c.identity, FetchedAt: float64(c.now().UnixNano()) / 1e9, Rows: append([]inventory.Device{}, rows...)}
	result = deliver(&entry, CacheStatusRefreshed)
	body, e := json.Marshal(entry)
	if e == nil && int64(len(body)) > c.limits.MaxInventoryBytes {
		e = newInventoryError("max_inventory_bytes", nil)
	}
	if e == nil {
		_, e = storage.Write(ctx, c.path, body)
	}
	if e != nil {
		result.WriteErrorType = "IOError"
	}
	return result, nil
}
func isPrivateCacheEntry(info os.FileInfo, directory bool) bool {
	if info == nil {
		return false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != uint32(os.Geteuid()) {
		return false
	}
	if directory {
		return info.IsDir() && info.Mode().Perm() == 0700
	}
	return info.Mode().IsRegular() && info.Mode().Perm() == 0600 && st.Nlink == 1
}
func (c *Cache) read(ctx context.Context) (*cacheEntry, error) {
	body, fp, e := storage.Read(ctx, c.path, c.limits.MaxInventoryBytes)
	if errors.Is(e, os.ErrNotExist) {
		return nil, nil
	}
	if errors.Is(e, storage.ErrLimit) {
		// Reopen without following links: only a private regular entry is a cache miss.
		fd, openErr := unix.Open(c.path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
		if openErr != nil {
			return nil, newInventoryError("cache_unavailable", openErr)
		}
		file := os.NewFile(uintptr(fd), c.path)
		info, statErr := file.Stat()
		closeErr := file.Close()
		if statErr != nil || closeErr != nil || !isPrivateCacheEntry(info, false) || !storage.SameEntry(c.path, storage.Fingerprint{Info: info}) {
			return nil, newInventoryError("cache_unavailable", nil)
		}
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		return nil, nil
	}
	if e != nil {
		return nil, newInventoryError("cache_unavailable", e)
	}
	if !isPrivateCacheEntry(fp.Info, false) || !storage.SameEntry(c.path, fp) {
		return nil, newInventoryError("cache_unavailable", nil)
	}
	var entry cacheEntry
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&entry) != nil {
		return nil, nil
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, nil
	}
	now := float64(c.now().UnixNano()) / 1e9
	if entry.Schema != 1 || entry.Identity != c.identity || math.IsNaN(entry.FetchedAt) || math.IsInf(entry.FetchedAt, 0) || entry.FetchedAt < 0 || entry.FetchedAt > now || entry.Rows == nil {
		return nil, nil
	}
	if c.validateRows(entry.Rows) != nil {
		return nil, nil
	}
	return &entry, nil
}
func (c *Cache) validateRows(rows []inventory.Device) error {
	if rows == nil || len(rows) > c.limits.MaxDevices {
		return newInventoryError("invalid_inventory", nil)
	}
	var size int64
	for _, r := range rows {
		if strings.TrimSpace(r.IP) == "" {
			return newInventoryError("invalid_inventory", nil)
		}
		size += int64(len(r.IP) + len(r.Name) + len(r.DNS) + len(r.Vendor) + len(r.OS) + len(r.Model) + len(r.OSVersion) + len(r.Serial) + 37)
		if size > c.limits.MaxInventoryBytes {
			return newInventoryError("max_inventory_bytes", nil)
		}
	}
	return nil
}

// Identity hashes Ruby-compatible ordered JSON; NewCache applies the second hash.
func Identity(orderedJSON []byte) string {
	sum := sha256.Sum256(orderedJSON)
	return hex.EncodeToString(sum[:])
}
