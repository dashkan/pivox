// Hand-written, NOT sqlc-generated. Lives in this package so the
// transaction helper is co-located with the Querier interface it
// produces. sqlc only writes files it generates, so this file is
// preserved across `sqlc generate` runs — but if you ever see a
// generation step claim to delete it, that's a bug; don't `git rm`
// this in confusion.
//
// See `internal/AGENTS.md` for the project-wide rule on when to use
// RunInTx.

package db

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
)

// SlowTxThreshold is the duration above which RunInTx emits a
// `slog.Warn` so operators can spot transactions that hold locks too
// long. 250ms is a balance: fast enough to surface real outliers
// (cold-cache reads + a write are typically ~10–50ms locally; a
// well-indexed handler on a warm pool should never approach this),
// slow enough that we don't drown logs in cold-start noise.
//
// If a handler legitimately runs slower (bulk import, big LRO step),
// it should run outside RunInTx — long-held DB locks are the bug
// this threshold is meant to detect.
const SlowTxThreshold = 250 * time.Millisecond

// TxBeginner is the minimal subset of *pgxpool.Pool that RunInTx
// needs. *pgxpool.Pool implements this directly. Tests can satisfy
// it with a tx-mock instead of standing up a real pool.
type TxBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// RunInTx runs fn inside a Postgres transaction.
//
// The fn closure receives a transaction-bound `Querier` (`qtx`).
// **All DB operations inside the closure MUST use this qtx.** Mixing
// it with the connection-pool-bound *Queries (`s.queries`) is a
// bug — they run on different connections and therefore different
// transactions, defeating atomicity and creating TOCTOU windows
// between scope checks and writes.
//
// Lifecycle:
//
//   - Begin a tx on entry.
//   - Defer a Rollback (no-op if Commit already ran successfully —
//     pgx returns ErrTxClosed which we silence).
//   - Call fn(qtx).
//   - On non-nil error from fn, return early. Defer fires Rollback.
//   - On nil error, Commit. If Commit fails, return that error
//     (defer's Rollback is a no-op against a failed-commit tx).
//   - Emit a slog.Warn when total elapsed time exceeds
//     SlowTxThreshold.
//
// Generic over T so handlers return whatever their natural shape is
// (proto, internal struct, slice, etc.) without an `interface{}`
// dance.
//
// Logging is deliberately minimal: just the slow-tx warning.
// Per-tx tracing belongs at the handler / interceptor layer, not
// here — wrapping every commit in span overhead would add latency
// to every write path. If we need richer telemetry later, add it as
// a Config option (e.g. `RunInTxConfig{Threshold, Logger,
// MetricsHook}`) — don't add fields to `RunInTx` itself.
func RunInTx[T any](ctx context.Context, pool TxBeginner, fn func(qtx Querier) (T, error)) (T, error) {
	start := time.Now()
	var zero T

	tx, err := pool.Begin(ctx)
	if err != nil {
		return zero, err
	}
	// Always defer Rollback. After a successful Commit the tx is
	// already closed, so this becomes a no-op-with-ErrTxClosed which
	// we silence. After fn returns an error or panics, this is the
	// rollback path. Standard pgx idiom.
	defer func() { _ = tx.Rollback(ctx) }()

	result, err := fn(New(tx))
	if err != nil {
		return zero, err
	}
	if err := tx.Commit(ctx); err != nil {
		return zero, err
	}

	if elapsed := time.Since(start); elapsed > SlowTxThreshold {
		slog.WarnContext(ctx, "slow transaction",
			"elapsed_ms", elapsed.Milliseconds(),
			"threshold_ms", SlowTxThreshold.Milliseconds(),
		)
	}
	return result, nil
}

// RunInTxVoid is the side-effect-only variant for handlers that
// don't return a value beyond ok/err. Spelling RunInTx[T] with
// T = struct{} works but is awkward at every call site; this
// helper hides the empty-result dance.
func RunInTxVoid(ctx context.Context, pool TxBeginner, fn func(qtx Querier) error) error {
	_, err := RunInTx(ctx, pool, func(qtx Querier) (struct{}, error) {
		return struct{}{}, fn(qtx)
	})
	return err
}
