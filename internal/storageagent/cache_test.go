package storageagent

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMemoryCache_Defaults(t *testing.T) {
	mc := NewMemoryCache(0, 0)
	require.NotNil(t, mc)
	assert.Equal(t, defaultMaxItemSize, mc.maxItemSize)

	mc2 := NewMemoryCache(-1, -1)
	require.NotNil(t, mc2)
	assert.Equal(t, defaultMaxItemSize, mc2.maxItemSize)
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
	const maxItemSize = 1024
	mc := NewMemoryCache(100, maxItemSize)

	largeBody := make([]byte, maxItemSize+1)

	ok := mc.Put("big", largeBody, "application/octet-stream", "etag", time.Now())
	assert.False(t, ok, "objects larger than maxItemSize should not be cached")
}

func TestLRUEviction(t *testing.T) {
	const maxEntries = 3
	mc := NewMemoryCache(maxEntries, 1024*1024)

	for i := range maxEntries {
		key := string(rune('a'+i)) + "_key"
		mc.Put(key, []byte("data"), "text/plain", "etag", time.Now())
	}

	mc.Put("new_key", []byte("data"), "text/plain", "etag", time.Now())

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	hit := mc.Get(w, r, "a_key")
	assert.False(t, hit, "oldest entry should have been evicted by LRU")

	w = httptest.NewRecorder()
	hit = mc.Get(w, r, "new_key")
	assert.True(t, hit, "newest entry should be present")
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

	assert.Equal(t, 2, mc.Stats())

	mc.Invalidate("a")
	assert.Equal(t, 1, mc.Stats())
}

func TestConditionalRequest(t *testing.T) {
	mc := NewMemoryCache(100, 1024*1024)
	now := time.Now().Truncate(time.Second)

	mc.Put("asset", []byte("body content"), "text/plain", "etag-value", now)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/asset", nil)
	mc.Get(w, r, "asset")
	etag := w.Result().Header.Get("ETag")
	require.NotEmpty(t, etag)

	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest(http.MethodGet, "/asset", nil)
	r2.Header.Set("If-None-Match", etag)
	hit := mc.Get(w2, r2, "asset")
	assert.True(t, hit, "key should be found in cache")

	resp := w2.Result()
	assert.Equal(t, http.StatusNotModified, resp.StatusCode,
		"conditional request with matching ETag should return 304")
	assert.Empty(t, strings.TrimSpace(w2.Body.String()),
		"304 response should have empty body")
}

// TestConcurrentPutGet exercises the underlying lru.Cache under -race.
// The previous byte-budgeted implementation had a release-and-reacquire
// pattern around RemoveOldest that leaked accounting under concurrent Put;
// this regression test pins the simpler shape: lru.Cache is goroutine-safe
// on its own, so MemoryCache is too.
func TestConcurrentPutGet(t *testing.T) {
	const (
		writers    = 8
		readers    = 8
		opsPerG    = 200
		maxEntries = 64
	)
	mc := NewMemoryCache(maxEntries, 1024)

	var wg sync.WaitGroup
	wg.Add(writers + readers)

	now := time.Now()
	for w := range writers {
		go func(id int) {
			defer wg.Done()
			for i := range opsPerG {
				key := fmt.Sprintf("w%d-k%d", id, i)
				mc.Put(key, []byte("payload"), "text/plain", "etag", now)
			}
		}(w)
	}

	for r := range readers {
		go func(id int) {
			defer wg.Done()
			for i := range opsPerG {
				key := fmt.Sprintf("w%d-k%d", id%writers, i)
				rec := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodGet, "/x", nil)
				_ = mc.Get(rec, req, key)
			}
		}(r)
	}

	wg.Wait()

	// LRU should hold no more than its cap, regardless of how many writers
	// raced.
	assert.LessOrEqual(t, mc.Stats(), maxEntries)
}
