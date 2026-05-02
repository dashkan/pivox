package workers

import (
	"context"
	"log/slog"

	"github.com/riverqueue/river"

	db "github.com/dashkan/pivox/internal/db/generated"
)

// PurgeOrgsArgs is the (empty) arg struct for the org-purge periodic
// job. River requires JobArgs implement Kind(); the kind string is
// the on-disk dispatch key, so changing it without coordinating a
// migration would orphan in-flight rows. Treat as the wire contract
// it is.
type PurgeOrgsArgs struct{}

// Kind implements river.JobArgs.
func (PurgeOrgsArgs) Kind() string { return "purge_orgs" }

// PurgeOrgsWorker handles purge_orgs jobs. Each invocation lists
// soft-deleted orgs whose 30-day grace window has elapsed and
// hard-deletes them; FK ON DELETE CASCADE removes spaces, members,
// domains, SSO config, assets, requests, tags, API keys, and AI
// conversations transitively. Replaces the pre-River PurgeWorker
// (deleted in the cutover commit) — same SQL, periodic invocation
// driven by River's scheduler instead of a hand-rolled tick + lock.
//
// Multi-replica safety is delegated to River's leader election
// (only one replica's worker will pick up each periodic tick).
type PurgeOrgsWorker struct {
	river.WorkerDefaults[PurgeOrgsArgs]

	Queries db.Querier
	Logger  *slog.Logger
}

// Work implements river.Worker[PurgeOrgsArgs]. Per-row purge
// failures are logged and skipped — a single stuck row shouldn't
// stall the rest of the batch and the next periodic tick will retry
// it. List-time errors propagate so River records the job as
// errored and applies its own retry schedule.
func (w *PurgeOrgsWorker) Work(ctx context.Context, _ *river.Job[PurgeOrgsArgs]) error {
	orgs, err := w.Queries.ListOrgsPastPurgeTime(ctx)
	if err != nil {
		return err
	}
	if len(orgs) == 0 {
		return nil
	}
	w.Logger.InfoContext(ctx, "purge_orgs: cascading orgs past grace window", "count", len(orgs))
	for _, o := range orgs {
		// PurgeExpiredOrganization is the race-safe variant: it only
		// fires on a row that's still soft-deleted with an elapsed
		// purge_time. A concurrent UndeleteOrganization between the
		// list and this delete is absorbed (no rows affected, no
		// error from :exec).
		if err := w.Queries.PurgeExpiredOrganization(ctx, o.ID); err != nil {
			w.Logger.ErrorContext(ctx, "purge_orgs: PurgeExpiredOrganization failed",
				"org", o.Name, "error", err)
			continue
		}
		w.Logger.InfoContext(ctx, "purge_orgs: org cascaded", "org", o.Name)
	}
	return nil
}
