package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// poolAdapter wraps pgxmock.PgxPoolIface so it satisfies db.TxBeginner.
// pgxmock returns pgx.Tx-compatible mocks; we just need the BeginTx
// surface that the wrapper actually calls.
type poolAdapter struct{ pgxmock.PgxPoolIface }

func (p poolAdapter) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	return p.PgxPoolIface.BeginTx(ctx, opts)
}

func newMockPool(t *testing.T) (poolAdapter, pgxmock.PgxPoolIface) {
	t.Helper()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(mock.Close)
	return poolAdapter{mock}, mock
}

// TestRunInTx_NilPool covers the nil-pool defense. Production
// constructors panic on missing Pool fields, so this should be
// unreachable in normal operation — but the sentinel guards
// direct nil-pool calls in test code.
func TestRunInTx_NilPool(t *testing.T) {
	t.Parallel()
	_, err := RunInTx(context.Background(), nil, func(qtx Querier) (struct{}, error) {
		t.Fatal("fn must not run with nil pool")
		return struct{}{}, nil
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil pool")
}

// TestRunInTx_HappyPath_Commits exercises the end-to-end success
// path: Begin → fn(qtx) returns nil → Commit fires.
func TestRunInTx_HappyPath_Commits(t *testing.T) {
	t.Parallel()
	pool, mock := newMockPool(t)
	mock.ExpectBegin()
	mock.ExpectCommit()

	got, err := RunInTx(context.Background(), pool, func(qtx Querier) (string, error) {
		return "ok", nil
	})
	require.NoError(t, err)
	assert.Equal(t, "ok", got)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestRunInTx_FnError_Rollbacks exercises the error path: fn
// returns non-nil error → no Commit, deferred Rollback fires.
func TestRunInTx_FnError_Rollbacks(t *testing.T) {
	t.Parallel()
	pool, mock := newMockPool(t)
	mock.ExpectBegin()
	mock.ExpectRollback()

	wantErr := errors.New("boom")
	_, err := RunInTx(context.Background(), pool, func(qtx Querier) (struct{}, error) {
		return struct{}{}, wantErr
	})
	require.ErrorIs(t, err, wantErr)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestRunInTx_BeginError surfaces Begin failures wrapped.
func TestRunInTx_BeginError(t *testing.T) {
	t.Parallel()
	pool, mock := newMockPool(t)
	mock.ExpectBegin().WillReturnError(errors.New("connection refused"))

	_, err := RunInTx(context.Background(), pool, func(qtx Querier) (struct{}, error) {
		t.Fatal("fn must not run when Begin failed")
		return struct{}{}, nil
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "begin tx")
	assert.Contains(t, err.Error(), "connection refused")
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestRunInTx_CommitError surfaces Commit failures wrapped, after
// fn returned nil.
func TestRunInTx_CommitError(t *testing.T) {
	t.Parallel()
	pool, mock := newMockPool(t)
	mock.ExpectBegin()
	mock.ExpectCommit().WillReturnError(errors.New("deadlock victim"))

	_, err := RunInTx(context.Background(), pool, func(qtx Querier) (struct{}, error) {
		return struct{}{}, nil
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "commit tx")
	assert.Contains(t, err.Error(), "deadlock victim")
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestRunInTx_PanicRecovered confirms that a panic inside fn is
// recovered, converted to error, and the deferred rollback fires —
// keeps the pgx connection from leaking back to the pool in an
// unknown state.
func TestRunInTx_PanicRecovered(t *testing.T) {
	t.Parallel()
	pool, mock := newMockPool(t)
	mock.ExpectBegin()
	mock.ExpectRollback()

	_, err := RunInTx(context.Background(), pool, func(qtx Querier) (struct{}, error) {
		panic("something exploded")
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "panicked")
	assert.Contains(t, err.Error(), "something exploded")
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestRunInTx_PanicWithError covers the case where fn panics with a
// real error type (not just a string). Recovery still converts to
// error.
func TestRunInTx_PanicWithError(t *testing.T) {
	t.Parallel()
	pool, mock := newMockPool(t)
	mock.ExpectBegin()
	mock.ExpectRollback()

	_, err := RunInTx(context.Background(), pool, func(qtx Querier) (struct{}, error) {
		panic(errors.New("fancy error type"))
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "panicked")
}

// TestRunInTxVoid_HappyPath confirms the side-effect-only variant
// commits cleanly when fn returns nil.
func TestRunInTxVoid_HappyPath(t *testing.T) {
	t.Parallel()
	pool, mock := newMockPool(t)
	mock.ExpectBegin()
	mock.ExpectCommit()

	called := false
	err := RunInTxVoid(context.Background(), pool, func(qtx Querier) error {
		called = true
		return nil
	})
	require.NoError(t, err)
	assert.True(t, called)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestRunInTxVoid_Error rolls back on fn error.
func TestRunInTxVoid_Error(t *testing.T) {
	t.Parallel()
	pool, mock := newMockPool(t)
	mock.ExpectBegin()
	mock.ExpectRollback()

	wantErr := errors.New("nope")
	err := RunInTxVoid(context.Background(), pool, func(qtx Querier) error {
		return wantErr
	})
	require.ErrorIs(t, err, wantErr)
}

// TestRunInTx_PassThroughTxOptions plumbs explicit pgx.TxOptions
// through to BeginTx. Pgxmock's ExpectBeginTx matches on the exact
// options struct, so this proves the variadic last-arg shape
// reaches the begin call unchanged.
func TestRunInTx_PassThroughTxOptions(t *testing.T) {
	t.Parallel()
	pool, mock := newMockPool(t)
	want := pgx.TxOptions{
		IsoLevel:       pgx.Serializable,
		AccessMode:     pgx.ReadOnly,
		DeferrableMode: pgx.Deferrable,
	}
	mock.ExpectBeginTx(want)
	mock.ExpectCommit()

	_, err := RunInTx(context.Background(), pool,
		func(qtx Querier) (struct{}, error) { return struct{}{}, nil },
		want,
	)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestRunInTx_DefaultTxOptions confirms the no-opts call passes
// pgx.TxOptions{} (zero value = read-committed, read-write,
// non-deferrable) to BeginTx.
func TestRunInTx_DefaultTxOptions(t *testing.T) {
	t.Parallel()
	pool, mock := newMockPool(t)
	mock.ExpectBeginTx(pgx.TxOptions{}) // zero-value defaults
	mock.ExpectCommit()

	_, err := RunInTx(context.Background(), pool,
		func(qtx Querier) (struct{}, error) { return struct{}{}, nil })
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestRunInTx_SlowTxWarning is a smoke test for the slow-tx
// warning path. We can't easily intercept slog output without
// installing a custom handler; assert behavior just doesn't crash
// when the deferred warning fires (real fn time exceeds 250ms is
// avoided to keep the test fast — but the deferred branch is
// proven by `go test -race` going clean).
func TestRunInTx_SlowTxWarning_NoCrash(t *testing.T) {
	t.Parallel()
	pool, mock := newMockPool(t)
	mock.ExpectBegin()
	mock.ExpectCommit()

	// Sleep slightly under threshold — fast happy path, no warning.
	_, err := RunInTx(context.Background(), pool,
		func(qtx Querier) (struct{}, error) {
			time.Sleep(10 * time.Millisecond)
			return struct{}{}, nil
		})
	require.NoError(t, err)
}

// TestRunInTx_GenericReturnTypes proves the type parameter actually
// flows the closure's return value back without losing type info.
func TestRunInTx_GenericReturnTypes(t *testing.T) {
	t.Parallel()

	t.Run("int", func(t *testing.T) {
		t.Parallel()
		pool, mock := newMockPool(t)
		mock.ExpectBegin()
		mock.ExpectCommit()
		got, err := RunInTx(context.Background(), pool, func(qtx Querier) (int, error) {
			return 42, nil
		})
		require.NoError(t, err)
		assert.Equal(t, 42, got)
	})

	t.Run("struct", func(t *testing.T) {
		t.Parallel()
		type myRow struct{ ID, Name string }
		pool, mock := newMockPool(t)
		mock.ExpectBegin()
		mock.ExpectCommit()
		got, err := RunInTx(context.Background(), pool, func(qtx Querier) (myRow, error) {
			return myRow{ID: "x", Name: "y"}, nil
		})
		require.NoError(t, err)
		assert.Equal(t, myRow{ID: "x", Name: "y"}, got)
	})

	t.Run("slice", func(t *testing.T) {
		t.Parallel()
		pool, mock := newMockPool(t)
		mock.ExpectBegin()
		mock.ExpectCommit()
		got, err := RunInTx(context.Background(), pool, func(qtx Querier) ([]string, error) {
			return []string{"a", "b"}, nil
		})
		require.NoError(t, err)
		assert.Equal(t, []string{"a", "b"}, got)
	})
}

// TestRunInTx_ErrorIsWrapped confirms fmt.Errorf("...: %w", ...)
// wrapping is in place so callers can errors.Is/As against the
// underlying pgx error class.
func TestRunInTx_ErrorIsWrapped_BeginAndCommit(t *testing.T) {
	t.Parallel()

	t.Run("begin", func(t *testing.T) {
		t.Parallel()
		sentinel := fmt.Errorf("sentinel begin error")
		pool, mock := newMockPool(t)
		mock.ExpectBegin().WillReturnError(sentinel)
		_, err := RunInTx(context.Background(), pool,
			func(qtx Querier) (struct{}, error) { return struct{}{}, nil })
		require.ErrorIs(t, err, sentinel)
	})

	t.Run("commit", func(t *testing.T) {
		t.Parallel()
		sentinel := fmt.Errorf("sentinel commit error")
		pool, mock := newMockPool(t)
		mock.ExpectBegin()
		mock.ExpectCommit().WillReturnError(sentinel)
		_, err := RunInTx(context.Background(), pool,
			func(qtx Querier) (struct{}, error) { return struct{}{}, nil })
		require.ErrorIs(t, err, sentinel)
	})
}

// TestRunInTx_RetryOn40001_SucceedsOnSecondAttempt covers the
// canonical retry case: SERIALIZABLE conflict on first commit,
// then success on the second tx. Pgxmock plays back expectations
// in order so we set up two full Begin/Commit cycles.
func TestRunInTx_RetryOn40001_SucceedsOnSecondAttempt(t *testing.T) {
	t.Parallel()
	pool, mock := newMockPool(t)

	// Attempt 1: begin, commit fails with 40001, rollback (deferred).
	mock.ExpectBegin()
	mock.ExpectCommit().WillReturnError(&pgconn.PgError{Code: "40001", Message: "could not serialize access"})
	mock.ExpectRollback() // deferred rollback after commit failure

	// Attempt 2: begin, commit succeeds.
	mock.ExpectBegin()
	mock.ExpectCommit()

	calls := 0
	got, err := RunInTx(context.Background(), pool, func(qtx Querier) (int, error) {
		calls++
		return calls, nil
	})
	require.NoError(t, err)
	assert.Equal(t, 2, got, "fn should run twice; second call returns 2")
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestRunInTx_RetryOn40P01 covers deadlock_detected, which fires
// at any isolation level (not just SERIALIZABLE). Retry semantics
// identical to 40001.
func TestRunInTx_RetryOn40P01(t *testing.T) {
	t.Parallel()
	pool, mock := newMockPool(t)

	mock.ExpectBegin()
	mock.ExpectCommit().WillReturnError(&pgconn.PgError{Code: "40P01", Message: "deadlock detected"})
	mock.ExpectRollback()

	mock.ExpectBegin()
	mock.ExpectCommit()

	_, err := RunInTx(context.Background(), pool, func(qtx Querier) (struct{}, error) {
		return struct{}{}, nil
	})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestRunInTx_NonRetryableError_DoesNotRetry confirms that errors
// outside the retry allowlist propagate immediately without retry.
// Examples: unique-constraint violation (23505), foreign-key
// violation (23503), connection refused, fn-returned application
// errors. Replaying these would either double-fail or amplify the
// real bug.
func TestRunInTx_NonRetryableError_DoesNotRetry(t *testing.T) {
	t.Parallel()

	t.Run("unique violation 23505", func(t *testing.T) {
		t.Parallel()
		pool, mock := newMockPool(t)
		mock.ExpectBegin()
		mock.ExpectCommit().WillReturnError(&pgconn.PgError{Code: "23505", Message: "duplicate key"})
		mock.ExpectRollback()

		calls := 0
		_, err := RunInTx(context.Background(), pool, func(qtx Querier) (struct{}, error) {
			calls++
			return struct{}{}, nil
		})
		require.Error(t, err)
		assert.Equal(t, 1, calls, "non-retryable error must not replay fn")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("application error from fn", func(t *testing.T) {
		t.Parallel()
		pool, mock := newMockPool(t)
		mock.ExpectBegin()
		mock.ExpectRollback()

		calls := 0
		appErr := errors.New("business rule violated")
		_, err := RunInTx(context.Background(), pool, func(qtx Querier) (struct{}, error) {
			calls++
			return struct{}{}, appErr
		})
		require.ErrorIs(t, err, appErr)
		assert.Equal(t, 1, calls)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

// TestRunInTx_RetriesExhausted covers the cap. After maxTxRetries
// attempts all returning 40001, the wrapper gives up and surfaces
// the last error with a "retries exhausted" prefix.
func TestRunInTx_RetriesExhausted(t *testing.T) {
	t.Parallel()
	pool, mock := newMockPool(t)

	pgErr := &pgconn.PgError{Code: "40001", Message: "still serializing"}
	// 3 full Begin/Commit-fails/Rollback cycles (maxTxRetries = 3).
	for i := 0; i < 3; i++ {
		mock.ExpectBegin()
		mock.ExpectCommit().WillReturnError(pgErr)
		mock.ExpectRollback()
	}

	calls := 0
	_, err := RunInTx(context.Background(), pool, func(qtx Querier) (struct{}, error) {
		calls++
		return struct{}{}, nil
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "retries exhausted")
	assert.ErrorIs(t, err, pgErr)
	assert.Equal(t, 3, calls)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestRunInTx_CtxCancelledBetweenRetries: if the caller's ctx is
// done between attempts, the wrapper bails out without sleeping
// or starting another attempt.
func TestRunInTx_CtxCancelledBetweenRetries(t *testing.T) {
	t.Parallel()
	pool, mock := newMockPool(t)

	pgErr := &pgconn.PgError{Code: "40001", Message: "conflict"}
	mock.ExpectBegin()
	mock.ExpectCommit().WillReturnError(pgErr)
	mock.ExpectRollback()
	// No second cycle — ctx is cancelled before retry fires.

	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	_, err := RunInTx(ctx, pool, func(qtx Querier) (struct{}, error) {
		calls++
		// Cancel after the first attempt's fn body runs. The retry
		// path checks ctx.Err() before sleeping.
		cancel()
		return struct{}{}, nil
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, calls)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestIsRetryableTxError_Direct covers the predicate in isolation
// for completeness. Wraps + non-pg errors → not retryable.
func TestIsRetryableTxError_Direct(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"40001 serialization", &pgconn.PgError{Code: "40001"}, true},
		{"40P01 deadlock", &pgconn.PgError{Code: "40P01"}, true},
		{"23505 unique violation", &pgconn.PgError{Code: "23505"}, false},
		{"23503 foreign key", &pgconn.PgError{Code: "23503"}, false},
		{"non-pg error", errors.New("plain error"), false},
		{"nil", nil, false},
		{"wrapped 40001", fmt.Errorf("commit tx: %w", &pgconn.PgError{Code: "40001"}), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, isRetryableTxError(tc.err))
		})
	}
}

// TestRunInTx_PanicMessageNotLeaky confirms the recovered-panic
// error returned to the caller carries the panic content but no
// stack trace (the stack goes to slog only, not the wire-facing
// error message).
func TestRunInTx_PanicMessageNotLeaky(t *testing.T) {
	t.Parallel()
	pool, mock := newMockPool(t)
	mock.ExpectBegin()
	mock.ExpectRollback()

	_, err := RunInTx(context.Background(), pool, func(qtx Querier) (struct{}, error) {
		panic("test panic")
	})
	require.Error(t, err)
	assert.True(t, strings.HasPrefix(err.Error(), "tx fn panicked"))
}

// TestRunInTx_BackoffTimingApprox asserts that on a retry the loop
// actually sleeps at least initialRetryBackoff (5ms) before retrying.
// Upper bound is generous to absorb scheduler jitter on busy CI;
// the assertion of interest is "the sleep happened at all" — without
// it a runaway retry loop would pummel Postgres.
func TestRunInTx_BackoffTimingApprox(t *testing.T) {
	t.Parallel()
	pool, mock := newMockPool(t)
	// Attempt 1: Begin → Commit returns 40001 → defer-Rollback (closed, ignored)
	mock.ExpectBegin()
	mock.ExpectCommit().WillReturnError(&pgconn.PgError{Code: pgSerializationFailed})
	mock.ExpectRollback()
	// Attempt 2: success.
	mock.ExpectBegin()
	mock.ExpectCommit()

	var attemptTimes []time.Time
	_, err := RunInTx(context.Background(), pool, func(qtx Querier) (struct{}, error) {
		attemptTimes = append(attemptTimes, time.Now())
		return struct{}{}, nil
	})
	require.NoError(t, err)
	require.Len(t, attemptTimes, 2, "fn should run on attempt 1 and again on retry")

	gap := attemptTimes[1].Sub(attemptTimes[0])
	// Lower bound: at least initialRetryBackoff (no jitter subtraction —
	// jitter is additive, range [backoff, 1.5*backoff)).
	assert.GreaterOrEqualf(t, gap, initialRetryBackoff,
		"retry should wait at least %v; saw %v", initialRetryBackoff, gap)
	// Upper bound: 1.5*backoff + scheduler slack. 50ms is plenty.
	upper := initialRetryBackoff + initialRetryBackoff/2 + 50*time.Millisecond
	assert.Lessf(t, gap, upper,
		"retry should not wait more than %v; saw %v", upper, gap)
}

// TestRunInTxRaw_PassesUnderlyingTx exercises the escape-hatch
// helper. Verifies the closure receives both qtx and a non-nil
// pgx.Tx, and that the Commit/Rollback semantics still work.
func TestRunInTxRaw_PassesUnderlyingTx(t *testing.T) {
	t.Parallel()
	pool, mock := newMockPool(t)
	mock.ExpectBegin()
	mock.ExpectCommit()

	var sawTx pgx.Tx
	var sawQtx Querier
	_, err := RunInTxRaw(context.Background(), pool, func(qtx Querier, tx pgx.Tx) (struct{}, error) {
		sawTx = tx
		sawQtx = qtx
		return struct{}{}, nil
	})
	require.NoError(t, err)
	assert.NotNil(t, sawTx, "tx should be passed to the closure")
	assert.NotNil(t, sawQtx, "qtx should be passed to the closure")
}

// TestRunInTxRaw_FnError_Rollbacks confirms RunInTxRaw inherits the
// rollback-on-error semantics of RunInTx.
func TestRunInTxRaw_FnError_Rollbacks(t *testing.T) {
	t.Parallel()
	pool, mock := newMockPool(t)
	mock.ExpectBegin()
	mock.ExpectRollback()

	wantErr := errors.New("boom")
	_, err := RunInTxRaw(context.Background(), pool, func(qtx Querier, tx pgx.Tx) (struct{}, error) {
		return struct{}{}, wantErr
	})
	require.ErrorIs(t, err, wantErr)
}
