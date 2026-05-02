package workers

import (
	"context"
	"errors"
	"log/slog"

	"github.com/riverqueue/river"

	db "github.com/dashkan/pivox/internal/db/generated"
)

// CleanupAuthArgs is the (empty) arg struct for the auth-artifacts
// cleanup periodic job. Kind is the on-disk dispatch key — wire
// contract.
type CleanupAuthArgs struct{}

// Kind implements river.JobArgs.
func (CleanupAuthArgs) Kind() string { return "cleanup_auth" }

// CleanupAuthWorker deletes expired auth_token_codes and
// delegated_auth_sessions every tick. Replaces the pre-River
// inline goroutine in cmd/pivox-cloud/main.go (deleted in cutover)
// — same two SQL calls, periodic invocation driven by River.
//
// The two cleanups are independent: a failure on one does NOT
// suppress the other (matches pre-River inline behavior). Errors
// from either are joined and returned so River sees the failure
// and retries on its schedule, but both calls were attempted.
type CleanupAuthWorker struct {
	river.WorkerDefaults[CleanupAuthArgs]

	Queries db.Querier
	Logger  *slog.Logger
}

// Work implements river.Worker[CleanupAuthArgs].
func (w *CleanupAuthWorker) Work(ctx context.Context, _ *river.Job[CleanupAuthArgs]) error {
	var errs []error
	if err := w.Queries.DeleteExpiredAuthTokenCodes(ctx); err != nil {
		w.Logger.ErrorContext(ctx, "cleanup_auth: DeleteExpiredAuthTokenCodes failed", "error", err)
		errs = append(errs, err)
	}
	if err := w.Queries.DeleteExpiredDelegatedAuthSessions(ctx); err != nil {
		w.Logger.ErrorContext(ctx, "cleanup_auth: DeleteExpiredDelegatedAuthSessions failed", "error", err)
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}
