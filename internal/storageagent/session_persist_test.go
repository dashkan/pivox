package storageagent

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// openSessionStoreWithPersist returns a SessionStore wired through a
// fresh SQLite Store at a tempdir path. The path is also returned so
// callers can simulate an agent restart by closing the first store
// and opening a new one against the same path.
func openSessionStoreWithPersist(t *testing.T) (*SessionStore, *Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent.db")
	st, err := OpenStore(StoreConfig{Path: path})
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	sessions := NewSessionStore(SessionStoreConfig{Store: st})
	return sessions, st, path
}

func TestSessionStore_Grant_PersistsToStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	sessions, store, _ := openSessionStoreWithPersist(t)
	expiry := time.Now().Add(1 * time.Hour).UTC().Truncate(time.Second)

	require.NoError(t, sessions.Grant(ctx, "tok-1",
		[]string{"/spaces/a/*"}, expiry))

	// In-memory effective.
	assert.True(t, sessions.Authorize("tok-1", "/spaces/a/file.mp4"))

	// And persisted.
	rows, err := store.LoadSessions(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "tok-1", rows[0].Token)
	assert.Equal(t, []string{"/spaces/a/*"}, rows[0].Patterns)
	assert.True(t, rows[0].Expiry.Equal(expiry))
}

func TestSessionStore_Revoke_RemovesFromStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	sessions, store, _ := openSessionStoreWithPersist(t)
	expiry := time.Now().Add(1 * time.Hour).UTC().Truncate(time.Second)

	require.NoError(t, sessions.Grant(ctx, "tok-keep",
		[]string{"/k/*"}, expiry))
	require.NoError(t, sessions.Grant(ctx, "tok-drop",
		[]string{"/d/*"}, expiry))

	require.NoError(t, sessions.Revoke(ctx, "tok-drop"))

	rows, err := store.LoadSessions(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "tok-keep", rows[0].Token)
}

func TestSessionStore_FlushExpired_RemovesFromStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	sessions, store, _ := openSessionStoreWithPersist(t)

	require.NoError(t, sessions.Grant(ctx, "tok-past",
		[]string{"/*"}, time.Now().Add(-1*time.Hour)))
	require.NoError(t, sessions.Grant(ctx, "tok-future",
		[]string{"/*"}, time.Now().Add(1*time.Hour)))

	require.NoError(t, sessions.FlushExpired(ctx))

	rows, err := store.LoadSessions(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "tok-future", rows[0].Token)
}

// TestSessionStore_LoadFromStore_RestartsAuthorize is the load-bearing
// #79 acceptance criterion at the SessionStore layer: an existing
// valid session in SQLite must authorize after an agent restart, with
// no controller round-trip.
func TestSessionStore_LoadFromStore_RestartsAuthorize(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	path := filepath.Join(t.TempDir(), "agent.db")
	expiry := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)

	// First "process": grant a session, close everything.
	{
		st, err := OpenStore(StoreConfig{Path: path})
		require.NoError(t, err)

		sessions := NewSessionStore(SessionStoreConfig{Store: st})

		require.NoError(t, sessions.Grant(ctx, "persisted-tok",
			[]string{"/spaces/persist/*"}, expiry))
		require.NoError(t, st.Close())
	}

	// Second "process": fresh SessionStore, attach the same SQLite,
	// LoadFromStore. The previously granted token must authorize again
	// without anyone calling Grant.
	st, err := OpenStore(StoreConfig{Path: path})
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	sessions := NewSessionStore(SessionStoreConfig{Store: st})
	require.NoError(t, sessions.LoadFromStore(ctx))

	assert.True(t, sessions.Authorize("persisted-tok",
		"/spaces/persist/file.mp4"),
		"previously persisted session must authorize after restart")
}

// TestSessionStore_LoadFromStore_FlushesExpiredOnBoot ensures the
// boot-time reload doesn't bring back already-expired rows. Without
// this, an agent that's been off for hours would re-load and then
// have to wait one cleanup tick before refusing dead tokens.
func TestSessionStore_LoadFromStore_FlushesExpiredOnBoot(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	path := filepath.Join(t.TempDir(), "agent.db")

	// Seed an expired and a valid row directly via Store.
	{
		st, err := OpenStore(StoreConfig{Path: path})
		require.NoError(t, err)
		require.NoError(t, st.SaveSession(ctx, StoredSession{
			Token: "stale", Patterns: []string{"/*"},
			Expiry: time.Now().Add(-1 * time.Hour),
		}))
		require.NoError(t, st.SaveSession(ctx, StoredSession{
			Token: "fresh", Patterns: []string{"/*"},
			Expiry: time.Now().Add(1 * time.Hour),
		}))
		require.NoError(t, st.Close())
	}

	st, err := OpenStore(StoreConfig{Path: path})
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	sessions := NewSessionStore(SessionStoreConfig{Store: st})
	require.NoError(t, sessions.LoadFromStore(ctx))

	assert.False(t, sessions.Authorize("stale", "/anything"),
		"already-expired session must not authorize after boot reload")
	assert.True(t, sessions.Authorize("fresh", "/anything"),
		"non-expired session must authorize after boot reload")

	// The stale row must also be physically gone from the store, so it
	// doesn't get reloaded on a second restart (cumulative buildup).
	rows, err := st.LoadSessions(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "fresh", rows[0].Token)
}

// TestSessionStore_Grant_PersistFailure_LeavesInMemoryUnchanged is the
// load-bearing assertion behind the "atomic with persistence" doc on
// SessionStore: if the SQLite write fails, the in-memory map MUST NOT
// be updated, and the caller MUST receive an error. Simulated by
// closing the underlying Store before calling Grant — modernc.org/sqlite
// returns "sql: database is closed" once the handle is closed.
func TestSessionStore_Grant_PersistFailure_LeavesInMemoryUnchanged(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	st, err := OpenStore(StoreConfig{Path: filepath.Join(t.TempDir(), "agent.db")})
	require.NoError(t, err)

	sessions := NewSessionStore(SessionStoreConfig{Store: st})

	// Close the store to force every subsequent persist call to fail.
	require.NoError(t, st.Close())

	err = sessions.Grant(ctx, "tok-doomed",
		[]string{"/anywhere/*"}, time.Now().Add(time.Hour))
	require.Error(t, err, "Grant must surface persistence failures")

	assert.False(t, sessions.Authorize("tok-doomed", "/anywhere/x"),
		"failed Grant must NOT have populated the in-memory map "+
			"(otherwise in-memory and disk diverge silently)")
}

// TestSessionStore_NoStoreAttached preserves the in-memory-only path
// for tests and small integrations that don't need persistence.
func TestSessionStore_NoStoreAttached(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	sessions := NewSessionStore(SessionStoreConfig{})
	expiry := time.Now().Add(time.Hour)

	require.NoError(t, sessions.Grant(ctx, "tok-mem",
		[]string{"/m/*"}, expiry))
	assert.True(t, sessions.Authorize("tok-mem", "/m/x"))

	require.NoError(t, sessions.Revoke(ctx, "tok-mem"))
	assert.False(t, sessions.Authorize("tok-mem", "/m/x"))

	require.NoError(t, sessions.FlushExpired(ctx))

	// LoadFromStore is a no-op without an attached store.
	require.NoError(t, sessions.LoadFromStore(ctx))
}
