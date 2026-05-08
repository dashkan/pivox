package storageagent

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agentv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/agent/v1"
)

// fsEndpoint returns a Filesystem-backed EndpointConfig for tests
// that don't want to touch S3 (Filesystem endpoints construct without
// any external connection).
func fsEndpoint(name, path string) *agentv1.EndpointConfig {
	return &agentv1.EndpointConfig{
		Name: name,
		Configuration: &agentv1.EndpointConfig_Filesystem{
			Filesystem: &agentv1.FileSystemEndpointConfig{Path: path},
		},
	}
}

// openEndpointsWithPersist returns an EndpointStore wired through a
// fresh Store at a tempdir path.
func openEndpointsWithPersist(t *testing.T) (*EndpointStore, *Store) {
	t.Helper()
	st, err := OpenStore(StoreConfig{Path: filepath.Join(t.TempDir(), "agent.db")})
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	endpoints := NewEndpointStore(EndpointStoreConfig{
		Cache: NewMemoryCache(10, 1024),
		Store: st,
	})
	return endpoints, st
}

func TestEndpointStore_Update_PersistsToStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	endpoints, store := openEndpointsWithPersist(t)

	cfgs := []*agentv1.EndpointConfig{
		fsEndpoint("organizations/acme/storageGateways/gw/endpoints/media", t.TempDir()),
		fsEndpoint("organizations/acme/storageGateways/gw/endpoints/local", t.TempDir()),
	}
	require.NoError(t, endpoints.Update(ctx, cfgs))

	// Persisted.
	rows, err := store.LoadEndpoints(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 2)

	byName := map[string]*agentv1.EndpointConfig{}
	for _, c := range rows {
		byName[c.GetName()] = c
	}
	require.Contains(t, byName, cfgs[0].GetName())
	require.Contains(t, byName, cfgs[1].GetName())
}

func TestEndpointStore_Update_AtomicOnPersistFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	st, err := OpenStore(StoreConfig{Path: filepath.Join(t.TempDir(), "agent.db")})
	require.NoError(t, err)
	endpoints := NewEndpointStore(EndpointStoreConfig{
		Cache: NewMemoryCache(10, 1024),
		Store: st,
	})

	// Seed an initial endpoint.
	require.NoError(t, endpoints.Update(ctx, []*agentv1.EndpointConfig{
		fsEndpoint("organizations/acme/storageGateways/gw/endpoints/initial", t.TempDir()),
	}))

	// Close the Store to force the next persist to fail.
	require.NoError(t, st.Close())

	// Attempt a replacement — must error AND leave in-memory unchanged.
	err = endpoints.Update(ctx, []*agentv1.EndpointConfig{
		fsEndpoint("organizations/acme/storageGateways/gw/endpoints/replacement", t.TempDir()),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "update endpoints",
		"error must be wrapped at the storeagent layer")
	assert.Contains(t, err.Error(), "closed",
		"error must surface the underlying SQLite-closed condition")

	// In-memory still has "initial", not "replacement".
	endpoints.mu.RLock()
	defer endpoints.mu.RUnlock()
	assert.Contains(t, endpoints.endpoints, "initial",
		"failed Update must NOT have replaced the in-memory map")
	assert.NotContains(t, endpoints.endpoints, "replacement",
		"failed Update must NOT have applied the new config")
}

// TestEndpointStore_LoadFromStore_RestartsServing is the load-bearing
// #79 acceptance criterion at the EndpointStore layer: persist a
// filesystem endpoint, "restart" by constructing a fresh
// EndpointStore against the same SQLite DB, LoadFromStore, confirm
// the endpoint is reachable in-memory.
func TestEndpointStore_LoadFromStore_RestartsServing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	dbPath := filepath.Join(t.TempDir(), "agent.db")
	mediaDir := t.TempDir()
	const fullName = "organizations/acme/storageGateways/gw/endpoints/media"

	// First "process": persist the endpoint, close.
	{
		st, err := OpenStore(StoreConfig{Path: dbPath})
		require.NoError(t, err)
		endpoints := NewEndpointStore(EndpointStoreConfig{
			Cache: NewMemoryCache(10, 1024),
			Store: st,
		})
		require.NoError(t, endpoints.Update(ctx, []*agentv1.EndpointConfig{
			fsEndpoint(fullName, mediaDir),
		}))
		require.NoError(t, st.Close())
	}

	// Second "process": fresh EndpointStore + same DB + LoadFromStore.
	st, err := OpenStore(StoreConfig{Path: dbPath})
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	endpoints := NewEndpointStore(EndpointStoreConfig{
		Cache: NewMemoryCache(10, 1024),
		Store: st,
	})
	require.NoError(t, endpoints.LoadFromStore(ctx))

	// In-memory map is populated. The short name is "media" (the
	// trailing path segment of the resource name).
	endpoints.mu.RLock()
	defer endpoints.mu.RUnlock()
	require.Contains(t, endpoints.endpoints, "media",
		"persisted endpoint must reload into the routing map keyed "+
			"by its short name without any controller round-trip")
	assert.Equal(t, fullName, endpoints.endpoints["media"].config.GetName())
	assert.Equal(t, mediaDir, endpoints.endpoints["media"].config.GetFilesystem().GetPath())
}

func TestEndpointStore_NoStoreAttached(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	endpoints := NewEndpointStore(EndpointStoreConfig{
		Cache: NewMemoryCache(10, 1024),
	})
	require.NoError(t, endpoints.Update(ctx, []*agentv1.EndpointConfig{
		fsEndpoint("organizations/acme/storageGateways/gw/endpoints/m", t.TempDir()),
	}))

	// LoadFromStore is a no-op without a store.
	require.NoError(t, endpoints.LoadFromStore(ctx))
}

// TestEndpointStore_Update_RejectsFailingS3Endpoint preserves the
// existing pre-flight contract: if the controller pushes an S3 config
// pointing at an unreachable backend, Update returns an error before
// touching either persistence or the in-memory map.
func TestEndpointStore_Update_RejectsFailingS3Endpoint(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	endpoints, store := openEndpointsWithPersist(t)

	// Seed a known-good endpoint.
	require.NoError(t, endpoints.Update(ctx, []*agentv1.EndpointConfig{
		fsEndpoint("organizations/acme/storageGateways/gw/endpoints/safe", t.TempDir()),
	}))

	// Push a config that fails newS3Client (unreachable host).
	err := endpoints.Update(ctx, []*agentv1.EndpointConfig{
		{
			Name: "organizations/acme/storageGateways/gw/endpoints/bad",
			Configuration: &agentv1.EndpointConfig_S3{
				S3: &agentv1.S3EndpointConfig{
					EndpointUri: "http://127.0.0.1:1",
					Bucket:      "nonexistent",
				},
			},
		},
	})
	require.Error(t, err,
		"S3 client construction failure must propagate from Update")

	// In-memory still has "safe", not the failing config.
	endpoints.mu.RLock()
	require.Contains(t, endpoints.endpoints, "safe")
	require.NotContains(t, endpoints.endpoints, "bad")
	endpoints.mu.RUnlock()

	// And the persisted set must still match the safe-only state —
	// the failed Update must not have written the bad config.
	rows, err := store.LoadEndpoints(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Contains(t, rows[0].GetName(), "/safe")
}
