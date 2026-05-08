package storageagent

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// silentLogger returns a slog logger that discards everything. Used by
// tests that don't want to assert on log output.
func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// captureLogger returns a slog logger that writes to the returned
// buffer. Tests assert on the captured text to verify the boot block
// logged the right message at the right level.
func captureLogger(t *testing.T) (*slog.Logger, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})), &buf
}

func TestOpenAgentState_RequiresStateDir(t *testing.T) {
	t.Parallel()
	assert.PanicsWithValue(t,
		"storageagent: OpenAgentStateConfig.StateDir is required",
		func() {
			_ = OpenAgentState(context.Background(), OpenAgentStateConfig{
				Logger: silentLogger(),
			})
		})
}

func TestOpenAgentState_RequiresLogger(t *testing.T) {
	t.Parallel()
	assert.PanicsWithValue(t,
		"storageagent: OpenAgentStateConfig.Logger is required",
		func() {
			_ = OpenAgentState(context.Background(), OpenAgentStateConfig{
				StateDir: t.TempDir(),
			})
		})
}

// TestOpenAgentState_FreshDir_OK is the happy-path boot: a fresh state
// dir, no pre-existing agent.db. All three references in the returned
// AgentState are non-nil.
func TestOpenAgentState_FreshDir_OK(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	state := OpenAgentState(context.Background(), OpenAgentStateConfig{
		StateDir: dir,
		Logger:   silentLogger(),
	})
	require.NotNil(t, state.Sessions)
	require.NotNil(t, state.Denied)
	require.NotNil(t, state.Store, "fresh dir should produce a non-nil Store")
	t.Cleanup(func() { _ = state.Store.Close() })

	// Disk file actually appeared.
	_, err := os.Stat(filepath.Join(dir, "agent.db"))
	require.NoError(t, err, "agent.db should exist after OpenAgentState")
}

// TestOpenAgentState_CreatesMissingDir asserts that a non-existent
// state dir is created, not an error.
func TestOpenAgentState_CreatesMissingDir(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "nested", "state")
	state := OpenAgentState(context.Background(), OpenAgentStateConfig{
		StateDir: missing,
		Logger:   silentLogger(),
	})
	require.NotNil(t, state.Store)
	t.Cleanup(func() { _ = state.Store.Close() })

	st, err := os.Stat(missing)
	require.NoError(t, err)
	require.True(t, st.IsDir())
}

// TestOpenAgentState_RestartPreservesGrantsAndDenied is the
// load-bearing #79 acceptance test for phase 4: persist a session AND
// a denied set, restart, both must be effective without controller
// round-trip.
func TestOpenAgentState_RestartPreservesGrantsAndDenied(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()

	// First boot — grant a session, set a denied pattern, close.
	{
		state := OpenAgentState(ctx, OpenAgentStateConfig{
			StateDir: dir,
			Logger:   silentLogger(),
		})
		require.NotNil(t, state.Store)

		require.NoError(t, state.Sessions.Grant(ctx, "boot-tok",
			[]string{"/spaces/persisted/*"},
			time.Now().Add(2*time.Hour)))
		require.NoError(t, state.Denied.Update(ctx,
			[]string{"/spaces/dead/*"}))

		require.NoError(t, state.Store.Close())
	}

	// Second boot — same dir, no controller touch. Both stores must
	// be effective from the local DB.
	state := OpenAgentState(ctx, OpenAgentStateConfig{
		StateDir: dir,
		Logger:   silentLogger(),
	})
	require.NotNil(t, state.Store)
	t.Cleanup(func() { _ = state.Store.Close() })

	assert.True(t, state.Sessions.Authorize("boot-tok", "/spaces/persisted/file.mp4"),
		"session granted before restart must authorize on second boot")
	assert.True(t, state.Denied.IsDenied("/spaces/dead/file.mp4"),
		"denied pattern set before restart must reject on second boot")
	assert.False(t, state.Denied.IsDenied("/spaces/alive/file.mp4"),
		"unrelated path must not be rejected")
}

// TestOpenAgentState_LoadDeniedFails_KeepsStoreAttached covers the
// boot branch where OpenStore succeeds but DeniedPatterns.LoadFromStore
// fails. The contract: Store stays attached (non-nil), the failure is
// logged at Error level, and the agent boots regardless. Symmetry
// note vs the LoadSessionsFails test: the corruption used here (column
// rename) breaks Update too, so we cannot assert "Update still
// persists" the way the Sessions test does — the Sessions test
// corrupts a single row's JSON, leaving the column intact for fresh
// inserts. The boot.go code path is structurally identical for the
// two stores; this test exercises the denied branch end-to-end.
func TestOpenAgentState_LoadDeniedFails_KeepsStoreAttached(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()

	// Seed the DB and rename the `pattern` column out from under the
	// `LoadDeniedPatterns` SELECT. CREATE TABLE IF NOT EXISTS in
	// subsequent OpenStore is a no-op (table already exists), so the
	// rename survives the reopen and SELECT pattern fails with
	// "no such column".
	{
		seed, err := OpenStore(StoreConfig{Path: filepath.Join(dir, agentDBFilename)})
		require.NoError(t, err)
		_, err = seed.db.ExecContext(ctx,
			`ALTER TABLE denied_patterns RENAME COLUMN pattern TO pattern_renamed`)
		require.NoError(t, err)
		require.NoError(t, seed.Close())
	}

	logger, buf := captureLogger(t)
	state := OpenAgentState(ctx, OpenAgentStateConfig{
		StateDir: dir,
		Logger:   logger,
	})
	require.NotNil(t, state.Denied)
	require.NotNil(t, state.Store,
		"Store must stay attached on Denied LoadFromStore failure")
	t.Cleanup(func() { _ = state.Store.Close() })

	out := buf.String()
	assert.Contains(t, out, "level=ERROR",
		"denied-load failure must log at Error level")
	assert.Contains(t, out, "reload denied patterns",
		"log message must surface that the denied-patterns reload failed")
}

// TestOpenAgentState_LoadSessionsFails_KeepsStoreAttached covers the
// boot branch where OpenStore succeeds but SessionStore.LoadFromStore
// fails (corrupt JSON in patterns_json). Store stays attached so
// subsequent Grant calls persist; the failure is logged at Error level.
func TestOpenAgentState_LoadSessionsFails_KeepsStoreAttached(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()

	// Seed a corrupt session row.
	{
		seed, err := OpenStore(StoreConfig{Path: filepath.Join(dir, agentDBFilename)})
		require.NoError(t, err)
		_, err = seed.db.ExecContext(ctx,
			`INSERT INTO sessions (token, patterns_json, expiry_unix) VALUES (?, ?, ?)`,
			"corrupt-tok", []byte("not-json"), time.Now().Add(time.Hour).Unix())
		require.NoError(t, err)
		require.NoError(t, seed.Close())
	}

	logger, buf := captureLogger(t)
	state := OpenAgentState(ctx, OpenAgentStateConfig{
		StateDir: dir,
		Logger:   logger,
	})
	require.NotNil(t, state.Sessions)
	require.NotNil(t, state.Store,
		"Store must stay attached on LoadFromStore failure so subsequent grants persist")
	t.Cleanup(func() { _ = state.Store.Close() })

	out := buf.String()
	assert.Contains(t, out, "level=ERROR")
	assert.Contains(t, out, "reload sessions",
		"log message must surface the operation that failed")

	require.NoError(t, state.Sessions.Grant(ctx, "fresh-tok",
		[]string{"/f/*"}, time.Now().Add(time.Hour)),
		"Grant must work against an attached Store after LoadFromStore failed")
	assert.True(t, state.Sessions.Authorize("fresh-tok", "/f/file"))
}

// TestOpenAgentState_BadDir_FallsBackToInMemory verifies the
// log-and-continue contract: if MkdirAll fails (path points to an
// existing file), AgentState.Store is nil and the in-memory stores
// remain usable; an Error is logged.
func TestOpenAgentState_BadDir_FallsBackToInMemory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tmp := t.TempDir()
	blockingFile := filepath.Join(tmp, "not-a-dir")
	require.NoError(t, os.WriteFile(blockingFile, []byte("x"), 0o644))
	stateDir := filepath.Join(blockingFile, "state")

	logger, buf := captureLogger(t)
	state := OpenAgentState(ctx, OpenAgentStateConfig{
		StateDir: stateDir,
		Logger:   logger,
	})
	require.NotNil(t, state.Sessions, "must return a usable in-memory SessionStore")
	require.NotNil(t, state.Denied, "must return a usable in-memory DeniedPatterns")
	assert.Nil(t, state.Store, "Store must be nil on state-dir failure")

	// In-memory writes still work (no Store attached → no persist call).
	require.NoError(t, state.Sessions.Grant(ctx, "in-mem",
		[]string{"/m/*"}, time.Now().Add(time.Hour)))
	assert.True(t, state.Sessions.Authorize("in-mem", "/m/file"))

	require.NoError(t, state.Denied.Update(ctx, []string{"/d/*"}))
	assert.True(t, state.Denied.IsDenied("/d/x"))

	// Boot block logged the failure loud.
	out := buf.String()
	assert.Contains(t, out, "level=ERROR",
		"state-dir failure must log at Error level")
	assert.Contains(t, out, "in-memory only",
		"log message must surface the fallback explicitly")

	// Belt-and-suspenders nil Close.
	assert.NotPanics(t, func() { _ = state.Store.Close() },
		"(*Store)(nil).Close() must be a no-op")
}
