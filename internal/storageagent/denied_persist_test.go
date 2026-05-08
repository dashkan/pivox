package storageagent

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// openDeniedWithPersist returns a DeniedPatterns wired through a fresh
// Store at a tempdir path. Returns the path so tests can simulate
// agent restart by closing and re-opening against the same file.
func openDeniedWithPersist(t *testing.T) (*DeniedPatterns, *Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent.db")
	st, err := OpenStore(StoreConfig{Path: path})
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	denied := NewDeniedPatterns(DeniedPatternsConfig{Store: st})
	return denied, st, path
}

func TestDeniedPatterns_Update_PersistsToStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	denied, store, _ := openDeniedWithPersist(t)
	require.NoError(t, denied.Update(ctx,
		[]string{"/spaces/dead/*", "/assets/x/*"}))

	// In-memory effective.
	assert.True(t, denied.IsDenied("/spaces/dead/file.mp4"))
	assert.True(t, denied.IsDenied("/assets/x/y"))
	assert.False(t, denied.IsDenied("/safe/file"))

	// And persisted.
	rows, err := store.LoadDeniedPatterns(ctx)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"/spaces/dead/*", "/assets/x/*"}, rows)
}

func TestDeniedPatterns_Update_AtomicOnPersistFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	st, err := OpenStore(StoreConfig{Path: filepath.Join(t.TempDir(), "agent.db")})
	require.NoError(t, err)
	denied := NewDeniedPatterns(DeniedPatternsConfig{Store: st})

	// Seed an initial set.
	require.NoError(t, denied.Update(ctx, []string{"/initial/*"}))
	require.True(t, denied.IsDenied("/initial/file"))

	// Close the Store to force the next persist to fail.
	require.NoError(t, st.Close())

	// Update must surface the persist failure AND leave in-memory
	// state untouched (matches SessionStore.Grant atomicity).
	err = denied.Update(ctx, []string{"/should/not/take/effect/*"})
	require.Error(t, err)
	// Tighten the contract: the error must wrap the underlying
	// "database is closed" condition. Keeps the test honest if the
	// driver ever changes how it reports closed-DB errors.
	assert.Contains(t, err.Error(), "update denied patterns",
		"error must be wrapped at the storeagent layer for caller debuggability")
	assert.Contains(t, err.Error(), "closed",
		"error must surface the underlying SQLite-closed condition")

	assert.True(t, denied.IsDenied("/initial/file"),
		"failed Update must NOT have replaced the in-memory set "+
			"(in-memory and disk would otherwise diverge silently)")
	assert.False(t, denied.IsDenied("/should/not/take/effect/x"),
		"failed Update must NOT have applied the new patterns")
}

// TestDeniedPatterns_LoadFromStore_RestartsEnforcement is the
// load-bearing #79 acceptance criterion at the DeniedPatterns layer:
// after process restart, denied patterns from the local DB must still
// reject requests, even if the controller is unreachable.
func TestDeniedPatterns_LoadFromStore_RestartsEnforcement(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "agent.db")

	// First "process": persist a denied set and close.
	{
		st, err := OpenStore(StoreConfig{Path: path})
		require.NoError(t, err)
		denied := NewDeniedPatterns(DeniedPatternsConfig{Store: st})
		require.NoError(t, denied.Update(ctx,
			[]string{"/spaces/dead/*", "/private/secrets/*"}))
		require.NoError(t, st.Close())
	}

	// Second "process": fresh DeniedPatterns + same store + LoadFromStore.
	st, err := OpenStore(StoreConfig{Path: path})
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	denied := NewDeniedPatterns(DeniedPatternsConfig{Store: st})
	require.NoError(t, denied.LoadFromStore(ctx))

	assert.True(t, denied.IsDenied("/spaces/dead/file.mp4"),
		"persisted denied pattern must still reject after restart "+
			"(no controller round-trip required)")
	assert.True(t, denied.IsDenied("/private/secrets/key"),
		"every persisted pattern must reload, not just the first")
	assert.False(t, denied.IsDenied("/spaces/alive/file.mp4"),
		"unrelated paths must not be rejected")
}

func TestDeniedPatterns_NoStoreAttached(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	denied := NewDeniedPatterns(DeniedPatternsConfig{})
	require.NoError(t, denied.Update(ctx, []string{"/m/*"}))
	assert.True(t, denied.IsDenied("/m/file"))

	// LoadFromStore is a no-op without a store.
	require.NoError(t, denied.LoadFromStore(ctx))
}

func TestDeniedPatterns_Update_EmptyClears(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	denied, store, _ := openDeniedWithPersist(t)
	require.NoError(t, denied.Update(ctx, []string{"/p1/*"}))
	require.True(t, denied.IsDenied("/p1/x"))

	require.NoError(t, denied.Update(ctx, nil))
	assert.False(t, denied.IsDenied("/p1/x"),
		"nil/empty Update must clear the in-memory set")

	rows, err := store.LoadDeniedPatterns(ctx)
	require.NoError(t, err)
	assert.Empty(t, rows, "nil/empty Update must clear the persisted set too")
}
