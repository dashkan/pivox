package storageagent

import (
	"context"
	"fmt"
	"log/slog"
	"path"
	"strings"
	"sync"
	"time"
)

// Session represents a user's access grant for this gateway.
type Session struct {
	Patterns []string
	Expiry   time.Time
}

// SessionStore is a thread-safe store of opaque session tokens mapped
// to access patterns. Populated via bidi SessionGrant messages, cleaned
// up periodically.
//
// When constructed with a non-nil SessionStoreConfig.Store, every
// Grant/Revoke/FlushExpired write is mirrored to the SQLite store
// atomically with the in-memory update. Boot-time reload is the
// caller's responsibility via LoadFromStore — see #79 for the broader
// crash-resilience flow. This package only exposes the contract; the
// boot wiring lives in cmd/pivox-agent/.
//
// Persistence semantics: writes are atomic with the in-memory update.
// If the SQLite write fails, the in-memory map is NOT updated and the
// caller receives an error. This matches the controller's expectation
// that a SessionGrant either takes effect both in memory and on disk
// or not at all (a session that survives in memory but not on disk
// would silently lose crash resilience).
//
// Lock contention trade-off: Grant/Revoke/FlushExpired hold the write
// lock across the SQLite call, so HTTP authorization (Authorize, RLock)
// briefly serializes behind every persist tail. Acceptable today
// (low-frequency grants, sub-ms WAL writes) and tracked for refinement
// in #80 when load justifies the work.
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*Session // key: opaque token

	// persist is set at construction via SessionStoreConfig.Store.
	// nil = in-memory only.
	persist *Store
}

// SessionStoreConfig is the constructor input for SessionStore.
type SessionStoreConfig struct {
	// Store, if non-nil, makes every Grant/Revoke/FlushExpired mirror
	// to SQLite atomically with the in-memory update. Optional.
	// Zero-value config = in-memory only.
	Store *Store
}

// NewSessionStore constructs a SessionStore from cfg. A nil cfg.Store
// produces an in-memory-only store (suitable for tests and
// integrations that don't need crash resilience).
func NewSessionStore(cfg SessionStoreConfig) *SessionStore {
	return &SessionStore{
		sessions: make(map[string]*Session),
		persist:  cfg.Store,
	}
}

// LoadFromStore reads existing sessions from the attached store into
// the in-memory map. Already-expired rows are physically deleted from
// the store first so they don't accumulate across restarts.
//
// Called once at agent boot, before the HTTP listener starts accepting
// requests. No-op if no store is attached.
//
// Failure modes: the expired-flush and the load are NOT a single
// transaction. If LoadSessions fails after DeleteExpiredSessions
// succeeded, the agent boots with an empty in-memory map but the
// on-disk store has been pruned of expired rows. This is acceptable
// because the controller is the source of truth and re-delivers
// active sessions on the next reconnect handshake; an extra pruning
// of stale rows is harmless.
func (s *SessionStore) LoadFromStore(ctx context.Context) error {
	if s.persist == nil {
		return nil
	}

	// Drop dead rows before loading them — saves work and prevents
	// gradual cumulative buildup across restarts.
	if _, err := s.persist.DeleteExpiredSessions(ctx, time.Now()); err != nil {
		return fmt.Errorf("flush expired before load: %w", err)
	}

	rows, err := s.persist.LoadSessions(ctx)
	if err != nil {
		return fmt.Errorf("load sessions: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, row := range rows {
		s.sessions[row.Token] = &Session{
			Patterns: row.Patterns,
			Expiry:   row.Expiry,
		}
	}
	return nil
}

// Grant stores a session. Overwrites if the token already exists
// (matches controller-side pattern-update on re-grant).
//
// Atomic with persistence: if a store is attached and the write fails,
// the in-memory map is left untouched and the error is returned.
//
// Expiry precision: SQLite stores Unix-second timestamps and
// LoadFromStore reloads at second granularity. To make the in-memory
// view agree with what survives a restart, expiry is truncated to
// seconds before storing. Sub-second precision in the input is
// dropped (acceptable for session lifetimes measured in hours).
func (s *SessionStore) Grant(ctx context.Context, token string, patterns []string, expiry time.Time) error {
	expiry = expiry.Truncate(time.Second)

	// Persist FIRST under the lock — atomicity requires the in-memory
	// update only succeed if persistence succeeded. Holding the write
	// lock across the SQLite call serializes Grant/Revoke/FlushExpired
	// at this layer; the underlying Store further serializes via its
	// 1-conn pool. This is acceptable because session grants are low
	// frequency (one per browser session, ~hourly TTL).
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.persist != nil {
		if err := s.persist.SaveSession(ctx, StoredSession{
			Token:    token,
			Patterns: patterns,
			Expiry:   expiry,
		}); err != nil {
			return fmt.Errorf("grant session: %w", err)
		}
	}
	s.sessions[token] = &Session{
		Patterns: patterns,
		Expiry:   expiry,
	}
	return nil
}

// Revoke removes a session immediately. Atomic with persistence; if
// the persisted delete fails, in-memory state is left untouched so the
// caller can retry without diverging from disk.
func (s *SessionStore) Revoke(ctx context.Context, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.persist != nil {
		if err := s.persist.DeleteSession(ctx, token); err != nil {
			return fmt.Errorf("revoke session: %w", err)
		}
	}
	delete(s.sessions, token)
	return nil
}

// Authorize checks if the given token grants access to the request
// path. Returns true if any pattern matches. Returns false if the
// token is unknown or its expiry is in the past.
func (s *SessionStore) Authorize(token string, requestPath string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sess, ok := s.sessions[token]
	if !ok {
		return false
	}
	if time.Now().After(sess.Expiry) {
		return false
	}
	for _, p := range sess.Patterns {
		if matchPattern(p, requestPath) {
			return true
		}
	}
	return false
}

// FlushExpired removes all sessions past their expiry time, both in
// memory and on disk. Returns an error if the persisted delete fails;
// the in-memory map is then left untouched so a retry sees the same
// state.
func (s *SessionStore) FlushExpired(ctx context.Context) error {
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.persist != nil {
		if _, err := s.persist.DeleteExpiredSessions(ctx, now); err != nil {
			return fmt.Errorf("flush expired sessions: %w", err)
		}
	}
	for token, sess := range s.sessions {
		if now.After(sess.Expiry) {
			delete(s.sessions, token)
		}
	}
	return nil
}

// StartCleanup runs FlushExpired on a timer until ctx is cancelled.
// Cleanup-time errors (e.g. transient SQLite issues) are logged and
// otherwise ignored — the next tick retries. Cancel via ctx.
func (s *SessionStore) StartCleanup(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.FlushExpired(ctx); err != nil {
				slog.WarnContext(ctx, "session cleanup tick failed",
					"error", err)
			}
		}
	}
}

// matchPattern checks whether requestPath matches the given pattern.
// If the pattern ends with /*, a prefix match is performed to support
// recursive wildcards. Otherwise path.Match is used for single-segment
// glob matching.
func matchPattern(pattern, requestPath string) bool {
	if prefix, ok := strings.CutSuffix(pattern, "/*"); ok {
		return strings.HasPrefix(requestPath, prefix+"/") || requestPath == prefix
	}
	matched, _ := path.Match(pattern, requestPath)
	return matched
}
