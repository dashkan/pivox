package storageagent

import (
	"context"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agentv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/agent/v1"
)

// openTestStore returns a fresh Store backed by a tempdir SQLite file.
// t.Cleanup closes it on test exit.
func openTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := OpenStore(StoreConfig{Path: filepath.Join(t.TempDir(), "agent.db")})
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestOpenStore_RequiresPath(t *testing.T) {
	t.Parallel()
	assert.PanicsWithValue(t,
		"storageagent: StoreConfig.Path is required",
		func() { _, _ = OpenStore(StoreConfig{}) },
		"empty Path is a programmer error and must panic")
}

func TestOpenStore_CreatesFreshFile(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := openTestStore(t)

	sessions, err := st.LoadSessions(ctx)
	require.NoError(t, err)
	assert.Empty(t, sessions)

	denied, err := st.LoadDeniedPatterns(ctx)
	require.NoError(t, err)
	assert.Empty(t, denied)

	endpoints, err := st.LoadEndpoints(ctx)
	require.NoError(t, err)
	assert.Empty(t, endpoints)
}

func TestOpenStore_RejectsSchemaVersionMismatch(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "agent.db")

	// First open writes schemaVersion (1).
	st, err := OpenStore(StoreConfig{Path: path})
	require.NoError(t, err)
	require.NoError(t, st.Close())

	// Manually corrupt the version row to simulate a stale DB from a
	// future binary that bumped schemaVersion.
	st2, err := OpenStore(StoreConfig{Path: path})
	require.NoError(t, err)
	_, err = st2.db.ExecContext(context.Background(),
		`UPDATE schema_meta SET version = 999`)
	require.NoError(t, err)
	require.NoError(t, st2.Close())

	// Reopen — should fail fast.
	_, err = OpenStore(StoreConfig{Path: path})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "schema_meta.version mismatch")
}

func TestStore_Session_SaveAndLoad(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := openTestStore(t)

	expiry := time.Now().Add(1 * time.Hour).UTC().Truncate(time.Second)
	sess := StoredSession{
		Token:    "tok-1",
		Patterns: []string{"/spaces/a/*", "/spaces/b/asset/*"},
		Expiry:   expiry,
	}
	require.NoError(t, st.SaveSession(ctx, sess))

	loaded, err := st.LoadSessions(ctx)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	assert.Equal(t, sess.Token, loaded[0].Token)
	assert.Equal(t, sess.Patterns, loaded[0].Patterns)
	assert.True(t, loaded[0].Expiry.Equal(sess.Expiry),
		"expiry mismatch: want %v got %v", sess.Expiry, loaded[0].Expiry)
}

func TestStore_Session_RequiresToken(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := openTestStore(t)

	err := st.SaveSession(ctx, StoredSession{
		Token:    "",
		Patterns: []string{"/x/*"},
		Expiry:   time.Now().Add(time.Hour),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "token is required")
}

func TestStore_Session_NilAndEmptyPatternsRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := openTestStore(t)

	expiry := time.Now().Add(1 * time.Hour).UTC().Truncate(time.Second)

	// nil patterns: round-trip as nil (or empty — Authorize ranges over
	// either identically; we accept both shapes).
	require.NoError(t, st.SaveSession(ctx, StoredSession{
		Token: "tok-nil", Patterns: nil, Expiry: expiry,
	}))
	// empty patterns: round-trip as []string{} or nil.
	require.NoError(t, st.SaveSession(ctx, StoredSession{
		Token: "tok-empty", Patterns: []string{}, Expiry: expiry,
	}))

	loaded, err := st.LoadSessions(ctx)
	require.NoError(t, err)
	require.Len(t, loaded, 2)
	for _, s := range loaded {
		assert.Empty(t, s.Patterns,
			"both nil and []string{} should round-trip as an empty pattern set")
	}
}

func TestStore_Session_OverwriteOnDuplicateToken(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := openTestStore(t)

	exp1 := time.Now().Add(1 * time.Hour).UTC().Truncate(time.Second)
	exp2 := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)

	require.NoError(t, st.SaveSession(ctx, StoredSession{
		Token: "tok-dup", Patterns: []string{"/old/*"}, Expiry: exp1,
	}))
	require.NoError(t, st.SaveSession(ctx, StoredSession{
		Token: "tok-dup", Patterns: []string{"/new/*"}, Expiry: exp2,
	}))

	loaded, err := st.LoadSessions(ctx)
	require.NoError(t, err)
	require.Len(t, loaded, 1, "duplicate token should overwrite, not insert")
	assert.Equal(t, []string{"/new/*"}, loaded[0].Patterns)
	assert.True(t, loaded[0].Expiry.Equal(exp2))
}

func TestStore_Session_Delete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := openTestStore(t)

	expiry := time.Now().Add(1 * time.Hour).UTC().Truncate(time.Second)
	require.NoError(t, st.SaveSession(ctx, StoredSession{
		Token: "tok-keep", Patterns: []string{"/k/*"}, Expiry: expiry,
	}))
	require.NoError(t, st.SaveSession(ctx, StoredSession{
		Token: "tok-drop", Patterns: []string{"/d/*"}, Expiry: expiry,
	}))

	require.NoError(t, st.DeleteSession(ctx, "tok-drop"))

	loaded, err := st.LoadSessions(ctx)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	assert.Equal(t, "tok-keep", loaded[0].Token)
}

func TestStore_Session_DeleteExpired(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := openTestStore(t)

	now := time.Now().UTC().Truncate(time.Second)
	past := now.Add(-1 * time.Hour)
	atBoundary := now // expiry == now must be KEPT (matches Authorize)
	future := now.Add(1 * time.Hour)

	require.NoError(t, st.SaveSession(ctx, StoredSession{
		Token: "tok-past", Patterns: []string{"/*"}, Expiry: past,
	}))
	require.NoError(t, st.SaveSession(ctx, StoredSession{
		Token: "tok-boundary", Patterns: []string{"/*"}, Expiry: atBoundary,
	}))
	require.NoError(t, st.SaveSession(ctx, StoredSession{
		Token: "tok-future", Patterns: []string{"/*"}, Expiry: future,
	}))

	deleted, err := st.DeleteExpiredSessions(ctx, now)
	require.NoError(t, err)
	assert.Equal(t, 1, deleted, "exactly the one strictly-past session should be flushed")

	loaded, err := st.LoadSessions(ctx)
	require.NoError(t, err)
	require.Len(t, loaded, 2)
	tokens := []string{loaded[0].Token, loaded[1].Token}
	assert.ElementsMatch(t, []string{"tok-boundary", "tok-future"}, tokens,
		"expiry == now must survive (Authorize uses time.Now().After, strict)")
}

func TestStore_Session_LoadCorruptedPatternsJSON(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := openTestStore(t)

	// Inject a row with bogus JSON bytes.
	_, err := st.db.ExecContext(ctx,
		`INSERT INTO sessions (token, patterns_json, expiry_unix) VALUES (?, ?, ?)`,
		"corrupt-tok", []byte("not-json"), time.Now().Add(time.Hour).Unix())
	require.NoError(t, err)

	_, err = st.LoadSessions(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal session patterns for \"corrupt-tok\"",
		"error must name the offending token for debuggability")
}

func TestStore_Denied_ReplaceAndLoad(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := openTestStore(t)

	require.NoError(t, st.ReplaceDeniedPatterns(ctx,
		[]string{"/spaces/dead/*", "/assets/x/*"}))

	loaded, err := st.LoadDeniedPatterns(ctx)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"/spaces/dead/*", "/assets/x/*"}, loaded)

	// Replace with a smaller set; old rows must be gone.
	require.NoError(t, st.ReplaceDeniedPatterns(ctx, []string{"/only/*"}))

	loaded, err = st.LoadDeniedPatterns(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"/only/*"}, loaded)
}

func TestStore_Denied_ReplaceWithEmptyClears(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := openTestStore(t)

	require.NoError(t, st.ReplaceDeniedPatterns(ctx, []string{"/p1/*"}))
	require.NoError(t, st.ReplaceDeniedPatterns(ctx, nil))

	loaded, err := st.LoadDeniedPatterns(ctx)
	require.NoError(t, err)
	assert.Empty(t, loaded)
}

func TestStore_Endpoints_ReplaceAndLoad(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := openTestStore(t)

	configs := []*agentv1.EndpointConfig{
		{
			Name: "organizations/acme/storageGateways/gw1/endpoints/media",
			Configuration: &agentv1.EndpointConfig_S3{
				S3: &agentv1.S3EndpointConfig{
					EndpointUri:     "https://s3.example.com",
					Bucket:          "media-bucket",
					Region:          "us-east-1",
					AccessKeyId:     "AKIA...",
					SecretAccessKey: "secret...",
				},
			},
			CacheConfig: &agentv1.EndpointCacheConfig{
				Enabled:        true,
				MaxSizeGb:      10,
				EvictionPolicy: "lru",
				TtlHours:       24,
			},
		},
		{
			Name: "organizations/acme/storageGateways/gw1/endpoints/local",
			Configuration: &agentv1.EndpointConfig_Filesystem{
				Filesystem: &agentv1.FileSystemEndpointConfig{
					Path: "/mnt/nfs/pivox-assets",
				},
			},
		},
	}

	require.NoError(t, st.ReplaceEndpoints(ctx, configs))

	loaded, err := st.LoadEndpoints(ctx)
	require.NoError(t, err)
	require.Len(t, loaded, 2)

	byName := map[string]*agentv1.EndpointConfig{}
	for _, c := range loaded {
		byName[c.GetName()] = c
	}
	require.Contains(t, byName, configs[0].GetName())
	require.Contains(t, byName, configs[1].GetName())

	media := byName[configs[0].GetName()]
	assert.Equal(t, "media-bucket", media.GetS3().GetBucket())
	assert.Equal(t, "us-east-1", media.GetS3().GetRegion())
	assert.True(t, media.GetCacheConfig().GetEnabled())

	local := byName[configs[1].GetName()]
	assert.Equal(t, "/mnt/nfs/pivox-assets", local.GetFilesystem().GetPath())
}

func TestStore_Endpoints_RejectsNilConfig(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := openTestStore(t)

	err := st.ReplaceEndpoints(ctx, []*agentv1.EndpointConfig{
		{Name: "endpoints/ok", Configuration: &agentv1.EndpointConfig_Filesystem{
			Filesystem: &agentv1.FileSystemEndpointConfig{Path: "/data"},
		}},
		nil,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "configs[1] is nil")

	// Existing rows must be untouched (tx rolled back).
	loaded, err := st.LoadEndpoints(ctx)
	require.NoError(t, err)
	assert.Empty(t, loaded, "rejected batch must not mutate existing endpoints")
}

func TestStore_Endpoints_RejectsEmptyName(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := openTestStore(t)

	err := st.ReplaceEndpoints(ctx, []*agentv1.EndpointConfig{
		{Name: "", Configuration: &agentv1.EndpointConfig_Filesystem{
			Filesystem: &agentv1.FileSystemEndpointConfig{Path: "/data"},
		}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty name")
}

func TestStore_Endpoints_LoadCorruptedProto(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := openTestStore(t)

	_, err := st.db.ExecContext(ctx,
		`INSERT INTO endpoints (name, config_proto) VALUES (?, ?)`,
		"endpoints/bad", []byte{0xff, 0xff, 0xff})
	require.NoError(t, err)

	_, err = st.LoadEndpoints(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal endpoint \"endpoints/bad\"")
}

// TestStore_RestartPersists is the load-bearing #79 acceptance criterion:
// open store → write → close → reopen → confirm everything is still there.
// This is what the agent does at boot before the controller is reachable.
func TestStore_RestartPersists(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	path := filepath.Join(t.TempDir(), "agent.db")
	expiry := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)

	// First run: write everything.
	{
		st, err := OpenStore(StoreConfig{Path: path})
		require.NoError(t, err)

		require.NoError(t, st.SaveSession(ctx, StoredSession{
			Token:    "persisted-tok",
			Patterns: []string{"/spaces/persist/*"},
			Expiry:   expiry,
		}))
		require.NoError(t, st.ReplaceDeniedPatterns(ctx,
			[]string{"/spaces/dead/*"}))
		require.NoError(t, st.ReplaceEndpoints(ctx, []*agentv1.EndpointConfig{
			{
				Name: "endpoints/media",
				Configuration: &agentv1.EndpointConfig_Filesystem{
					Filesystem: &agentv1.FileSystemEndpointConfig{Path: "/data"},
				},
			},
		}))

		require.NoError(t, st.Close())
	}

	// Second run: reopen the same file, verify state survived.
	st, err := OpenStore(StoreConfig{Path: path})
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	sessions, err := st.LoadSessions(ctx)
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	assert.Equal(t, "persisted-tok", sessions[0].Token)
	assert.Equal(t, []string{"/spaces/persist/*"}, sessions[0].Patterns)
	assert.True(t, sessions[0].Expiry.Equal(expiry))

	denied, err := st.LoadDeniedPatterns(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"/spaces/dead/*"}, denied)

	endpoints, err := st.LoadEndpoints(ctx)
	require.NoError(t, err)
	require.Len(t, endpoints, 1)
	assert.Equal(t, "endpoints/media", endpoints[0].GetName())
	assert.Equal(t, "/data", endpoints[0].GetFilesystem().GetPath())
}

func TestStore_ConcurrentSessionWrites(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := openTestStore(t)

	const n = 50
	expiry := time.Now().Add(1 * time.Hour).UTC().Truncate(time.Second)

	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- st.SaveSession(ctx, StoredSession{
				Token:    "concurrent-tok-" + strconv.Itoa(i),
				Patterns: []string{"/c/*"},
				Expiry:   expiry,
			})
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	loaded, err := st.LoadSessions(ctx)
	require.NoError(t, err)
	assert.Len(t, loaded, n,
		"all concurrent SaveSession writes should land without sqlite busy / lock errors")
}
