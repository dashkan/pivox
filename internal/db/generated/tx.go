// Hand-written, NOT sqlc-generated. Lives in this package so the
// transaction abstraction is co-located with the Querier interface
// it produces. sqlc only writes files it generates, so this file is
// preserved across `sqlc generate` runs — but if you ever see a
// generation step claim to delete it, that's a bug; don't `git rm`
// this in confusion.
//
// See `internal/AGENTS.md` for the project-wide rule on when to use
// transactions.

package db

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"runtime/debug"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// SlowTxThreshold is the duration above which RunInTx emits a
// `slog.Warn` so operators can spot transactions that hold locks too
// long. 250ms balances "fast enough to surface real outliers" against
// "slow enough to ignore cold-start noise."
//
// Application-level constant — single value across the deployment.
// If a future need requires per-deployment tuning, lift it into
// config.Config and have main.go assign through this var. Most
// handlers either fit comfortably under 250ms or shouldn't be in a
// tx at all (bulk imports, big LRO steps).
const SlowTxThreshold = 250 * time.Millisecond

// Retry constants for transient Postgres errors that guarantee no
// commit landed (so retry is safe at the application layer).
//
//   - 40001 serialization_failure — under SERIALIZABLE/REPEATABLE
//     READ, Postgres detected a concurrent tx would have produced
//     a different result, picks a victim, rolls back the whole tx.
//   - 40P01 deadlock_detected — Postgres detected a deadlock cycle,
//     killed the victim, rolled back the whole tx.
//
// Both fire from any DB call inside the tx (most often Commit).
// Postgres docs explicitly say "applications must be prepared to
// retry." We retry up to maxTxRetries times with exponential
// backoff + jitter; the jitter spreads contending tx pairs so they
// don't lockstep into the same conflict on every attempt.
//
// 3 attempts is the conventional cap — more than 3 means real
// contention pressure that retries can't paper over (and the tx
// just propagates the error to the caller).
const (
	maxTxRetries          = 3
	initialRetryBackoff   = 5 * time.Millisecond
	maxRetryBackoff       = 100 * time.Millisecond
	pgSerializationFailed = "40001"
	pgDeadlockDetected    = "40P01"
)

// TxBeginner is the minimal subset of *pgxpool.Pool that RunInTx
// needs. *pgxpool.Pool satisfies it directly via BeginTx.
type TxBeginner interface {
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
}

// RWPool unifies the DBTX surface (pgx connection-pool exec/query
// methods, used by raw filter.Query and similar non-sqlc reads) with
// TxBeginner (begins a transaction). *pgxpool.Pool implements both,
// so a single Pool field on a server Config can stand in for both
// roles — callers don't have to pass the same pool reference twice.
type RWPool interface {
	DBTX
	TxBeginner
}

// RunInTxRaw is the escape-hatch sibling of RunInTx that exposes
// the underlying pgx.Tx alongside the sqlc-generated Querier.
//
// Use ONLY for Postgres-specific functionality not expressible via
// sqlc queries:
//
//   - pg_advisory_xact_lock(<key>) / pg_advisory_xact_unlock for
//     application-level mutual exclusion within a tx.
//   - SET LOCAL session-level overrides (statement_timeout,
//     lock_timeout, etc.) scoped to this tx.
//   - LISTEN / NOTIFY inside a tx.
//   - Raw COPY for bulk loads.
//
// Convention at the call site: use qtx for every sqlc-generated
// query; reach for tx ONLY for the specific raw operation that
// can't go through sqlc. Mixing the two on the same tx is fine —
// they share the same pgx.Tx underneath, so reads/writes against
// either route remain atomic.
//
// Same retry / panic-recovery / slow-tx / error-wrapping semantics
// as RunInTx.
func RunInTxRaw[T any](ctx context.Context, pool TxBeginner,
	fn func(qtx Querier, tx pgx.Tx) (T, error), opts ...pgx.TxOptions) (T, error) {
	return runInTxImpl(ctx, pool, false, fn, opts...)
}

// RunInTx runs fn inside a Postgres transaction owned by pool.
//
// Defaults: pgx.TxOptions{} — read-committed, read-write,
// non-deferrable. Pass pgx.TxOptions explicitly via the variadic
// last arg when the handler needs stronger guarantees:
//
//	result, err := db.RunInTx(ctx, s.pool, fn)
//	result, err := db.RunInTx(ctx, s.pool, fn,
//	    pgx.TxOptions{IsoLevel: pgx.Serializable})
//
// fn MUST use the supplied qtx for ALL DB operations within scope —
// mixing it with any connection-pool-bound Queries instance defeats
// the atomicity (different connections, different transactions).
//
// fn side effects MUST be DB-only. Non-DB side effects (cache writes,
// queue publishes, external HTTP calls) are dangerous because the
// fn may be REPLAYED on a transient Postgres error (40001
// serialization failure or 40P01 deadlock — see retry semantics
// below). Idempotent non-DB side effects are fine; non-idempotent
// ones must be moved outside the closure.
//
// Lifecycle (per attempt):
//   - Begin tx with the supplied (or default) options.
//   - Call fn(qtx). On non-nil error, the deferred rollback fires.
//     On panic, the panic is recovered, logged with stack, converted
//     to error, and the deferred rollback fires.
//   - Commit on nil error from fn.
//   - Always log non-ErrTxClosed rollback errors (they signal
//     connection state issues worth seeing).
//   - Always log if total elapsed > SlowTxThreshold, regardless of
//     success/failure outcome.
//
// Retry: on Postgres error 40001 (serialization_failure) or 40P01
// (deadlock_detected) — both classes Postgres explicitly guarantees
// "rolled back, nothing committed, retry-safe" — RunInTx replays
// the closure with a fresh tx up to maxTxRetries times with
// exponential backoff + jitter. Other errors propagate immediately.
// Slow-tx warning measures total elapsed across all attempts.
//
// Generic over T so handlers return whatever natural shape they
// produce (proto rows, internal structs, slices) without an
// `interface{}` dance.
func RunInTx[T any](ctx context.Context, pool TxBeginner,
	fn func(qtx Querier) (T, error), opts ...pgx.TxOptions) (T, error) {
	return runInTxImpl(ctx, pool, false, func(qtx Querier, _ pgx.Tx) (T, error) {
		return fn(qtx)
	}, opts...)
}

// runInTxImpl is the shared retry/panic-recovery/slow-tx core.
// RunInTx and RunInTxRaw both delegate here — the only difference
// between them is whether the closure receives the underlying
// pgx.Tx alongside Querier. Keeping a single core means retry
// semantics, slow-tx warnings, and metrics live in exactly one
// place.
func runInTxImpl[T any](ctx context.Context, pool TxBeginner, validateOnly bool,
	fn func(qtx Querier, tx pgx.Tx) (T, error), opts ...pgx.TxOptions) (T, error) {
	var zero T
	if pool == nil {
		return zero, errNilPool
	}

	var txOpts pgx.TxOptions
	if len(opts) > 0 {
		txOpts = opts[0]
	}

	start := time.Now()
	// attempts: written by the retry loop, read by the slow-tx defer
	// after the function returns. Same goroutine, sequential — no race.
	var attempts int
	defer func() {
		if elapsed := time.Since(start); elapsed > SlowTxThreshold {
			slog.WarnContext(ctx, "slow transaction",
				"elapsed_ms", elapsed.Milliseconds(),
				"threshold_ms", SlowTxThreshold.Milliseconds(),
				"isolation", txOpts.IsoLevel,
				"attempts", attempts,
			)
		}
		// METRICS: when Prometheus lands (see CLAUDE.md), record here:
		//   - histogram pivox_tx_duration_seconds{isolation,outcome}
		//     — observe elapsed.Seconds() with outcome ∈ {commit,rollback,retry_exhausted}
		//   - counter pivox_tx_retries_total{code} — bump per retry
		//     attempt with the SQLSTATE that triggered it (40001/40P01)
		//   - counter pivox_tx_slow_total{isolation} — bump when
		//     elapsed > SlowTxThreshold (operators alert on rate of change)
		// Plumb outcome via a local var on the success/error returns
		// below. Don't add the metrics yet — wait until the Prom
		// registry + middleware land repo-wide so this stays one
		// observability layer, not N inline counters.
	}()

	backoff := initialRetryBackoff
	var lastErr error
	for attempt := 1; attempt <= maxTxRetries; attempt++ {
		attempts = attempt
		result, err := runTxOnce(ctx, pool, txOpts, validateOnly, fn)
		if err == nil {
			if attempt > 1 {
				slog.InfoContext(ctx, "tx succeeded after retry",
					"attempts", attempt,
				)
			}
			return result, nil
		}
		if !isRetryableTxError(err) {
			// zero rather than the partially-populated result — the
			// (T, error) convention everywhere else in the codebase
			// is "non-nil err means zero value." Don't surface partial
			// state on the error path.
			return zero, err
		}
		lastErr = err
		if attempt == maxTxRetries {
			break
		}
		// Honor ctx cancellation between attempts; don't pile retries
		// onto a caller that has already given up.
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		// Backoff with jitter. The +rand[0, backoff/2) term spreads
		// contending tx pairs so they don't lockstep into the same
		// conflict every attempt.
		sleep := backoff + time.Duration(rand.Int64N(int64(backoff)/2))
		slog.DebugContext(ctx, "tx retry",
			"attempt", attempt,
			"next_backoff_ms", sleep.Milliseconds(),
			"error", err,
		)
		// NewTimer + Stop instead of time.After to avoid leaking the
		// underlying *time.Timer when ctx wins the race. time.After's
		// timer can't be cancelled and lives until it fires; for a
		// long-running server that retries thousands of times under a
		// cancelled ctx, those timers accumulate in the runtime heap.
		timer := time.NewTimer(sleep)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return zero, ctx.Err()
		case <-timer.C:
		}
		backoff *= 2
		if backoff > maxRetryBackoff {
			backoff = maxRetryBackoff
		}
	}
	// If the caller cancelled during the final attempt, report that
	// rather than "retries exhausted" — exhaustion implies we gave up
	// after legitimate contention; cancellation means the caller did.
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return zero, fmt.Errorf("tx retries exhausted (%d attempts): %w", maxTxRetries, lastErr)
}

// runTxOnce is a single tx attempt. Begin → fn → Commit/Rollback,
// with panic recovery. The retry loop in runInTxImpl wraps this.
func runTxOnce[T any](ctx context.Context, pool TxBeginner, txOpts pgx.TxOptions, validateOnly bool,
	fn func(qtx Querier, tx pgx.Tx) (T, error)) (T, error) {
	var zero T
	tx, err := pool.BeginTx(ctx, txOpts)
	if err != nil {
		return zero, fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if rbErr := tx.Rollback(ctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			slog.WarnContext(ctx, "tx rollback failed", "error", rbErr)
		}
	}()

	var (
		result T
		fnErr  error
	)
	func() {
		defer func() {
			if r := recover(); r != nil {
				stack := debug.Stack()
				slog.ErrorContext(ctx, "tx fn panicked",
					"panic", fmt.Sprintf("%v", r),
					"stack", string(stack),
				)
				fnErr = fmt.Errorf("tx fn panicked: %v", r)
			}
		}()
		result, fnErr = fn(New(tx), tx)
	}()
	if fnErr != nil {
		return result, fnErr
	}
	if validateOnly {
		// Deliberate dry-run: skip Commit so the deferred Rollback discards
		// the writes. fn ran against real constraints (unique/FK/NOT NULL,
		// row locks), so any failure a live request would hit already
		// surfaced as fnErr above — the caller gets the same outcome with
		// nothing persisted. Transactional River enqueues (InsertTx) roll
		// back here too, so they need no separate guard.
		return result, nil
	}

	if err := tx.Commit(ctx); err != nil {
		return result, fmt.Errorf("commit tx: %w", err)
	}
	return result, nil
}

// isRetryableTxError reports whether err is a Postgres error that
// can be safely replayed by re-running the entire transaction.
//
// Per the Postgres docs:
//
//   - 40001 serialization_failure — SERIALIZABLE/REPEATABLE READ
//     conflict; transaction was aborted, nothing committed.
//   - 40P01 deadlock_detected — deadlock victim; transaction was
//     aborted, nothing committed.
//
// Both classes are safe to retry because Postgres guarantees the
// rollback completed before returning the error. Any other error
// (connection drop mid-commit, fn-returned error, etc.) is
// ambiguous or caller-driven and must NOT be retried.
func isRetryableTxError(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == pgSerializationFailed || pgErr.Code == pgDeadlockDetected
}

// RunInTxVoid is the side-effect-only variant for handlers that
// don't return a value beyond ok/err. Spelling RunInTx[T] with
// T=struct{} works but is awkward at every call site.
func RunInTxVoid(ctx context.Context, pool TxBeginner,
	fn func(qtx Querier) error, opts ...pgx.TxOptions) error {
	_, err := RunInTx(ctx, pool, func(qtx Querier) (struct{}, error) {
		return struct{}{}, fn(qtx)
	}, opts...)
	return err
}

// RunInTxValidate runs fn in a transaction and, when validateOnly is true,
// rolls back instead of committing: the writes are discarded, but fn still
// ran against real constraints (unique / FK / NOT NULL / row locks), so any
// error a live request would hit is returned unchanged. This is the AIP
// validate_only substrate — permission + field-shape validation already ran
// in the interceptors, so this simulates the DB side and nothing else.
//
// Non-DB side effects (cache/audit invalidation, outbound HTTP, notifications)
// must stay OUTSIDE the closure and be guarded by `if !validateOnly`; they do
// not roll back with the tx. Transactional River enqueues via InsertTx, being
// rows in the same tx, DO roll back and need no guard.
func RunInTxValidate[T any](ctx context.Context, pool TxBeginner, validateOnly bool,
	fn func(qtx Querier) (T, error), opts ...pgx.TxOptions) (T, error) {
	return runInTxImpl(ctx, pool, validateOnly, func(qtx Querier, _ pgx.Tx) (T, error) {
		return fn(qtx)
	}, opts...)
}

// RunInTxRawValidate is RunInTxValidate with raw pgx.Tx access, for handlers
// that enqueue River jobs (InsertTx) or otherwise need the tx inside the
// validated closure.
func RunInTxRawValidate[T any](ctx context.Context, pool TxBeginner, validateOnly bool,
	fn func(qtx Querier, tx pgx.Tx) (T, error), opts ...pgx.TxOptions) (T, error) {
	return runInTxImpl(ctx, pool, validateOnly, fn, opts...)
}

// RunInTxVoidValidate is the side-effect-only RunInTxValidate for handlers
// (e.g. Delete) that return no value beyond ok/err.
func RunInTxVoidValidate(ctx context.Context, pool TxBeginner, validateOnly bool,
	fn func(qtx Querier) error, opts ...pgx.TxOptions) error {
	_, err := RunInTxValidate(ctx, pool, validateOnly, func(qtx Querier) (struct{}, error) {
		return struct{}{}, fn(qtx)
	}, opts...)
	return err
}

// errNilPool is returned when RunInTx is called with a nil pool.
// Production constructors panic on missing Config fields, so this
// should be unreachable in normal operation; it exists as a defense
// for direct nil-pool calls (mostly in tests).
var errNilPool = errors.New("db: RunInTx called with nil pool")
