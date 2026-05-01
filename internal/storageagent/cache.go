package storageagent

import (
	"bytes"
	"log/slog"
	"net/http"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
)

const (
	// Defaults tuned to ~800 MB worst-case (100 × 8 MB) so an out-of-the-box
	// agent stays well under 1 GB. Operators tune via the agent CLI flags.
	defaultCacheEntries = 100
	defaultMaxItemSize  = 8 * 1024 * 1024 // 8 MB

	// Hard bounds on the constructor inputs. Values outside these ranges are
	// clamped (with a warn log) rather than rejected, so a misconfigured
	// agent still boots.
	entriesFloor   = 1
	entriesCeiling = 100_000

	itemSizeFloor   = 1024             // 1 KB
	itemSizeCeiling = 64 * 1024 * 1024 // 64 MB

	// maxTotalCacheBytes is the worst-case memory ceiling
	// (maxEntries * maxItemSize). If a caller's combination would exceed it,
	// maxEntries is reduced and a warn is logged. 32 GB is comfortably above
	// any realistic agent host while still preventing pathological configs.
	maxTotalCacheBytes int64 = 32 * 1024 * 1024 * 1024
)

type cachedObject struct {
	body        []byte
	contentType string
	etag        string
	lastMod     time.Time
}

// MemoryCache is an in-memory LRU cache for small/hot assets. Entries are
// evicted by recency once the count cap is hit; objects larger than
// maxItemSize bypass the cache and are proxied directly. Worst-case memory
// is maxEntries * maxItemSize, clamped at construct time to a safe ceiling.
type MemoryCache struct {
	cache       *lru.Cache[string, *cachedObject]
	maxItemSize int
}

// NewMemoryCache creates a memory cache. maxEntries caps the number of
// cached objects (LRU eviction beyond the cap). maxItemSize caps the size
// of any single cacheable object; larger objects are rejected by Put.
//
// Inputs are clamped to safe bounds:
//   - 0 or negative → default
//   - maxEntries: [1, 100000]
//   - maxItemSize: [1 KB, 64 MB]
//   - maxEntries × maxItemSize ≤ 32 GB (entries reduced if violated)
//
// Out-of-bound values are clamped, not rejected, so a misconfigured agent
// still starts; clamps are logged at warn level.
func NewMemoryCache(maxEntries int, maxItemSize int) *MemoryCache {
	maxEntries = clampCacheEntries(maxEntries)
	maxItemSize = clampMaxItemSize(maxItemSize)

	if int64(maxEntries)*int64(maxItemSize) > maxTotalCacheBytes {
		capped := int(maxTotalCacheBytes / int64(maxItemSize))
		slog.Warn("storageagent: cache entries reduced to honor total-memory ceiling",
			"requested_entries", maxEntries,
			"capped_entries", capped,
			"max_item_size_bytes", maxItemSize,
			"max_total_bytes", maxTotalCacheBytes)
		maxEntries = capped
	}

	cache, _ := lru.NewWithEvict(maxEntries, onCacheEvict)
	return &MemoryCache{cache: cache, maxItemSize: maxItemSize}
}

func clampCacheEntries(n int) int {
	if n <= 0 {
		return defaultCacheEntries
	}
	if n < entriesFloor {
		slog.Warn("storageagent: cache max-entries below floor; clamping",
			"requested", n, "floor", entriesFloor)
		return entriesFloor
	}
	if n > entriesCeiling {
		slog.Warn("storageagent: cache max-entries above ceiling; clamping",
			"requested", n, "ceiling", entriesCeiling)
		return entriesCeiling
	}
	return n
}

func clampMaxItemSize(n int) int {
	if n <= 0 {
		return defaultMaxItemSize
	}
	if n < itemSizeFloor {
		slog.Warn("storageagent: cache max-item-size below floor; clamping",
			"requested_bytes", n, "floor_bytes", itemSizeFloor)
		return itemSizeFloor
	}
	if n > itemSizeCeiling {
		slog.Warn("storageagent: cache max-item-size above ceiling; clamping",
			"requested_bytes", n, "ceiling_bytes", itemSizeCeiling)
		return itemSizeCeiling
	}
	return n
}

// onCacheEvict logs evictions at debug level so cache pressure is
// observable in field diagnostics. Fires on count-cap eviction, explicit
// Remove/RemoveOldest, and Add-replacement.
func onCacheEvict(key string, v *cachedObject) {
	slog.Debug("storageagent: cache eviction", "key", key, "size", len(v.body))
}

// MaxItemSize returns the per-item byte cap; objects larger than this are
// rejected by Put. Callers can use this to skip the buffered-read path
// for objects that would not be cached anyway.
func (mc *MemoryCache) MaxItemSize() int {
	return mc.maxItemSize
}

// Get retrieves a cached object and writes it to the response.
// Returns true if the object was found in cache (cache hit).
// The real request is passed through so http.ServeContent can handle
// conditional requests (If-None-Match, If-Modified-Since) and Range.
func (mc *MemoryCache) Get(w http.ResponseWriter, r *http.Request, key string) bool {
	obj, ok := mc.cache.Get(key)
	if !ok {
		return false
	}
	w.Header().Set("Content-Type", obj.contentType)
	w.Header().Set("ETag", `"`+obj.etag+`"`)
	w.Header().Set("X-Cache", "HIT")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeContent(w, r, "", obj.lastMod, bytes.NewReader(obj.body))
	return true
}

// Put stores an object in the cache if it's small enough.
// Returns false if the object exceeds maxItemSize.
func (mc *MemoryCache) Put(key string, body []byte, contentType string, etag string, lastMod time.Time) bool {
	if len(body) > mc.maxItemSize {
		return false
	}
	mc.cache.Add(key, &cachedObject{
		body:        body,
		contentType: contentType,
		etag:        etag,
		lastMod:     lastMod,
	})
	return true
}

// Invalidate removes a specific key from the cache.
func (mc *MemoryCache) Invalidate(key string) {
	mc.cache.Remove(key)
}

// Stats returns the current number of cached entries.
func (mc *MemoryCache) Stats() int {
	return mc.cache.Len()
}
