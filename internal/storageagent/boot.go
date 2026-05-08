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

// OpenSessionStateConfig is the constructor input for OpenSessionState.
type OpenSessionStateConfig struct {
	// StateDir is the filesystem path that holds the agent's local
	// state DB (and, in later phases of #79, denied patterns and
	// endpoints). Created if it does not exist. Required.
	//
	// IMPORTANT: this should NOT be the cache directory. Operators
	// commonly run cache-cleanup scripts (`rm -rf <cache-dir>/*`),
	// and state living inside the cache dir would be wiped along with
	// the cached blobs. Use a distinct directory (the agent's CLI
	// defaults state-dir = /var/lib/pivox/state, sibling of the cache).
	StateDir string

	// Logger receives boot-time errors. Required.
	Logger *slog.Logger
}

// OpenSessionState opens the agent's local state DB at
// <StateDir>/agent.db, constructs a SessionStore with write-through
// persistence, and reloads any persisted sessions from disk.
//
// Failure mode: log-and-continue. If MkdirAll, OpenStore, or
// LoadFromStore fails, the function logs at slog.Error and returns an
// in-memory-only SessionStore (with a nil Store). The controller is
// the source of truth for active sessions and re-delivers them on
// reconnect handshake — refusing to start the agent on local-state
// failure would be strictly worse for availability. The trade-off is
// that the operator loses crash resilience until the state-dir issue
// is fixed; the loud Error log is the signal to act.
//
// Caller responsibilities:
//
//   - Close the returned *Store on shutdown. Store may be nil; the
//     returned Store handle's Close is nil-safe ((*Store)(nil).Close()
//     is a no-op), so a plain `defer store.Close()` is fine — an
//     explicit nil-check is belt-and-suspenders, not required.
//   - Use the returned *SessionStore as the agent's session store; it
//     is fully usable in either mode (in-memory-only or persistent).
func OpenSessionState(ctx context.Context, cfg OpenSessionStateConfig) (*SessionStore, *Store) {
	if cfg.StateDir == "" {
		panic("storageagent: OpenSessionStateConfig.StateDir is required")
	}
	if cfg.Logger == nil {
		panic("storageagent: OpenSessionStateConfig.Logger is required")
	}

	if err := os.MkdirAll(cfg.StateDir, 0o755); err != nil {
		cfg.Logger.Error(
			"create agent state dir; falling back to in-memory only (no crash resilience)",
			"dir", cfg.StateDir, "error", err)
		return NewSessionStore(SessionStoreConfig{}), nil
	}

	dbPath := filepath.Join(cfg.StateDir, agentDBFilename)
	store, err := OpenStore(StoreConfig{Path: dbPath})
	if err != nil {
		cfg.Logger.Error(
			"open agent state DB; falling back to in-memory only (no crash resilience)",
			"path", dbPath, "error", err)
		return NewSessionStore(SessionStoreConfig{}), nil
	}

	sessions := NewSessionStore(SessionStoreConfig{Store: store})
	if err := sessions.LoadFromStore(ctx); err != nil {
		// Keep the Store attached: subsequent grants can still persist.
		// We just couldn't reload prior state — the controller will
		// redeliver active sessions on reconnect.
		cfg.Logger.Error(
			"reload sessions from agent state DB; serving with empty in-memory state",
			"path", dbPath, "error", err)
	}

	return sessions, store
}
