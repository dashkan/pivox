package workers

import (
	"context"
	"log/slog"

	"github.com/riverqueue/river"

	db "github.com/dashkan/pivox/internal/db/generated"
)

// ReapOperationsArgs is the (empty) arg struct for the LRO reaper
// periodic job. Kind is the on-disk dispatch key — wire contract.
type ReapOperationsArgs struct{}

// Kind implements river.JobArgs.
func (ReapOperationsArgs) Kind() string { return "reap_operations" }

// ReapOperationsWorker deletes expired operation rows. Replaces
// the pre-River lro.Reaper (deleted in cutover) — same single SQL,
// periodic invocation driven by River's scheduler.
type ReapOperationsWorker struct {
	river.WorkerDefaults[ReapOperationsArgs]

	Queries db.Querier
	Logger  *slog.Logger
}

// Work implements river.Worker[ReapOperationsArgs]. Errors propagate
// so River applies its retry schedule.
func (w *ReapOperationsWorker) Work(ctx context.Context, _ *river.Job[ReapOperationsArgs]) error {
	if err := w.Queries.DeleteExpiredOperations(ctx); err != nil {
		w.Logger.ErrorContext(ctx, "reap_operations: DeleteExpiredOperations failed", "error", err)
		return err
	}
	return nil
}
