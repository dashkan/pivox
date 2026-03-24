package agent

import (
	"bytes"
	"net/http"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
)

const (
	// Maximum size of a single object eligible for memory caching.
	// Objects larger than this are always proxied directly.
	maxCacheableSize = 10 * 1024 * 1024 // 10MB

	// Default maximum number of entries in the memory cache.
	defaultCacheEntries = 1000

	// Default maximum total memory for cached objects.
	defaultCacheMaxBytes = 256 * 1024 * 1024 // 256MB
)

// cachedObject holds a cached HTTP response body with metadata.
type cachedObject struct {
	body        []byte
	contentType string
	etag        string
	lastMod     time.Time
	size        int
}

// MemoryCache is an in-memory LRU cache for small/hot assets.
// Large objects (>maxCacheableSize) bypass the cache and are proxied
// directly. The cache tracks total memory usage and evicts when the
// limit is exceeded.
//
// TODO: Add disk cache tier for larger objects and persistence across
// restarts. Disk cache would sit between memory cache and upstream,
// using the endpoint's CacheConfig (max_size_gb, eviction_policy, ttl).
type MemoryCache struct {
	cache    *lru.Cache[string, *cachedObject]
	mu       sync.Mutex
	curBytes int64
	maxBytes int64
}

// NewMemoryCache creates a memory cache with the given max entries and
// total memory limit in bytes.
func NewMemoryCache(maxEntries int, maxBytes int64) *MemoryCache {
	if maxEntries <= 0 {
		maxEntries = defaultCacheEntries
	}
	if maxBytes <= 0 {
		maxBytes = defaultCacheMaxBytes
	}

	mc := &MemoryCache{maxBytes: maxBytes}

	// onEvict tracks memory released when entries are evicted.
	cache, _ := lru.NewWithEvict[string, *cachedObject](maxEntries, func(_ string, v *cachedObject) {
		mc.mu.Lock()
		mc.curBytes -= int64(v.size)
		mc.mu.Unlock()
	})

	mc.cache = cache
	return mc
}

// Get retrieves a cached object and writes it to the response.
// Returns true if the object was found in cache (cache hit).
func (mc *MemoryCache) Get(w http.ResponseWriter, key string) bool {
	obj, ok := mc.cache.Get(key)
	if !ok {
		return false
	}

	w.Header().Set("Content-Type", obj.contentType)
	w.Header().Set("ETag", obj.etag)
	w.Header().Set("Last-Modified", obj.lastMod.UTC().Format(http.TimeFormat))
	w.Header().Set("X-Cache", "HIT")
	http.ServeContent(w, &http.Request{}, "", obj.lastMod, bytes.NewReader(obj.body))
	return true
}

// Put stores an object in the cache if it's small enough.
// Returns false if the object is too large to cache.
func (mc *MemoryCache) Put(key string, body []byte, contentType string, etag string, lastMod time.Time) bool {
	size := len(body)
	if size > maxCacheableSize {
		return false
	}

	// Check if adding this would exceed memory limit.
	mc.mu.Lock()
	for mc.curBytes+int64(size) > mc.maxBytes && mc.cache.Len() > 0 {
		// Evict oldest to make room. The evict callback updates curBytes.
		mc.cache.RemoveOldest()
	}
	mc.curBytes += int64(size)
	mc.mu.Unlock()

	mc.cache.Add(key, &cachedObject{
		body:        body,
		contentType: contentType,
		etag:        etag,
		lastMod:     lastMod,
		size:        size,
	})
	return true
}

// Invalidate removes a specific key from the cache.
func (mc *MemoryCache) Invalidate(key string) {
	mc.cache.Remove(key)
}

// Stats returns cache statistics.
func (mc *MemoryCache) Stats() (entries int, bytes int64) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	return mc.cache.Len(), mc.curBytes
}
