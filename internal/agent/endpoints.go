package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	agentv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/agent/v1"
)

// EndpointStore holds the active endpoint configurations and S3 clients.
// Thread-safe for concurrent reads (HTTP requests) and writes (bidi updates).
type EndpointStore struct {
	mu        sync.RWMutex
	endpoints map[string]*endpoint // keyed by endpoint name
	cache     *MemoryCache
}

// endpoint is a resolved endpoint with a ready-to-use client.
type endpoint struct {
	config       *agentv1.EndpointConfig
	s3           *minio.Client // nil for filesystem endpoints
	cacheEnabled bool
}

// NewEndpointStore creates an empty EndpointStore with a memory cache.
func NewEndpointStore(cache *MemoryCache) *EndpointStore {
	return &EndpointStore{
		endpoints: make(map[string]*endpoint),
		cache:     cache,
	}
}

// Update replaces all endpoints with the provided configs. Existing S3
// clients are discarded and new ones are created.
func (s *EndpointStore) Update(configs []*agentv1.EndpointConfig) error {
	endpoints := make(map[string]*endpoint, len(configs))

	for _, cfg := range configs {
		ep := &endpoint{
			config:       cfg,
			cacheEnabled: cfg.GetCacheConfig() != nil && cfg.GetCacheConfig().GetEnabled(),
		}

		if s3Cfg := cfg.GetS3(); s3Cfg != nil {
			client, err := newS3Client(s3Cfg)
			if err != nil {
				return fmt.Errorf("endpoint %s: create S3 client: %w", cfg.GetName(), err)
			}
			ep.s3 = client
		}

		// Extract the short name from the resource name for routing.
		// e.g. "organizations/acme/storageGateways/gw1/endpoints/media" → "media"
		name := cfg.GetName()
		parts := strings.Split(name, "/")
		shortName := parts[len(parts)-1]
		endpoints[shortName] = ep
	}

	s.mu.Lock()
	s.endpoints = endpoints
	s.mu.Unlock()
	return nil
}

// ServeFile handles an HTTP request by routing to the correct endpoint
// and proxying the file. The path format is /{endpoint}/{object-key...}.
func (s *EndpointStore) ServeFile(w http.ResponseWriter, r *http.Request) {
	// Parse path: /{endpoint-name}/{rest-of-key}
	path := strings.TrimPrefix(r.URL.Path, "/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	endpointName := parts[0]
	objectKey := parts[1]

	s.mu.RLock()
	ep, ok := s.endpoints[endpointName]
	s.mu.RUnlock()

	if !ok {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	// Check memory cache first.
	cacheKey := endpointName + "/" + objectKey
	if ep.cacheEnabled && s.cache != nil && s.cache.Get(w, r, cacheKey) {
		return
	}

	if ep.s3 != nil {
		s.serveS3(w, r, ep, objectKey, cacheKey)
	} else if fsCfg := ep.config.GetFilesystem(); fsCfg != nil {
		s.serveFilesystem(w, r, fsCfg, objectKey)
	} else {
		http.Error(w, "endpoint has no configuration", http.StatusInternalServerError)
	}
}

// serveS3 proxies a GET request to an S3-compatible backend.
func (s *EndpointStore) serveS3(w http.ResponseWriter, r *http.Request, ep *endpoint, objectKey string, cacheKey string) {
	bucket := ep.config.GetS3().GetBucket()

	obj, err := ep.s3.GetObject(r.Context(), bucket, objectKey, minio.GetObjectOptions{})
	if err != nil {
		http.Error(w, "upstream error", http.StatusBadGateway)
		return
	}
	defer obj.Close()

	info, err := obj.Stat()
	if err != nil {
		errResp := minio.ToErrorResponse(err)
		if errResp.Code == "NoSuchKey" {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		http.Error(w, "upstream error", http.StatusBadGateway)
		return
	}

	// Assets are versioned in their storage key (assets/{id}/v{n}/file.ext)
	// so they are effectively immutable. Set aggressive cache headers.
	w.Header().Set("Content-Type", info.ContentType)
	w.Header().Set("ETag", `"`+info.ETag+`"`)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("X-Cache", "MISS")

	// For cacheable objects, read into buffer, cache, and serve via
	// ServeContent (handles Range, If-None-Match, If-Modified-Since).
	if ep.cacheEnabled && s.cache != nil && info.Size <= maxCacheableSize {
		buf, err := io.ReadAll(obj)
		if err == nil {
			s.cache.Put(cacheKey, buf, info.ContentType, info.ETag, info.LastModified)
			http.ServeContent(w, r, objectKey, info.LastModified, bytes.NewReader(buf))
			return
		}
		// ReadAll failed — fall through to direct streaming.
	}

	// Large or non-cacheable objects: ServeContent with the S3 object
	// directly. minio's Object implements io.ReadSeeker.
	http.ServeContent(w, r, objectKey, info.LastModified, obj)
}

// serveFilesystem serves a file from a local/NFS-mounted filesystem.
func (s *EndpointStore) serveFilesystem(w http.ResponseWriter, r *http.Request, cfg *agentv1.FileSystemEndpointConfig, objectKey string) {
	// Prevent path traversal.
	cleaned := filepath.Clean(objectKey)
	if strings.Contains(cleaned, "..") {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	fullPath := filepath.Join(cfg.GetPath(), cleaned)

	// Verify the resolved path is still under the mount point.
	absMount, _ := filepath.Abs(cfg.GetPath())
	absPath, _ := filepath.Abs(fullPath)
	if !strings.HasPrefix(absPath, absMount) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	f, err := os.Open(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil || stat.IsDir() {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	// Immutable assets — aggressive caching.
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	// http.ServeContent handles Content-Type detection, Range requests,
	// and conditional requests (If-Modified-Since, If-None-Match).
	http.ServeContent(w, r, stat.Name(), stat.ModTime(), f)
}

// newS3Client creates a minio client from an S3 endpoint config.
func newS3Client(cfg *agentv1.S3EndpointConfig) (*minio.Client, error) {
	u, err := url.Parse(cfg.GetEndpointUri())
	if err != nil {
		return nil, fmt.Errorf("parse endpoint URI %q: %w", cfg.GetEndpointUri(), err)
	}

	secure := u.Scheme == "https"
	host := u.Host

	opts := &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.GetAccessKeyId(), cfg.GetSecretAccessKey(), ""),
		Secure: secure,
	}

	if cfg.GetRegion() != "" {
		opts.Region = cfg.GetRegion()
	}

	client, err := minio.New(host, opts)
	if err != nil {
		return nil, fmt.Errorf("create client: %w", err)
	}

	// Verify bucket exists.
	exists, err := client.BucketExists(context.Background(), cfg.GetBucket())
	if err != nil {
		return nil, fmt.Errorf("check bucket %q: %w", cfg.GetBucket(), err)
	}
	if !exists {
		return nil, fmt.Errorf("bucket %q does not exist", cfg.GetBucket())
	}

	return client, nil
}
