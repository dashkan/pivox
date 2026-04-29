package workers

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/dashkan/pivox/internal/db/generated"
)

// PurgeWorker drives the final cascade for soft-deleted
// organizations whose 30-day grace window has elapsed. Each tick:
//
//  1. Acquires the purge advisory lock; another replica holding it
//     means we skip this tick.
//  2. Lists soft-deleted orgs whose `purge_time` is in the past.
//  3. For each, calls `PurgeOrganization` which DELETEs the org
//     row; FK ON DELETE CASCADE removes spaces, members, domains,
//     SSO config, assets, requests, tags, API keys, and AI
//     conversations transitively.
//
// Steady-state load: at most a handful of orgs per tick. The
// 100-row LIMIT in the query bounds runaway batches and the
// next tick picks up the remainder.
//
// PurgeWorker does NOT operate on soft-deleted users yet — user
// soft-delete is implicit in the DeleteUser LRO (no row-level
// soft-delete state for firebase_identities). When per-user soft
// delete lands, this worker grows a parallel users.purge_time
// scan.
type PurgeWorker struct {
	pool     *pgxpool.Pool
	queries  db.Querier
	logger   *slog.Logger
	interval time.Duration
}

// NewPurgeWorker constructs a worker. `interval` controls how often
// the scan runs; production typically uses a few minutes, tests
// override to milliseconds. `pool` is required (advisory locks
// need a real connection); `queries` is the typed wrapper used for
// the actual scan and purge calls.
func NewPurgeWorker(pool *pgxpool.Pool, queries db.Querier, logger *slog.Logger, interval time.Duration) *PurgeWorker {
	return &PurgeWorker{pool: pool, queries: queries, logger: logger, interval: interval}
}

// Name implements Worker. Used in startup/shutdown logs.
func (w *PurgeWorker) Name() string { return "purge" }

// Run blocks on the supplied context. Per-tick errors are logged
// and swallowed — the loop keeps running. Returns ctx.Err() on
// cancellation so the caller can distinguish shutdown from a fatal
// loop exit (today there are no fatal exits; the lock-skip path is
// not an error).
func (w *PurgeWorker) Run(ctx context.Context) error {
	return loop(ctx, w.logger, w.Name(), w.interval, w.tick)
}

// tick takes the advisory lock and delegates to processBatch.
// Errors are logged but never returned: a transient DB hiccup
// shouldn't kill the worker.
func (w *PurgeWorker) tick(ctx context.Context) {
	acquired, err := withAdvisoryLock(ctx, w.pool, purgeWorkerLockID, w.processBatch)
	if err != nil {
		w.logger.Error("purge: tick failed", "error", err)
		return
	}
	if !acquired {
		// Another replica holds the lock — log at DEBUG so contention
		// is visible in telemetry without flooding INFO. Without this
		// line a missing-tick is indistinguishable from
		// worker-not-running, which makes ops debugging painful.
		w.logger.Debug("purge: skipped (advisory lock held by peer)")
	}
}

// processBatch lists orgs past their purge window and cascades each
// one. Split out so tests can exercise the inner logic without
// needing a real *pgxpool.Pool for the advisory lock.
func (w *PurgeWorker) processBatch(ctx context.Context) error {
	orgs, err := w.queries.ListOrgsPastPurgeTime(ctx)
	if err != nil {
		return err
	}
	if len(orgs) == 0 {
		return nil
	}
	w.logger.Info("purge: cascading orgs past grace window", "count", len(orgs))
	for _, o := range orgs {
		// PurgeExpiredOrganization is the race-safe variant: it
		// only fires on a row that's still soft-deleted with an
		// elapsed purge_time. A concurrent UndeleteOrganization
		// between the list and this delete is absorbed (no rows
		// affected, no error from :exec).
		if err := w.queries.PurgeExpiredOrganization(ctx, o.ID); err != nil {
			w.logger.Error("purge: PurgeExpiredOrganization failed", "org", o.Name, "error", err)
			continue // proceed with the rest; stuck row will surface again next tick
		}
		w.logger.Info("purge: org cascaded", "org", o.Name)
	}
	return nil
}
