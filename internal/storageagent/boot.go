package storageagent

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
)

// agentDBFilename is the on-disk name of the agent state database
// inside the configured state dir. Single-file SQLite; WAL files are
// adjacent (-wal, -shm).
const agentDBFilename = "agent.db"

// OpenAgentStateConfig is the constructor input for OpenAgentState.
type OpenAgentStateConfig struct {
	// StateDir is the filesystem path that holds the agent's local
	// state DB (sessions, denied patterns, and endpoints). Created if
	// it does not exist. Required.
	//
	// IMPORTANT: this should NOT be the cache directory. Operators
	// commonly run cache-cleanup scripts (`rm -rf <cache-dir>/*`),
	// and state living inside the cache dir would be wiped along with
	// the cached blobs. Use a distinct directory (the agent's CLI
	// defaults state-dir = /var/lib/pivox/state, sibling of the cache).
	StateDir string

	// Cache is the in-memory blob cache used by S3-backed endpoints.
	// Required — passed through to the constructed EndpointStore.
	Cache *MemoryCache

	// Logger receives boot-time errors. Required.
	Logger *slog.Logger
}

// AgentState bundles the agent's in-memory stores along with the
// underlying *Store handle that all writes flow through.
//
// Sessions, Denied, and Endpoints are always non-nil — they are
// constructed in every code path, including the in-memory fallback.
// Store is nil only on the log-and-continue failure path (state-dir
// or OpenStore failure); Close on the nil Store is a no-op.
type AgentState struct {
	// Sessions is the agent's session store, wired through Store
	// when non-nil. Always non-nil.
	Sessions *SessionStore

	// Denied is the agent's denied-patterns set, wired through Store
	// when non-nil. Always non-nil.
	Denied *DeniedPatterns

	// Endpoints is the agent's endpoint routing map, wired through
	// Store when non-nil. Always non-nil.
	Endpoints *EndpointStore

	// Store is the underlying SQLite handle. May be nil if the state
	// DB could not be opened — the caller's `defer state.Store.Close()`
	// is still safe because (*Store)(nil).Close() is a no-op.
	Store *Store
}

// OpenAgentState opens the agent's local state DB at
// <StateDir>/agent.db and constructs the in-memory stores with
// write-through persistence, reloading any persisted state from disk.
//
// Failure mode: log-and-continue. If MkdirAll or OpenStore fails, the
// function logs at slog.Error and returns in-memory-only stores (with
// AgentState.Store == nil). The controller is the source of truth and
// re-delivers active sessions / denied patterns on the next reconnect
// handshake — refusing to start the agent on local-state failure
// would be strictly worse for availability. The operator loses crash
// resilience until the state-dir issue is fixed; the loud Error log
// is the signal to act.
//
// LoadFromStore failures (per-store) are also log-and-continue: the
// Store handle stays attached so subsequent writes can still persist;
// only the reload of prior state was lost.
//
// Caller responsibilities:
//
//   - Close AgentState.Store on shutdown. Store may be nil; the
//     handle's Close is nil-safe ((*Store)(nil).Close() is a no-op),
//     so a plain `defer state.Store.Close()` is fine.
//   - Use AgentState.Sessions and AgentState.Denied as the agent's
//     in-memory stores; they are fully usable in either mode
//     (in-memory-only or persistent).
func OpenAgentState(ctx context.Context, cfg OpenAgentStateConfig) AgentState {
	if cfg.StateDir == "" {
		panic("storageagent: OpenAgentStateConfig.StateDir is required")
	}
	if cfg.Cache == nil {
		panic("storageagent: OpenAgentStateConfig.Cache is required")
	}
	if cfg.Logger == nil {
		panic("storageagent: OpenAgentStateConfig.Logger is required")
	}

	inMemoryFallback := func() AgentState {
		return AgentState{
			Sessions:  NewSessionStore(SessionStoreConfig{}),
			Denied:    NewDeniedPatterns(DeniedPatternsConfig{}),
			Endpoints: NewEndpointStore(EndpointStoreConfig{Cache: cfg.Cache}),
		}
	}

	if err := os.MkdirAll(cfg.StateDir, 0o755); err != nil {
		cfg.Logger.Error(
			"create agent state dir; falling back to in-memory only (no crash resilience)",
			"dir", cfg.StateDir, "error", err)
		return inMemoryFallback()
	}

	dbPath := filepath.Join(cfg.StateDir, agentDBFilename)
	store, err := OpenStore(StoreConfig{Path: dbPath})
	if err != nil {
		cfg.Logger.Error(
			"open agent state DB; falling back to in-memory only (no crash resilience)",
			"path", dbPath, "error", err)
		return inMemoryFallback()
	}

	sessions := NewSessionStore(SessionStoreConfig{Store: store})
	if err := sessions.LoadFromStore(ctx); err != nil {
		// Keep the Store attached: subsequent grants can still persist.
		// We just couldn't reload prior state — the controller will
		// redeliver active sessions on reconnect.
		cfg.Logger.Error(
			"reload sessions from agent state DB; serving with empty in-memory sessions",
			"path", dbPath, "error", err)
	}

	denied := NewDeniedPatterns(DeniedPatternsConfig{Store: store})
	if err := denied.LoadFromStore(ctx); err != nil {
		// Same shape as sessions: keep Store attached, log loud, the
		// controller resends the full denied set on reconnect.
		cfg.Logger.Error(
			"reload denied patterns from agent state DB; serving with empty in-memory denied set",
			"path", dbPath, "error", err)
	}

	endpoints := NewEndpointStore(EndpointStoreConfig{
		Cache: cfg.Cache,
		Store: store,
	})
	if err := endpoints.LoadFromStore(ctx); err != nil {
		// Same shape as sessions/denied: keep Store attached, log
		// loud. EndpointStore.LoadFromStore can also fail mid-load
		// on per-endpoint S3 client construction (unreachable
		// backend), in which case the agent boots with no endpoints
		// in memory and the controller's HandshakeAck/ConfigUpdate
		// re-delivers a fresh set.
		cfg.Logger.Error(
			"reload endpoints from agent state DB; serving with empty in-memory endpoints",
			"path", dbPath, "error", err)
	}

	return AgentState{
		Sessions:  sessions,
		Denied:    denied,
		Endpoints: endpoints,
		Store:     store,
	}
}
