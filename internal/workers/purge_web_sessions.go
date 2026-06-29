package workers

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
)

// PurgeWebSessionsArgs is the (empty) arg struct for the web-session
// purge periodic job. River requires JobArgs implement Kind(); the
// kind string is the on-disk dispatch key, so changing it without
// coordinating a migration would orphan in-flight rows. Treat as the
// wire contract it is.
type PurgeWebSessionsArgs struct{}

// Kind implements river.JobArgs.
func (PurgeWebSessionsArgs) Kind() string { return "purge_web_sessions" }

// PurgeWebSessionsWorker garbage-collects expired rows from the
// `web_sessions` table — the server-side session store owned by the
// TanStack Start BFF. The BFF also lazy-expires on read (a stale row
// is never honoured even between ticks), so this job is pure GC: it
// reclaims rows for sessions that were never read again before their
// 30-day purge horizon elapsed.
//
// The table is BFF-owned and lives in its own `sessions` database (no
// Go migrations, no sqlc query or Go model — the gRPC backend never
// reads it), so this worker holds a pool for the sessions DB (distinct
// from the app `pivox` pool) and issues raw SQL. The DELETE is a single
// statement, so it needs no transaction.
//
// Multi-replica safety is delegated to River's leader election (only
// one replica's worker picks up each periodic tick).
type PurgeWebSessionsWorker struct {
	river.WorkerDefaults[PurgeWebSessionsArgs]

	Pool   *pgxpool.Pool
	Logger *slog.Logger
}

// Work implements river.Worker[PurgeWebSessionsArgs]. A delete-time
// error propagates so River records the job as errored and applies
// its own retry schedule; the next tick would also retry.
func (w *PurgeWebSessionsWorker) Work(ctx context.Context, _ *river.Job[PurgeWebSessionsArgs]) error {
	tag, err := w.Pool.Exec(ctx, `DELETE FROM web_sessions WHERE expires_at < now()`)
	if err != nil {
		// The BFF creates web_sessions lazily on its first session op. If this
		// purge tick beats the BFF on a cold start, the table won't exist yet —
		// "42P01 undefined_table" just means there's nothing to GC, so no-op
		// rather than erroring (it self-heals once the BFF runs). Any other
		// error propagates for River to retry.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "42P01" {
			return nil
		}
		w.Logger.ErrorContext(ctx, "purge_web_sessions: delete failed", "error", err)
		return err
	}
	if n := tag.RowsAffected(); n > 0 {
		w.Logger.InfoContext(ctx, "purge_web_sessions: deleted expired sessions", "count", n)
	}
	return nil
}
