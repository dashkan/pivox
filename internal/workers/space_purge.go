package workers

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/dashkan/pivox/internal/db/generated"
)

// SpacePurgeWorker is the space-scope analogue of PurgeWorker. Each
// tick:
//
//  1. Acquires the space-purge advisory lock; another replica
//     holding it means we skip.
//  2. Lists soft-deleted spaces whose `purge_time` is in the past.
//  3. For each, calls `PurgeExpiredSpace` which DELETEs the space
//     row; FK ON DELETE CASCADE removes space_members, assets, and
//     asset_requests transitively.
//
// Distinct from PurgeWorker so a slow space-cascade doesn't starve
// org-level purges (and vice versa). Uses a different advisory
// lock ID so the two can run concurrently.
//
// Steady-state load: at most a handful of spaces per tick. The
// 100-row LIMIT in the query bounds runaway batches and the next
// tick picks up the remainder.
type SpacePurgeWorker struct {
	pool     *pgxpool.Pool
	queries  db.Querier
	logger   *slog.Logger
	interval time.Duration
}

// NewSpacePurgeWorker constructs a worker. `interval` controls how
// often the scan runs; production typically uses a few minutes,
// tests override to milliseconds. `pool` is required (advisory
// locks need a real connection); `queries` is the typed wrapper
// used for the actual scan and purge calls.
func NewSpacePurgeWorker(pool *pgxpool.Pool, queries db.Querier, logger *slog.Logger, interval time.Duration) *SpacePurgeWorker {
	return &SpacePurgeWorker{pool: pool, queries: queries, logger: logger, interval: interval}
}

// Name implements Worker.
func (w *SpacePurgeWorker) Name() string { return "space-purge" }

// Run blocks on the supplied context. Per-tick errors are logged
// and swallowed — the loop keeps running. Returns ctx.Err() on
// cancellation.
func (w *SpacePurgeWorker) Run(ctx context.Context) error {
	return loop(ctx, w.logger, w.Name(), w.interval, w.tick)
}

// tick takes the advisory lock and delegates to processBatch.
func (w *SpacePurgeWorker) tick(ctx context.Context) {
	acquired, err := withAdvisoryLock(ctx, w.pool, spacePurgeWorkerLockID, w.processBatch)
	if err != nil {
		w.logger.Error("space-purge: tick failed", "error", err)
		return
	}
	if !acquired {
		w.logger.Debug("space-purge: skipped (advisory lock held by peer)")
	}
}

// ProcessBatchForTest exposes the unexported tick body for
// cross-package E2E tests, bypassing the advisory-lock dance (the
// lock is a multi-replica coordination token; in-process tests run
// a single replica and don't need it). Same naming convention as
// `EnforceSoftDeleteGateForTest` in internal/server.
func (w *SpacePurgeWorker) ProcessBatchForTest(ctx context.Context) error {
	return w.processBatch(ctx)
}

// processBatch lists spaces past their purge window and cascades
// each one. Split out so tests can exercise the inner logic without
// needing a real *pgxpool.Pool for the advisory lock.
func (w *SpacePurgeWorker) processBatch(ctx context.Context) error {
	spaces, err := w.queries.ListSpacesPastPurgeTime(ctx)
	if err != nil {
		return err
	}
	if len(spaces) == 0 {
		return nil
	}
	w.logger.Info("space-purge: cascading spaces past grace window", "count", len(spaces))
	for _, sp := range spaces {
		// PurgeExpiredSpace is the race-safe variant: only fires on
		// a row that's still soft-deleted with an elapsed
		// purge_time. A concurrent UndeleteSpace between the list
		// and this delete is absorbed (no rows affected, no error
		// from :exec).
		if err := w.queries.PurgeExpiredSpace(ctx, sp.ID); err != nil {
			w.logger.Error("space-purge: PurgeExpiredSpace failed",
				"space_id", sp.ID, "name", sp.Name, "error", err)
			continue
		}
		w.logger.Info("space-purge: space cascaded", "space_id", sp.ID, "name", sp.Name)
	}
	return nil
}
