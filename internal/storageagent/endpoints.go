package storageagent

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
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	agentv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/agent/v1"
)

// EndpointStore holds the active endpoint configurations and S3 clients.
// Thread-safe for concurrent reads (HTTP requests) and writes (bidi updates).
//
// When constructed with a non-nil EndpointStoreConfig.Store, every
// successful Update mirrors to the SQLite store atomically with the
// in-memory swap. Boot-time reload is the caller's responsibility via
// LoadFromStore — see #79 for the broader crash-resilience flow. The
// package only exposes the contract; the boot wiring lives in
// cmd/pivox-agent/.
//
// Persistence semantics: writes are atomic with the in-memory update.
// The new in-memory map is built first (including S3 client
// construction; per-endpoint failure short-circuits the whole batch);
// then the persist call writes the proto bytes for every config in a
// single SQLite transaction; only on success does the in-memory map
// swap. If either step fails the existing in-memory map and the
// existing on-disk set are both untouched.
type EndpointStore struct {
	mu        sync.RWMutex
	endpoints map[string]*endpoint // keyed by endpoint short name
	cache     *MemoryCache
	persist   *Store
}

// EndpointStoreConfig is the constructor input for EndpointStore.
type EndpointStoreConfig struct {
	// Cache is the in-memory blob cache used by S3-backed endpoints.
	// Required.
	Cache *MemoryCache

	// Store, if non-nil, mirrors every successful Update to SQLite
	// atomically with the in-memory replacement. Optional.
	// Zero-value (nil) Store = in-memory only.
	Store *Store
}

// endpoint is a resolved endpoint with a ready-to-use client.
type endpoint struct {
	config       *agentv1.EndpointConfig
	s3           *minio.Client // nil for filesystem endpoints
	cacheEnabled bool
}

// NewEndpointStore constructs an EndpointStore from cfg. Panics if
// cfg.Cache is nil.
func NewEndpointStore(cfg EndpointStoreConfig) *EndpointStore {
	if cfg.Cache == nil {
		panic("storageagent: EndpointStoreConfig.Cache is required")
	}
	return &EndpointStore{
		endpoints: make(map[string]*endpoint),
		cache:     cfg.Cache,
		persist:   cfg.Store,
	}
}

// LoadFromStore reads the persisted endpoint set into memory,
// reconstructing S3 clients along the way. Called once at agent boot,
// before the HTTP listener starts. No-op without an attached store.
//
// MUST be called exactly once, before any Update. Concurrent
// Update/LoadFromStore is not supported — Load unconditionally
// replaces the in-memory map, so a Load racing an Update would
// clobber live state. The agent boot sequence enforces this ordering;
// future admin "reload" endpoints would need a different mechanism.
//
// A per-endpoint S3 client construction failure aborts the whole load
// and returns an error — the agent will then serve no endpoints from
// disk and wait for the controller's HandshakeAck/ConfigUpdate to
// re-deliver a working set. This matches the "all-or-nothing" shape
// of Update.
func (s *EndpointStore) LoadFromStore(ctx context.Context) error {
	if s.persist == nil {
		return nil
	}
	configs, err := s.persist.LoadEndpoints(ctx)
	if err != nil {
		return fmt.Errorf("load endpoints: %w", err)
	}
	resolved, err := s.resolveEndpoints(ctx, configs)
	if err != nil {
		return fmt.Errorf("resolve endpoints from store: %w", err)
	}
	s.mu.Lock()
	s.endpoints = resolved
	s.mu.Unlock()
	return nil
}

// Update replaces all endpoints with the provided configs. Existing S3
// clients are discarded and new ones are created.
//
// Atomic: builds the new in-memory map first (including S3 client
// construction; any per-endpoint failure aborts before either
// in-memory or on-disk state is mutated), then persists the configs
// in a single SQLite transaction, then swaps in-memory. Failure at
// any step leaves both layers untouched.
//
// Lock-holding shape (DIFFERENT from SessionStore.Grant /
// DeniedPatterns.Update): mu.Lock is held ONLY around the in-memory
// map swap, NOT across S3 client construction or the SQLite write.
// This decouples HTTP read tail latency from controller-driven push
// latency — `ServeFile` (RLock) is never blocked on a slow
// `BucketExists` round-trip or an fsync stall. Trade-off: a reader
// between the SQLite commit and the in-memory swap briefly sees
// pre-Update state while disk is current. The window is bounded to a
// single map assignment; if the agent crashes within it, restart
// reloads the persisted state. Acceptable because Update is a
// full-set replacement, not a delta.
func (s *EndpointStore) Update(ctx context.Context, configs []*agentv1.EndpointConfig) error {
	resolved, err := s.resolveEndpoints(ctx, configs)
	if err != nil {
		return err
	}

	if s.persist != nil {
		if err := s.persist.ReplaceEndpoints(ctx, configs); err != nil {
			return fmt.Errorf("update endpoints: %w", err)
		}
	}

	s.mu.Lock()
	s.endpoints = resolved
	s.mu.Unlock()
	return nil
}

// resolveEndpoints builds the in-memory routing map from a slice of
// configs, constructing S3 clients per endpoint. Returns an error
// immediately on the first failure so the caller can leave existing
// state untouched. ctx is propagated through to BucketExists so a
// hung S3 backend respects the caller's deadline (notably the 30s
// boot timeout in cmd/pivox-agent/storage.go).
func (s *EndpointStore) resolveEndpoints(ctx context.Context, configs []*agentv1.EndpointConfig) (map[string]*endpoint, error) {
	out := make(map[string]*endpoint, len(configs))
	for _, cfg := range configs {
		ep := &endpoint{
			config:       cfg,
			cacheEnabled: cfg.GetCacheConfig() != nil && cfg.GetCacheConfig().GetEnabled(),
		}
		if s3Cfg := cfg.GetS3(); s3Cfg != nil {
			client, err := newS3Client(ctx, s3Cfg)
			if err != nil {
				return nil, fmt.Errorf("endpoint %s: create S3 client: %w", cfg.GetName(), err)
			}
			ep.s3 = client
		}
		// Extract the short name from the resource name for routing.
		// e.g. "organizations/acme/storageGateways/gw1/endpoints/media" → "media"
		name := cfg.GetName()
		parts := strings.Split(name, "/")
		shortName := parts[len(parts)-1]
		out[shortName] = ep
	}
	return out, nil
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
	defer func() { _ = obj.Close() }()

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
	if ep.cacheEnabled && s.cache != nil && info.Size <= int64(s.cache.MaxItemSize()) {
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
	defer func() { _ = f.Close() }()

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
// ctx bounds the bucket-existence verification probe; a hung S3
// backend therefore honors the caller's deadline rather than blocking
// indefinitely (relevant during agent boot, where the 30s bootCtx
// must be respected so a misconfigured endpoint can't stall startup).
func newS3Client(ctx context.Context, cfg *agentv1.S3EndpointConfig) (*minio.Client, error) {
	u, err := url.Parse(cfg.GetEndpointUri())
	if err != nil {
		return nil, fmt.Errorf("parse endpoint URI %q: %w", cfg.GetEndpointUri(), err)
	}

	secure := u.Scheme == "https"
	host := u.Host

	// Wrap minio's own tuned transport with otelhttp so each S3 request
	// (GetObject, etc.) gets a client span nested under the /files/ server
	// span. No-op when OTel export is disabled.
	baseTransport, err := minio.DefaultTransport(secure)
	if err != nil {
		return nil, fmt.Errorf("s3 transport: %w", err)
	}

	opts := &minio.Options{
		Creds:     credentials.NewStaticV4(cfg.GetAccessKeyId(), cfg.GetSecretAccessKey(), ""),
		Secure:    secure,
		Transport: otelhttp.NewTransport(baseTransport),
	}

	if cfg.GetRegion() != "" {
		opts.Region = cfg.GetRegion()
	}

	client, err := minio.New(host, opts)
	if err != nil {
		return nil, fmt.Errorf("create client: %w", err)
	}

	// Verify bucket exists. ctx is the caller's; respects bootCtx
	// timeout / signal cancellation rather than the previous
	// context.Background() which could block past the agent's
	// shutdown deadline.
	exists, err := client.BucketExists(ctx, cfg.GetBucket())
	if err != nil {
		return nil, fmt.Errorf("check bucket %q: %w", cfg.GetBucket(), err)
	}
	if !exists {
		return nil, fmt.Errorf("bucket %q does not exist", cfg.GetBucket())
	}

	return client, nil
}
