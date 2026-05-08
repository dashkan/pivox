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

	agentv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/agent/v1"
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

// testCache returns a small MemoryCache for tests that don't care
// about cache behavior, just need a non-nil instance to satisfy
// OpenAgentStateConfig.Cache / EndpointStoreConfig.Cache.
func testCache() *MemoryCache { return NewMemoryCache(10, 1024) }

func TestOpenAgentState_RequiresStateDir(t *testing.T) {
	t.Parallel()
	assert.PanicsWithValue(t,
		"storageagent: OpenAgentStateConfig.StateDir is required",
		func() {
			_ = OpenAgentState(context.Background(), OpenAgentStateConfig{
				Logger: silentLogger(),
				Cache:  testCache(),
			})
		})
}

func TestOpenAgentState_RequiresCache(t *testing.T) {
	t.Parallel()
	assert.PanicsWithValue(t,
		"storageagent: OpenAgentStateConfig.Cache is required",
		func() {
			_ = OpenAgentState(context.Background(), OpenAgentStateConfig{
				StateDir: t.TempDir(),
				Logger:   silentLogger(),
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
				Cache:    testCache(),
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
		Cache:    testCache(),
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
		Cache:    testCache(),
		Logger:   silentLogger(),
	})
	require.NotNil(t, state.Store)
	t.Cleanup(func() { _ = state.Store.Close() })

	st, err := os.Stat(missing)
	require.NoError(t, err)
	require.True(t, st.IsDir())
}

// TestOpenAgentState_RestartPreservesAllStores is the load-bearing
// #79 phase-5 acceptance test: persist a session, a denied pattern,
// AND an endpoint configuration; restart; all three must be effective
// from the local DB without controller round-trip.
func TestOpenAgentState_RestartPreservesAllStores(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	mediaDir := t.TempDir()
	const endpointName = "organizations/acme/storageGateways/gw/endpoints/media"

	// First boot — write to all three stores, close.
	{
		state := OpenAgentState(ctx, OpenAgentStateConfig{
			StateDir: dir,
			Cache:    testCache(),
			Logger:   silentLogger(),
		})
		require.NotNil(t, state.Store)

		require.NoError(t, state.Sessions.Grant(ctx, "boot-tok",
			[]string{"/spaces/persisted/*"},
			time.Now().Add(2*time.Hour)))
		require.NoError(t, state.Denied.Update(ctx,
			[]string{"/spaces/dead/*"}))
		require.NoError(t, state.Endpoints.Update(ctx,
			[]*agentv1.EndpointConfig{fsEndpoint(endpointName, mediaDir)}))

		require.NoError(t, state.Store.Close())
	}

	// Second boot — same dir, no controller touch. All three stores
	// must be effective from the local DB.
	state := OpenAgentState(ctx, OpenAgentStateConfig{
		StateDir: dir,
		Cache:    testCache(),
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

	// The endpoint short-name "media" must route from the in-memory
	// map without any controller round-trip.
	state.Endpoints.mu.RLock()
	defer state.Endpoints.mu.RUnlock()
	require.Contains(t, state.Endpoints.endpoints, "media",
		"persisted endpoint must reload after restart "+
			"(no controller round-trip required)")
	assert.Equal(t, mediaDir,
		state.Endpoints.endpoints["media"].config.GetFilesystem().GetPath())
}

// TestOpenAgentState_LoadEndpointsFails_KeepsStoreAttached covers the
// boot branch where OpenStore succeeds but EndpointStore.LoadFromStore
// fails. Symmetric to the Sessions and Denied versions: Store stays
// attached so subsequent Update calls can re-establish state from the
// controller's HandshakeAck/ConfigUpdate; the failure is logged at
// Error level.
func TestOpenAgentState_LoadEndpointsFails_KeepsStoreAttached(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()

	// Seed: open Store, write a row whose proto bytes are garbage so
	// that LoadEndpoints fails at proto.Unmarshal. Re-Open's
	// CREATE TABLE IF NOT EXISTS is a no-op (table already exists),
	// so the bad row survives.
	{
		seed, err := OpenStore(StoreConfig{Path: filepath.Join(dir, agentDBFilename)})
		require.NoError(t, err)
		_, err = seed.db.ExecContext(ctx,
			`INSERT INTO endpoints (name, config_proto) VALUES (?, ?)`,
			"corrupt-endpoint", []byte{0xff, 0xff, 0xff})
		require.NoError(t, err)
		require.NoError(t, seed.Close())
	}

	logger, buf := captureLogger(t)
	state := OpenAgentState(ctx, OpenAgentStateConfig{
		StateDir: dir,
		Cache:    testCache(),
		Logger:   logger,
	})
	require.NotNil(t, state.Endpoints)
	require.NotNil(t, state.Store,
		"Store must stay attached on Endpoints LoadFromStore failure")
	t.Cleanup(func() { _ = state.Store.Close() })

	out := buf.String()
	assert.Contains(t, out, "level=ERROR",
		"endpoints-load failure must log at Error level")
	assert.Contains(t, out, "reload endpoints",
		"log message must surface that the endpoints reload failed")

	// A fresh Update against the same Store must still succeed —
	// ReplaceEndpoints DELETEs every row before INSERTing the new
	// set, which clears the corrupt row that broke the reload.
	// Symmetric to TestOpenAgentState_LoadSessionsFails_KeepsStoreAttached's
	// closing Grant assertion.
	require.NoError(t, state.Endpoints.Update(ctx, []*agentv1.EndpointConfig{
		fsEndpoint("organizations/acme/storageGateways/gw/endpoints/fresh", t.TempDir()),
	}), "Update must work against an attached Store after LoadFromStore failed")
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
		Cache:    testCache(),
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
		Cache:    testCache(),
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
		Cache:    testCache(),
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
