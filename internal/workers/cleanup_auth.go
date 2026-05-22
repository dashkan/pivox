package workers

import (
	"context"
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

// CleanupAuthWorker deletes expired delegated_auth_sessions every
// tick. Replaces the pre-River inline goroutine in
// cmd/pivox-cloud/main.go — same SQL call, periodic invocation
// driven by River.
//
// This worker previously also reaped auth_token_codes; that table
// backed the Electron custom-token bridge, which was removed when
// auth moved to the OAuth broker.
type CleanupAuthWorker struct {
	river.WorkerDefaults[CleanupAuthArgs]

	Queries db.Querier
	Logger  *slog.Logger
}

// Work implements river.Worker[CleanupAuthArgs].
func (w *CleanupAuthWorker) Work(ctx context.Context, _ *river.Job[CleanupAuthArgs]) error {
	if err := w.Queries.DeleteExpiredDelegatedAuthSessions(ctx); err != nil {
		w.Logger.ErrorContext(ctx, "cleanup_auth: DeleteExpiredDelegatedAuthSessions failed", "error", err)
		return err
	}
	return nil
}
