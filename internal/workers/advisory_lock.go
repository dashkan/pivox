package workers

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// withAdvisoryLock acquires a session-scoped Postgres advisory lock,
// runs `work`, then releases the lock. The lock is a coordination
// token between replicas, NOT a row-level lock: it ensures only one
// replica enters `work` at a time, but rows touched inside `work`
// are not themselves locked at the database level. Per-row race
// safety is the queries' job (e.g. `MarkDomainVerified` only fires
// on PENDING; `PurgeExpiredOrganization` only fires on a
// soft-deleted row whose grace window has elapsed).
//
// pg_try_advisory_lock is non-blocking: if another replica holds
// the lock, this returns (false, nil) and `work` is not called. The
// caller treats that as "skip this tick"; the next tick will try
// again and either replica might grab it.
//
// The lock is keyed by `lockID`. Use a stable constant per worker
// type — different workers must use different IDs so they don't
// serialize against each other. Conventional values for this
// codebase live in lock_ids.go.
//
// Lock semantics:
//   - Session-scoped (released when the connection closes), not
//     transaction-scoped. We hold one connection from the pool for
//     the duration of `work`. If `work` panics or the ctx is
//     cancelled mid-flight, the deferred Release on the conn
//     returns it to the pool which closes the session — Postgres
//     auto-releases the lock at session close.
//   - Reentrant on the same session (Postgres semantics). We don't
//     rely on that here; one tick == one acquire.
func withAdvisoryLock(ctx context.Context, pool *pgxpool.Pool, lockID int64, work func(context.Context) error) (acquired bool, err error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return false, fmt.Errorf("acquire connection for advisory lock: %w", err)
	}
	defer conn.Release()

	var got bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", lockID).Scan(&got); err != nil {
		return false, fmt.Errorf("pg_try_advisory_lock(%d): %w", lockID, err)
	}
	if !got {
		return false, nil
	}
	defer func() {
		// Release errors are intentionally swallowed: if the conn
		// is borked, returning it to the pool closes the session
		// and Postgres releases the lock. A logged error here
		// would just be noise.
		_, _ = conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", lockID)
	}()

	if err := work(ctx); err != nil {
		return true, err
	}
	return true, nil
}

// Stable advisory-lock IDs. Different worker types use distinct
// IDs so they don't serialize against each other; same ID across
// replicas of the same worker type ensures only one replica is
// active at a time. Values are arbitrary int64s; they're meaningful
// only relative to each other and to the application namespace
// (Postgres advisory locks are global per database, so a
// long-running future cohabitant in this database should pick a
// non-overlapping range).
const (
	purgeWorkerLockID        int64 = 0x70_69_76_70 // 'pivp'
	verifyDomainWorkerLockID int64 = 0x70_69_76_64 // 'pivd'
	spacePurgeWorkerLockID   int64 = 0x70_69_76_73 // 'pivs'
)
