package workers

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/dashkan/pivox/internal/db/generated"
)

// VerifyDomainWorker drives PENDING domain rows to VERIFIED via
// the DNSResolver seam. v1 wires the stub resolver, which always
// "succeeds" — so every PENDING domain becomes VERIFIED on the
// next tick. When real DNS lands (sub-decision #10), only the
// resolver impl changes; the worker's tick body is unchanged.
//
// Backoff schedule (2 min × 1h → 30 min × 24h → 6h × 6d → EXPIRED)
// from the IAM roadmap is wire-only in this commit. With the stub
// resolver, every tick succeeds, so backoff scheduling has no
// observable effect. Tracking it here would be ceremony; the real-
// DNS commit grows the schedule into actual per-row state.
type VerifyDomainWorker struct {
	pool     *pgxpool.Pool
	queries  db.Querier
	resolver DNSResolver
	logger   *slog.Logger
	interval time.Duration
}

// NewVerifyDomainWorker constructs a worker. `resolver` is the
// DNS seam — production passes StubDNSResolver until real DNS
// lands. `interval` is the tick cadence; the v1 stub doesn't need
// fine-grained ticks, so a 2-minute production interval and
// millisecond test interval both work.
func NewVerifyDomainWorker(pool *pgxpool.Pool, queries db.Querier, resolver DNSResolver, logger *slog.Logger, interval time.Duration) *VerifyDomainWorker {
	return &VerifyDomainWorker{pool: pool, queries: queries, resolver: resolver, logger: logger, interval: interval}
}

// Name implements Worker.
func (w *VerifyDomainWorker) Name() string { return "verify-domain" }

// Run blocks until ctx is cancelled. Per-tick errors are logged
// and swallowed.
func (w *VerifyDomainWorker) Run(ctx context.Context) error {
	return loop(ctx, w.logger, w.Name(), w.interval, w.tick)
}

// tick takes the advisory lock and delegates to processBatch.
// Split so tests can exercise processBatch without spinning up a
// real *pgxpool.Pool for the lock.
func (w *VerifyDomainWorker) tick(ctx context.Context) {
	acquired, err := withAdvisoryLock(ctx, w.pool, verifyDomainWorkerLockID, w.processBatch)
	if err != nil {
		w.logger.Error("verify-domain: tick failed", "error", err)
		return
	}
	if !acquired {
		w.logger.Debug("verify-domain: skipped (advisory lock held by peer)")
	}
}

// processBatch scans for PENDING domains, runs DNS lookups, and
// updates rows. The MarkDomainVerified query is race-safe: it only
// flips PENDING→VERIFIED, so a concurrent FAILED transition is
// absorbed as a no-op.
func (w *VerifyDomainWorker) processBatch(ctx context.Context) error {
	domains, err := w.queries.ListPendingDomains(ctx)
	if err != nil {
		return err
	}
	if len(domains) == 0 {
		return nil
	}
	w.logger.Info("verify-domain: ticking pending domains", "count", len(domains))
	for _, d := range domains {
		records, lookupErr := w.resolver.LookupTXT(ctx, "_pivox-verify."+d.Domain)
		if lookupErr != nil || len(records) == 0 {
			// Lookup failure isn't a verification failure —
			// real DNS frequently returns transient errors that
			// don't mean the record is wrong. The row stays
			// PENDING; the next tick retries. The dedicated
			// FAILED transition fires only after the full
			// backoff schedule elapses, which the v1 stub never
			// triggers.
			w.logger.Warn("verify-domain: lookup failed; will retry next tick",
				"domain", d.Domain, "error", lookupErr)
			continue
		}
		updated, err := w.queries.MarkDomainVerified(ctx, d.ID)
		if err != nil {
			w.logger.Error("verify-domain: MarkDomainVerified failed",
				"domain", d.Domain, "error", err)
			continue
		}
		w.logger.Info("verify-domain: verified", "domain", updated.Domain)
	}
	return nil
}
