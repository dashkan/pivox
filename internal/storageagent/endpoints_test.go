package storageagent

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	agentv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/agent/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// EndpointStore.Update
// ---------------------------------------------------------------------------

func TestEndpointStore_Update_Filesystem(t *testing.T) {
	store := NewEndpointStore(EndpointStoreConfig{Cache: NewMemoryCache(10, 1024)})

	err := store.Update(t.Context(), []*agentv1.EndpointConfig{
		{
			Name: "organizations/acme/storageGateways/gw1/endpoints/media",
			Configuration: &agentv1.EndpointConfig_Filesystem{
				Filesystem: &agentv1.FileSystemEndpointConfig{
					Path: "/tmp/test-media",
				},
			},
		},
	})
	require.NoError(t, err)

	store.mu.RLock()
	ep, ok := store.endpoints["media"]
	store.mu.RUnlock()

	require.True(t, ok, "endpoint 'media' should exist")
	assert.Nil(t, ep.s3, "filesystem endpoint should have nil S3 client")
	assert.Equal(t, "/tmp/test-media", ep.config.GetFilesystem().GetPath())
}

func TestEndpointStore_Update_Replace(t *testing.T) {
	store := NewEndpointStore(EndpointStoreConfig{Cache: NewMemoryCache(10, 1024)})

	err := store.Update(t.Context(), []*agentv1.EndpointConfig{
		{
			Name: "organizations/acme/storageGateways/gw1/endpoints/old",
			Configuration: &agentv1.EndpointConfig_Filesystem{
				Filesystem: &agentv1.FileSystemEndpointConfig{Path: "/old"},
			},
		},
	})
	require.NoError(t, err)

	// Replace with new config.
	err = store.Update(t.Context(), []*agentv1.EndpointConfig{
		{
			Name: "organizations/acme/storageGateways/gw1/endpoints/new",
			Configuration: &agentv1.EndpointConfig_Filesystem{
				Filesystem: &agentv1.FileSystemEndpointConfig{Path: "/new"},
			},
		},
	})
	require.NoError(t, err)

	store.mu.RLock()
	_, oldExists := store.endpoints["old"]
	_, newExists := store.endpoints["new"]
	store.mu.RUnlock()

	assert.False(t, oldExists, "old endpoint should be removed after update")
	assert.True(t, newExists, "new endpoint should exist after update")
}

func TestEndpointStore_Update_CacheEnabled(t *testing.T) {
	store := NewEndpointStore(EndpointStoreConfig{Cache: NewMemoryCache(10, 1024)})

	err := store.Update(t.Context(), []*agentv1.EndpointConfig{
		{
			Name: "organizations/acme/storageGateways/gw1/endpoints/cached",
			Configuration: &agentv1.EndpointConfig_Filesystem{
				Filesystem: &agentv1.FileSystemEndpointConfig{Path: "/data"},
			},
			CacheConfig: &agentv1.EndpointCacheConfig{Enabled: true},
		},
	})
	require.NoError(t, err)

	store.mu.RLock()
	ep := store.endpoints["cached"]
	store.mu.RUnlock()

	assert.True(t, ep.cacheEnabled)
}

// ---------------------------------------------------------------------------
// ServeFile — routing
// ---------------------------------------------------------------------------

func TestServeFile_NotFound_NoEndpoint(t *testing.T) {
	store := NewEndpointStore(EndpointStoreConfig{Cache: NewMemoryCache(10, 1024)})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/nonexistent/file.txt", nil)

	store.ServeFile(w, r)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeFile_NotFound_EmptyPath(t *testing.T) {
	store := NewEndpointStore(EndpointStoreConfig{Cache: NewMemoryCache(10, 1024)})

	tests := []struct {
		name string
		path string
	}{
		{"root", "/"},
		{"single segment", "/media"},
		{"empty key", "/media/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, tt.path, nil)
			store.ServeFile(w, r)
			assert.Equal(t, http.StatusNotFound, w.Code)
		})
	}
}

func TestServeFile_NoConfig(t *testing.T) {
	store := NewEndpointStore(EndpointStoreConfig{Cache: NewMemoryCache(10, 1024)})

	// Register an endpoint with no S3 or filesystem config.
	store.mu.Lock()
	store.endpoints["broken"] = &endpoint{
		config: &agentv1.EndpointConfig{Name: "broken"},
	}
	store.mu.Unlock()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/broken/file.txt", nil)
	store.ServeFile(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// ---------------------------------------------------------------------------
// serveFilesystem
// ---------------------------------------------------------------------------

func TestServeFilesystem_Success(t *testing.T) {
	dir := t.TempDir()
	content := []byte("test file content")
	err := os.WriteFile(filepath.Join(dir, "test.txt"), content, 0o644)
	require.NoError(t, err)

	store := NewEndpointStore(EndpointStoreConfig{Cache: NewMemoryCache(10, 1024)})
	err = store.Update(t.Context(), []*agentv1.EndpointConfig{
		{
			Name: "organizations/acme/storageGateways/gw1/endpoints/local",
			Configuration: &agentv1.EndpointConfig_Filesystem{
				Filesystem: &agentv1.FileSystemEndpointConfig{Path: dir},
			},
		},
	})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/local/test.txt", nil)
	store.ServeFile(w, r)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "test file content", w.Body.String())
	assert.Contains(t, w.Header().Get("Cache-Control"), "immutable")
}

func TestServeFilesystem_NotFound(t *testing.T) {
	dir := t.TempDir()

	store := NewEndpointStore(EndpointStoreConfig{Cache: NewMemoryCache(10, 1024)})
	err := store.Update(t.Context(), []*agentv1.EndpointConfig{
		{
			Name: "organizations/acme/storageGateways/gw1/endpoints/local",
			Configuration: &agentv1.EndpointConfig_Filesystem{
				Filesystem: &agentv1.FileSystemEndpointConfig{Path: dir},
			},
		},
	})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/local/nonexistent.txt", nil)
	store.ServeFile(w, r)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeFilesystem_PathTraversal(t *testing.T) {
	dir := t.TempDir()

	store := NewEndpointStore(EndpointStoreConfig{Cache: NewMemoryCache(10, 1024)})
	err := store.Update(t.Context(), []*agentv1.EndpointConfig{
		{
			Name: "organizations/acme/storageGateways/gw1/endpoints/local",
			Configuration: &agentv1.EndpointConfig_Filesystem{
				Filesystem: &agentv1.FileSystemEndpointConfig{Path: dir},
			},
		},
	})
	require.NoError(t, err)

	tests := []struct {
		name string
		path string
	}{
		{"dotdot", "/local/../../../etc/passwd"},
		{"encoded dotdot", "/local/..%2F..%2Fetc/passwd"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, tt.path, nil)
			store.ServeFile(w, r)
			// Should get 403 or 404, never 200.
			assert.NotEqual(t, http.StatusOK, w.Code)
		})
	}
}

func TestServeFilesystem_Directory(t *testing.T) {
	dir := t.TempDir()
	err := os.MkdirAll(filepath.Join(dir, "subdir"), 0o755)
	require.NoError(t, err)

	store := NewEndpointStore(EndpointStoreConfig{Cache: NewMemoryCache(10, 1024)})
	err = store.Update(t.Context(), []*agentv1.EndpointConfig{
		{
			Name: "organizations/acme/storageGateways/gw1/endpoints/local",
			Configuration: &agentv1.EndpointConfig_Filesystem{
				Filesystem: &agentv1.FileSystemEndpointConfig{Path: dir},
			},
		},
	})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/local/subdir", nil)
	store.ServeFile(w, r)

	assert.Equal(t, http.StatusNotFound, w.Code, "directories should not be served")
}

// ---------------------------------------------------------------------------
// Cache integration with ServeFile
// ---------------------------------------------------------------------------

func TestServeFile_CacheHit(t *testing.T) {
	cache := NewMemoryCache(100, 1024*1024)
	store := NewEndpointStore(EndpointStoreConfig{Cache: cache})

	dir := t.TempDir()
	content := []byte("cached content")
	err := os.WriteFile(filepath.Join(dir, "asset.bin"), content, 0o644)
	require.NoError(t, err)

	err = store.Update(t.Context(), []*agentv1.EndpointConfig{
		{
			Name: "organizations/acme/storageGateways/gw1/endpoints/ep",
			Configuration: &agentv1.EndpointConfig_Filesystem{
				Filesystem: &agentv1.FileSystemEndpointConfig{Path: dir},
			},
			CacheConfig: &agentv1.EndpointCacheConfig{Enabled: true},
		},
	})
	require.NoError(t, err)

	// Pre-populate cache.
	cache.Put("ep/asset.bin", content, "application/octet-stream", "etag-1",
		time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/ep/asset.bin", nil)
	store.ServeFile(w, r)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "HIT", w.Header().Get("X-Cache"))
}
