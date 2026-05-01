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
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
)

// SlowTxThreshold is the duration above which PoolTxer.Run emits a
// `slog.Warn` so operators can spot transactions that hold locks too
// long. 250ms balances "fast enough to surface real outliers" against
// "slow enough to ignore cold-start noise."
//
// Handlers that legitimately run slower (bulk import, big LRO step)
// should run outside a tx — long-held DB locks are the bug this
// threshold is meant to detect, not legitimate batch work.
const SlowTxThreshold = 250 * time.Millisecond

// TxBeginner is the minimal subset of *pgxpool.Pool that PoolTxer
// needs. *pgxpool.Pool implements this directly. Tests can satisfy
// it with a tx-mock without standing up a real pool.
type TxBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Txer runs a closure inside a database transaction. Two
// implementations:
//
//   - *PoolTxer: production. Begins a real pgx tx, runs fn against a
//     transaction-bound Querier, commits on success / rolls back on
//     error or panic.
//   - *PassthroughTxer: tests. Runs fn against a fixed (typically
//     mock) Querier with no actual transaction. Lets unit tests
//     mock query-level behavior without faking the whole pgx.Tx
//     surface.
//
// Servers that need transactional writes hold a Txer in their config
// rather than a TxBeginner directly. The constructor wraps the
// production *pgxpool.Pool in &PoolTxer{Pool: pool}; unit tests
// inject a &PassthroughTxer{Q: mockQuerier}.
type Txer interface {
	// Run executes fn inside a transaction. fn MUST use the
	// supplied qtx for ALL DB operations within scope — mixing it
	// with any connection-pool-bound Queries instance defeats the
	// atomicity (different connections, different transactions).
	//
	// fn returning a non-nil error rolls back; nil commits.
	Run(ctx context.Context, fn func(qtx Querier) error) error
}

// PoolTxer is the production Txer — wraps a *pgxpool.Pool (or any
// TxBeginner) and runs fn inside a real Postgres transaction.
type PoolTxer struct {
	Pool TxBeginner
}

// Run begins a tx, calls fn against a tx-bound Querier, commits on
// nil error / rolls back otherwise. Always defers Rollback — after a
// successful Commit the tx is closed, making Rollback a no-op
// (returns ErrTxClosed which we silence). Standard pgx idiom.
//
// Emits a slog.Warn if total elapsed exceeds SlowTxThreshold.
func (p *PoolTxer) Run(ctx context.Context, fn func(qtx Querier) error) error {
	if p == nil || p.Pool == nil {
		return errNilPool
	}
	start := time.Now()
	tx, err := p.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := fn(New(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}

	if elapsed := time.Since(start); elapsed > SlowTxThreshold {
		slog.WarnContext(ctx, "slow transaction",
			"elapsed_ms", elapsed.Milliseconds(),
			"threshold_ms", SlowTxThreshold.Milliseconds(),
		)
	}
	return nil
}

// PassthroughTxer is a Txer that runs fn against a fixed Querier
// with no actual transaction. **Test-only.** Use only in unit tests
// where mocking the Querier is the goal and exercising the real tx
// machinery would just require faking the whole pgx.Tx surface for
// no testing value.
//
// PassthroughTxer must NEVER be wired into production paths — its
// no-tx semantics defeat the whole reason for using a Txer. Reviews
// should reject any production constructor that takes a Txer
// directly without going through a *pgxpool.Pool.
type PassthroughTxer struct {
	Q Querier
}

// Run calls fn(q) with the fixed Querier. No tx involved.
func (p *PassthroughTxer) Run(ctx context.Context, fn func(qtx Querier) error) error {
	return fn(p.Q)
}

// RunInTx runs fn inside a transaction owned by txer and returns
// the closure's result. Generic over T so handlers return whatever
// natural shape they produce (proto rows, internal structs, slices)
// without an `interface{}` dance.
//
// Lifecycle is owned by txer.Run; this helper just adapts a typed
// (T, error) closure into Txer's untyped error closure shape.
//
// Pattern at the call site:
//
//	result, err := db.RunInTx(ctx, s.txer, func(qtx db.Querier) (db.Foo, error) {
//	    row, err := qtx.Lookup(ctx, ...)
//	    if err != nil { return db.Foo{}, err }
//	    return qtx.Mutate(ctx, ...)
//	})
//
// Mixing the tx-bound qtx with a connection-pool-bound *Queries
// (`s.queries`) inside the closure is a bug — they run on different
// connections, defeating atomicity and creating TOCTOU windows
// between scope checks and writes.
func RunInTx[T any](ctx context.Context, txer Txer, fn func(qtx Querier) (T, error)) (T, error) {
	var result T
	err := txer.Run(ctx, func(qtx Querier) error {
		var fnErr error
		result, fnErr = fn(qtx)
		return fnErr
	})
	return result, err
}

// RunInTxVoid is the side-effect-only variant for handlers that
// don't return a value beyond ok/err. Spelling RunInTx[T] with
// T=struct{} works but is awkward at every call site.
func RunInTxVoid(ctx context.Context, txer Txer, fn func(qtx Querier) error) error {
	return txer.Run(ctx, fn)
}

// errNilPool is the sentinel returned when PoolTxer is constructed
// without a Pool. Production constructors panic on missing Config
// fields, so this should be unreachable in normal operation; it
// exists as a defense for direct &PoolTxer{} construction in tests.
var errNilPool = errPoolUnset{}

type errPoolUnset struct{}

func (errPoolUnset) Error() string { return "db: PoolTxer constructed without Pool" }
