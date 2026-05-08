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

func TestOpenSessionState_RequiresStateDir(t *testing.T) {
	t.Parallel()
	assert.PanicsWithValue(t,
		"storageagent: OpenSessionStateConfig.StateDir is required",
		func() {
			_, _ = OpenSessionState(context.Background(), OpenSessionStateConfig{
				Logger: silentLogger(),
			})
		})
}

func TestOpenSessionState_RequiresLogger(t *testing.T) {
	t.Parallel()
	assert.PanicsWithValue(t,
		"storageagent: OpenSessionStateConfig.Logger is required",
		func() {
			_, _ = OpenSessionState(context.Background(), OpenSessionStateConfig{
				StateDir: t.TempDir(),
			})
		})
}

// TestOpenSessionState_FreshDir_OK is the happy-path boot: a fresh
// state dir, no pre-existing agent.db, returns a Store-attached
// SessionStore and the Store handle (caller closes).
func TestOpenSessionState_FreshDir_OK(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sessions, store := OpenSessionState(context.Background(), OpenSessionStateConfig{
		StateDir: dir,
		Logger:   silentLogger(),
	})
	require.NotNil(t, sessions)
	require.NotNil(t, store, "fresh dir should produce a non-nil Store")
	t.Cleanup(func() { _ = store.Close() })

	// Disk file actually appeared.
	_, err := os.Stat(filepath.Join(dir, "agent.db"))
	require.NoError(t, err, "agent.db should exist after OpenSessionState")
}

// TestOpenSessionState_CreatesMissingDir asserts that a non-existent
// state dir is created (not an error). Operators set --state-dir to a
// path that may not exist on first run.
func TestOpenSessionState_CreatesMissingDir(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "nested", "state")
	sessions, store := OpenSessionState(context.Background(), OpenSessionStateConfig{
		StateDir: missing,
		Logger:   silentLogger(),
	})
	require.NotNil(t, sessions)
	require.NotNil(t, store)
	t.Cleanup(func() { _ = store.Close() })

	st, err := os.Stat(missing)
	require.NoError(t, err)
	require.True(t, st.IsDir())
}

// TestOpenSessionState_RestartPreservesGrants is the load-bearing
// phase 3 acceptance test: grant a session, close everything,
// re-open, the session must authorize without a re-grant.
func TestOpenSessionState_RestartPreservesGrants(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()

	// First boot — grant and close.
	{
		sessions, store := OpenSessionState(ctx, OpenSessionStateConfig{
			StateDir: dir,
			Logger:   silentLogger(),
		})
		require.NotNil(t, store)
		require.NoError(t, sessions.Grant(ctx, "boot-tok",
			[]string{"/spaces/persisted/*"},
			time.Now().Add(2*time.Hour)))
		require.NoError(t, store.Close())
	}

	// Second boot — same dir, no controller, must authorize.
	sessions, store := OpenSessionState(ctx, OpenSessionStateConfig{
		StateDir: dir,
		Logger:   silentLogger(),
	})
	require.NotNil(t, store)
	t.Cleanup(func() { _ = store.Close() })

	assert.True(t, sessions.Authorize("boot-tok", "/spaces/persisted/file.mp4"),
		"session granted before restart must authorize on second boot "+
			"without controller round-trip")
}

// TestOpenSessionState_LoadFails_KeepsStoreAttached covers the boot
// branch where OpenStore succeeds but LoadFromStore fails (e.g. a
// previously-persisted row is corrupt). The contract: Store stays
// attached so subsequent grants persist, the failure is logged at
// Error level, and the agent still serves (controller will redeliver
// active sessions on reconnect).
func TestOpenSessionState_LoadFails_KeepsStoreAttached(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()

	// Pre-seed the DB with a corrupt session row so that LoadFromStore
	// fails on iteration. Use the lower-level Store API to inject the
	// bad bytes, then close.
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
	sessions, store := OpenSessionState(ctx, OpenSessionStateConfig{
		StateDir: dir,
		Logger:   logger,
	})
	require.NotNil(t, sessions)
	require.NotNil(t, store,
		"Store must stay attached on LoadFromStore failure so subsequent grants persist")
	t.Cleanup(func() { _ = store.Close() })

	// Loud Error log surfaces the reload failure.
	out := buf.String()
	assert.Contains(t, out, "level=ERROR",
		"LoadFromStore failure must log at Error level")
	assert.Contains(t, out, "reload sessions",
		"log message must surface the operation that failed")

	// Subsequent Grant must still succeed — the Store handle is alive
	// and SaveSession works regardless of LoadFromStore's outcome.
	require.NoError(t, sessions.Grant(ctx, "fresh-tok",
		[]string{"/f/*"}, time.Now().Add(time.Hour)),
		"Grant must work against an attached Store after LoadFromStore failed")
	assert.True(t, sessions.Authorize("fresh-tok", "/f/file"),
		"newly granted session must authorize")
}

// TestOpenSessionState_BadDir_FallsBackToInMemory verifies the
// log-and-continue contract: if MkdirAll fails (e.g. dir path points
// to an existing file), the agent boots with an in-memory-only
// SessionStore and a nil Store, having logged at Error level.
func TestOpenSessionState_BadDir_FallsBackToInMemory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Create a regular file at the path we'll pass as StateDir.
	// MkdirAll on a path whose parent is a file fails with ENOTDIR.
	tmp := t.TempDir()
	blockingFile := filepath.Join(tmp, "not-a-dir")
	require.NoError(t, os.WriteFile(blockingFile, []byte("x"), 0o644))
	stateDir := filepath.Join(blockingFile, "state")

	logger, buf := captureLogger(t)
	sessions, store := OpenSessionState(ctx, OpenSessionStateConfig{
		StateDir: stateDir,
		Logger:   logger,
	})
	require.NotNil(t, sessions, "must return a usable in-memory SessionStore even on dir failure")
	assert.Nil(t, store, "Store must be nil when state dir cannot be created")

	// In-memory grant still works (no Store attached → no persist call).
	require.NoError(t, sessions.Grant(ctx, "in-mem",
		[]string{"/m/*"}, time.Now().Add(time.Hour)))
	assert.True(t, sessions.Authorize("in-mem", "/m/file"))

	// Boot block logged the failure loud.
	out := buf.String()
	assert.Contains(t, out, "level=ERROR",
		"state-dir failure must log at Error level")
	assert.Contains(t, out, "in-memory only",
		"log message must surface the fallback explicitly")
}
