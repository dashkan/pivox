package storageagent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agentv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/agent/v1"
	"github.com/dashkan/pivox/internal/testutil"
)

// ---------------------------------------------------------------------------
// newS3Client
// ---------------------------------------------------------------------------

func TestNewS3Client_Success(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping S3 integration test in short mode")
	}

	_, endpoint, bucketName := testutil.SetupTestS3(t)

	client, err := newS3Client(&agentv1.S3EndpointConfig{
		EndpointUri:     "http://" + endpoint,
		Bucket:          bucketName,
		AccessKeyId:     "testaccess",
		SecretAccessKey: "testsecret",
	})
	require.NoError(t, err)
	assert.NotNil(t, client)
}

func TestNewS3Client_BucketNotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping S3 integration test in short mode")
	}

	_, endpoint, _ := testutil.SetupTestS3(t)

	_, err := newS3Client(&agentv1.S3EndpointConfig{
		EndpointUri:     "http://" + endpoint,
		Bucket:          "nonexistent-bucket",
		AccessKeyId:     "testaccess",
		SecretAccessKey: "testsecret",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
}

func TestNewS3Client_BadEndpoint(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping S3 integration test in short mode")
	}

	_, err := newS3Client(&agentv1.S3EndpointConfig{
		EndpointUri:     "http://127.0.0.1:1",
		Bucket:          "some-bucket",
		AccessKeyId:     "testaccess",
		SecretAccessKey: "testsecret",
	})
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// EndpointStore.Update with S3
// ---------------------------------------------------------------------------

func TestEndpointStore_Update_S3(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping S3 integration test in short mode")
	}

	_, endpoint, bucketName := testutil.SetupTestS3(t)

	store := NewEndpointStore(EndpointStoreConfig{Cache: NewMemoryCache(10, 1024*1024)})
	err := store.Update(t.Context(), []*agentv1.EndpointConfig{
		{
			Name: "organizations/acme/storageGateways/gw1/endpoints/s3ep",
			Configuration: &agentv1.EndpointConfig_S3{
				S3: &agentv1.S3EndpointConfig{
					EndpointUri:     "http://" + endpoint,
					Bucket:          bucketName,
					AccessKeyId:     "testaccess",
					SecretAccessKey: "testsecret",
				},
			},
		},
	})
	require.NoError(t, err)

	store.mu.RLock()
	ep, ok := store.endpoints["s3ep"]
	store.mu.RUnlock()

	require.True(t, ok, "endpoint 's3ep' should exist")
	assert.NotNil(t, ep.s3, "S3 endpoint should have a non-nil S3 client")
}

// ---------------------------------------------------------------------------
// serveS3
// ---------------------------------------------------------------------------

func TestServeS3_Success(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping S3 integration test in short mode")
	}

	s3Client, endpoint, bucketName := testutil.SetupTestS3(t)

	ctx := context.Background()
	content := "hello world"
	_, err := s3Client.PutObject(ctx, bucketName, "test-key.txt",
		strings.NewReader(content), int64(len(content)),
		minio.PutObjectOptions{ContentType: "text/plain"})
	require.NoError(t, err)

	store := NewEndpointStore(EndpointStoreConfig{Cache: NewMemoryCache(10, 1024*1024)})
	err = store.Update(t.Context(), []*agentv1.EndpointConfig{
		{
			Name: "organizations/acme/storageGateways/gw1/endpoints/s3ep",
			Configuration: &agentv1.EndpointConfig_S3{
				S3: &agentv1.S3EndpointConfig{
					EndpointUri:     "http://" + endpoint,
					Bucket:          bucketName,
					AccessKeyId:     "testaccess",
					SecretAccessKey: "testsecret",
				},
			},
		},
	})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/s3ep/test-key.txt", nil)
	store.ServeFile(w, r)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, content, w.Body.String())
	assert.Equal(t, "text/plain", w.Header().Get("Content-Type"))
	assert.NotEmpty(t, w.Header().Get("ETag"))
	assert.Contains(t, w.Header().Get("Cache-Control"), "immutable")
	assert.Equal(t, "MISS", w.Header().Get("X-Cache"))
}

func TestServeS3_NotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping S3 integration test in short mode")
	}

	_, endpoint, bucketName := testutil.SetupTestS3(t)

	store := NewEndpointStore(EndpointStoreConfig{Cache: NewMemoryCache(10, 1024*1024)})
	err := store.Update(t.Context(), []*agentv1.EndpointConfig{
		{
			Name: "organizations/acme/storageGateways/gw1/endpoints/s3ep",
			Configuration: &agentv1.EndpointConfig_S3{
				S3: &agentv1.S3EndpointConfig{
					EndpointUri:     "http://" + endpoint,
					Bucket:          bucketName,
					AccessKeyId:     "testaccess",
					SecretAccessKey: "testsecret",
				},
			},
		},
	})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/s3ep/nonexistent-key.txt", nil)
	store.ServeFile(w, r)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeS3_WithCache(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping S3 integration test in short mode")
	}

	s3Client, endpoint, bucketName := testutil.SetupTestS3(t)

	ctx := context.Background()
	content := "cached hello"
	_, err := s3Client.PutObject(ctx, bucketName, "cached-key.txt",
		strings.NewReader(content), int64(len(content)),
		minio.PutObjectOptions{ContentType: "text/plain"})
	require.NoError(t, err)

	cache := NewMemoryCache(100, 1024*1024)
	store := NewEndpointStore(EndpointStoreConfig{Cache: cache})
	err = store.Update(t.Context(), []*agentv1.EndpointConfig{
		{
			Name: "organizations/acme/storageGateways/gw1/endpoints/s3ep",
			Configuration: &agentv1.EndpointConfig_S3{
				S3: &agentv1.S3EndpointConfig{
					EndpointUri:     "http://" + endpoint,
					Bucket:          bucketName,
					AccessKeyId:     "testaccess",
					SecretAccessKey: "testsecret",
				},
			},
			CacheConfig: &agentv1.EndpointCacheConfig{Enabled: true},
		},
	})
	require.NoError(t, err)

	// First request — cache MISS.
	w1 := httptest.NewRecorder()
	r1 := httptest.NewRequest(http.MethodGet, "/s3ep/cached-key.txt", nil)
	store.ServeFile(w1, r1)

	require.Equal(t, http.StatusOK, w1.Code)
	assert.Equal(t, content, w1.Body.String())
	assert.Equal(t, "MISS", w1.Header().Get("X-Cache"))

	// Second request — cache HIT.
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest(http.MethodGet, "/s3ep/cached-key.txt", nil)
	store.ServeFile(w2, r2)

	require.Equal(t, http.StatusOK, w2.Code)
	assert.Equal(t, content, w2.Body.String())
	assert.Equal(t, "HIT", w2.Header().Get("X-Cache"))
}

func TestServeS3_LargeObject_NoCache(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping S3 integration test in short mode")
	}

	s3Client, endpoint, bucketName := testutil.SetupTestS3(t)

	ctx := context.Background()

	// Cache configured with a small per-item cap; the test object exceeds it
	// and must be served directly without going through the cache.
	const itemCap = 1 * 1024 * 1024
	largeContent := strings.Repeat("x", itemCap+1)
	_, err := s3Client.PutObject(ctx, bucketName, "large-key.bin",
		strings.NewReader(largeContent), int64(len(largeContent)),
		minio.PutObjectOptions{ContentType: "application/octet-stream"})
	require.NoError(t, err)

	cache := NewMemoryCache(100, itemCap)
	store := NewEndpointStore(EndpointStoreConfig{Cache: cache})
	err = store.Update(t.Context(), []*agentv1.EndpointConfig{
		{
			Name: "organizations/acme/storageGateways/gw1/endpoints/s3ep",
			Configuration: &agentv1.EndpointConfig_S3{
				S3: &agentv1.S3EndpointConfig{
					EndpointUri:     "http://" + endpoint,
					Bucket:          bucketName,
					AccessKeyId:     "testaccess",
					SecretAccessKey: "testsecret",
				},
			},
			CacheConfig: &agentv1.EndpointCacheConfig{Enabled: true},
		},
	})
	require.NoError(t, err)

	// First request — should serve correctly but NOT cache.
	w1 := httptest.NewRecorder()
	r1 := httptest.NewRequest(http.MethodGet, "/s3ep/large-key.bin", nil)
	store.ServeFile(w1, r1)

	require.Equal(t, http.StatusOK, w1.Code)
	assert.Equal(t, len(largeContent), w1.Body.Len())
	assert.Equal(t, "MISS", w1.Header().Get("X-Cache"))

	// Second request — should still be MISS (not cached due to size).
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest(http.MethodGet, "/s3ep/large-key.bin", nil)
	store.ServeFile(w2, r2)

	require.Equal(t, http.StatusOK, w2.Code)
	assert.Equal(t, len(largeContent), w2.Body.Len())
	assert.Equal(t, "MISS", w2.Header().Get("X-Cache"))
}
