package storageagent

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMemoryCache_Defaults(t *testing.T) {
	// Zero values should use defaults without panicking.
	mc := NewMemoryCache(0, 0)
	require.NotNil(t, mc)
	assert.Equal(t, int64(defaultCacheMaxBytes), mc.maxBytes)

	// Negative values should also use defaults.
	mc2 := NewMemoryCache(-1, -1)
	require.NotNil(t, mc2)
	assert.Equal(t, int64(defaultCacheMaxBytes), mc2.maxBytes)
}

func TestPutAndGet_Hit(t *testing.T) {
	mc := NewMemoryCache(100, 1024*1024)

	body := []byte("hello world")
	now := time.Now()

	ok := mc.Put("key1", body, "text/plain", "abc123", now)
	require.True(t, ok)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/key1", nil)

	hit := mc.Get(w, r, "key1")
	require.True(t, hit)

	resp := w.Result()
	assert.Equal(t, "HIT", resp.Header.Get("X-Cache"))
	assert.Equal(t, "text/plain", resp.Header.Get("Content-Type"))
	assert.Contains(t, resp.Header.Get("ETag"), "abc123")
	assert.Equal(t, "hello world", w.Body.String())
}

func TestGet_Miss(t *testing.T) {
	mc := NewMemoryCache(100, 1024*1024)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/missing", nil)

	hit := mc.Get(w, r, "nonexistent")
	assert.False(t, hit)
}

func TestPut_TooLarge(t *testing.T) {
	mc := NewMemoryCache(100, 256*1024*1024)

	// Create a body larger than maxCacheableSize (10MB).
	largeBody := make([]byte, maxCacheableSize+1)

	ok := mc.Put("big", largeBody, "application/octet-stream", "etag", time.Now())
	assert.False(t, ok, "objects larger than maxCacheableSize should not be cached")
}

func TestLRUEviction(t *testing.T) {
	const maxEntries = 3
	mc := NewMemoryCache(maxEntries, 1024*1024)

	// Fill the cache with maxEntries items.
	for i := range maxEntries {
		key := string(rune('a'+i)) + "_key"
		mc.Put(key, []byte("data"), "text/plain", "etag", time.Now())
	}

	// Add one more to trigger LRU eviction of the oldest.
	mc.Put("new_key", []byte("data"), "text/plain", "etag", time.Now())

	// The first entry ("a_key") should have been evicted.
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	hit := mc.Get(w, r, "a_key")
	assert.False(t, hit, "oldest entry should have been evicted by LRU")

	// The newest entry should still be present.
	w = httptest.NewRecorder()
	hit = mc.Get(w, r, "new_key")
	assert.True(t, hit, "newest entry should be present")
}

func TestMemoryLimitEviction_NoEvictionNeeded(t *testing.T) {
	mc := NewMemoryCache(100, 100)

	mc.Put("first", []byte("1234567890"), "text/plain", "e1", time.Now())  // 10 bytes
	mc.Put("second", []byte("1234567890"), "text/plain", "e2", time.Now()) // 10 bytes

	entries, bytes := mc.Stats()
	assert.Equal(t, 2, entries)
	assert.Equal(t, int64(20), bytes)
}

func TestMemoryLimitEviction_EvictsOldestWhenFull(t *testing.T) {
	// maxBytes=50, so we can fit 5 x 10-byte entries. Adding a 6th must evict.
	mc := NewMemoryCache(100, 50)
	now := time.Now()

	for i := range 5 {
		key := fmt.Sprintf("key%d", i)
		mc.Put(key, []byte("1234567890"), "text/plain", "e", now) // 10 bytes each
	}

	entries, usedBytes := mc.Stats()
	assert.Equal(t, 5, entries)
	assert.Equal(t, int64(50), usedBytes)

	// Adding one more 10-byte entry should evict the oldest to make room.
	ok := mc.Put("new", []byte("abcdefghij"), "text/plain", "e", now)
	require.True(t, ok)

	entries, usedBytes = mc.Stats()
	assert.Equal(t, 5, entries, "cache should still hold 5 entries after eviction")
	assert.Equal(t, int64(50), usedBytes, "total bytes should remain at max")

	// The oldest entry (key0) should have been evicted.
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	assert.False(t, mc.Get(w, r, "key0"), "oldest entry should have been evicted")

	// The new entry should be present.
	w = httptest.NewRecorder()
	assert.True(t, mc.Get(w, r, "new"), "newly added entry should be present")
}

func TestMemoryLimitEviction_EvictsMultipleEntries(t *testing.T) {
	// maxBytes=30, fill with 3 x 10-byte entries. Then add a 20-byte entry
	// which requires evicting 2 entries.
	mc := NewMemoryCache(100, 30)
	now := time.Now()

	mc.Put("a", []byte("1234567890"), "text/plain", "e", now) // 10 bytes
	mc.Put("b", []byte("1234567890"), "text/plain", "e", now) // 10 bytes
	mc.Put("c", []byte("1234567890"), "text/plain", "e", now) // 10 bytes

	entries, usedBytes := mc.Stats()
	require.Equal(t, 3, entries)
	require.Equal(t, int64(30), usedBytes)

	// Add a 20-byte entry; needs to evict 2 oldest entries (a and b).
	ok := mc.Put("big", []byte("12345678901234567890"), "text/plain", "e", now)
	require.True(t, ok)

	entries, usedBytes = mc.Stats()
	assert.Equal(t, 2, entries, "should have 2 entries after evicting 2")
	assert.Equal(t, int64(30), usedBytes, "total bytes should remain at max")

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)

	assert.False(t, mc.Get(w, r, "a"), "entry 'a' should have been evicted")
	w = httptest.NewRecorder()
	assert.False(t, mc.Get(w, r, "b"), "entry 'b' should have been evicted")
	w = httptest.NewRecorder()
	assert.True(t, mc.Get(w, r, "c"), "entry 'c' should still be present")
	w = httptest.NewRecorder()
	assert.True(t, mc.Get(w, r, "big"), "entry 'big' should be present")
}

func TestInvalidate(t *testing.T) {
	mc := NewMemoryCache(100, 1024*1024)

	mc.Put("key1", []byte("data"), "text/plain", "etag", time.Now())

	mc.Invalidate("key1")

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	hit := mc.Get(w, r, "key1")
	assert.False(t, hit, "invalidated key should be a miss")
}

func TestStats(t *testing.T) {
	mc := NewMemoryCache(100, 1024*1024)

	mc.Put("a", []byte("12345"), "text/plain", "e1", time.Now())
	mc.Put("b", []byte("67890"), "text/plain", "e2", time.Now())

	entries, bytes := mc.Stats()
	assert.Equal(t, 2, entries)
	assert.Equal(t, int64(10), bytes)

	// After invalidation, stats should reflect the change.
	mc.Invalidate("a")
	entries, bytes = mc.Stats()
	assert.Equal(t, 1, entries)
	assert.Equal(t, int64(5), bytes)
}

func TestConditionalRequest(t *testing.T) {
	mc := NewMemoryCache(100, 1024*1024)
	now := time.Now().Truncate(time.Second)

	mc.Put("asset", []byte("body content"), "text/plain", "etag-value", now)

	// First request to get the ETag.
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/asset", nil)
	mc.Get(w, r, "asset")
	etag := w.Result().Header.Get("ETag")
	require.NotEmpty(t, etag)

	// Second request with If-None-Match should yield 304 Not Modified.
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest(http.MethodGet, "/asset", nil)
	r2.Header.Set("If-None-Match", etag)
	hit := mc.Get(w2, r2, "asset")
	assert.True(t, hit, "key should be found in cache")

	resp := w2.Result()
	assert.Equal(t, http.StatusNotModified, resp.StatusCode,
		"conditional request with matching ETag should return 304")
	// Body should be empty on 304.
	assert.Empty(t, strings.TrimSpace(w2.Body.String()),
		"304 response should have empty body")
}
