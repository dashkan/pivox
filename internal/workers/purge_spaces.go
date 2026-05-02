package workers

import (
	"context"
	"log/slog"

	"github.com/riverqueue/river"

	db "github.com/dashkan/pivox/internal/db/generated"
)

// PurgeSpacesArgs is the (empty) arg struct for the space-purge
// periodic job. Kind string is the on-disk dispatch key — wire
// contract.
type PurgeSpacesArgs struct{}

// Kind implements river.JobArgs.
func (PurgeSpacesArgs) Kind() string { return "purge_spaces" }

// PurgeSpacesWorker hard-deletes soft-deleted spaces whose 30-day
// grace window has elapsed. FK ON DELETE CASCADE removes
// space_members, assets, and asset_requests transitively. Replaces
// the pre-River SpacePurgeWorker (deleted in cutover) — same SQL,
// periodic invocation driven by River's scheduler.
type PurgeSpacesWorker struct {
	river.WorkerDefaults[PurgeSpacesArgs]

	Queries db.Querier
	Logger  *slog.Logger
}

// Work implements river.Worker[PurgeSpacesArgs]. Per-row purge
// failures are logged and skipped (next periodic tick retries);
// list-time errors propagate so River applies its retry schedule.
func (w *PurgeSpacesWorker) Work(ctx context.Context, _ *river.Job[PurgeSpacesArgs]) error {
	spaces, err := w.Queries.ListSpacesPastPurgeTime(ctx)
	if err != nil {
		return err
	}
	if len(spaces) == 0 {
		return nil
	}
	w.Logger.InfoContext(ctx, "purge_spaces: cascading spaces past grace window", "count", len(spaces))
	for _, sp := range spaces {
		// PurgeExpiredSpace is the race-safe variant: only fires on
		// a row that's still soft-deleted with an elapsed
		// purge_time. Concurrent UndeleteSpace is absorbed.
		if err := w.Queries.PurgeExpiredSpace(ctx, sp.ID); err != nil {
			w.Logger.ErrorContext(ctx, "purge_spaces: PurgeExpiredSpace failed",
				"space_id", sp.ID, "name", sp.Name, "error", err)
			continue
		}
		w.Logger.InfoContext(ctx, "purge_spaces: space cascaded", "space_id", sp.ID, "name", sp.Name)
	}
	return nil
}
