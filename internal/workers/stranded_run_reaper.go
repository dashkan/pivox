package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/dashkan/pivox/internal/engine/runjob"
)

// A workflow run must never dangle in a non-terminal state (PENDING/RUNNING)
// after the River job backing it gives up. The executor (RunWorkflowWorker)
// already finalizes a run FAILED on a *terminal* activity error, but the
// exhausted-*retryable* path is the gap: the worker keeps returning a retryable
// error, River retries to MaxAttempts and then DISCARDS the job, leaving the run
// non-terminal forever. This file closes that gap two ways, both reusing the
// executor's finalizeRunFailed path (no duplicated finalize logic):
//
//   - RunWorkflowErrorHandler — the immediate path. A River ErrorHandler that
//     finalizes the run the moment its job's final attempt errors or panics.
//   - ReapStrandedRunsWorker — the backstop. A periodic job that finalizes any
//     already-DISCARDED workflow_run job whose run is still non-terminal, catching
//     what the handler missed (handler not invoked, process crash, DB write failed
//     mid-finalize).

// runWorkflowJobKind is the River job kind the reaper and handler act on. Cached
// from the Args value method so the string literal lives in exactly one place.
var runWorkflowJobKind = runjob.Args{}.Kind()

// RunWorkflowErrorHandler is River's ErrorHandler for the workflow-run executor.
// On a workflow_run job's FINAL attempt (the one that trips River's discard) it
// finalizes the run FAILED via the shared finalizeRunFailed path. Non-final
// attempts and non-workflow_run jobs are ignored, and it never alters River's
// retry/discard decision (both handlers return a nil result) — recording the
// run's terminal state is a pure side effect.
type RunWorkflowErrorHandler struct {
	// Pool is the database pool. Required.
	Pool *pgxpool.Pool
	// Logger is the structured logger. Required.
	Logger *slog.Logger
}

var _ river.ErrorHandler = (*RunWorkflowErrorHandler)(nil)

// HandleError finalizes a workflow run FAILED when its job's final attempt
// errors (River is about to discard it). Returns nil so River's decision stands.
func (h *RunWorkflowErrorHandler) HandleError(ctx context.Context, job *rivertype.JobRow, err error) *river.ErrorHandlerResult {
	h.finalizeIfExhausted(ctx, job, err)
	return nil
}

// HandlePanic finalizes a workflow run FAILED when a panic exhausts the job's
// attempts — a panic on the final attempt strands the run exactly as a returned
// error does. Returns nil so River's decision stands.
func (h *RunWorkflowErrorHandler) HandlePanic(ctx context.Context, job *rivertype.JobRow, panicVal any, _ string) *river.ErrorHandlerResult {
	h.finalizeIfExhausted(ctx, job, fmt.Errorf("workflow run panicked: %v", panicVal))
	return nil
}

// finalizeIfExhausted marks the run FAILED iff this is a workflow_run job on its
// final attempt. job.Attempt is the attempt that just failed; when it reaches
// MaxAttempts River discards the job, so Attempt >= MaxAttempts identifies the
// last attempt. On earlier attempts it does nothing, leaving River's retry
// schedule intact. finalizeRunFailed no-ops on an already-terminal/CANCELLED run,
// so a concurrent success or business cancel is never clobbered.
func (h *RunWorkflowErrorHandler) finalizeIfExhausted(ctx context.Context, job *rivertype.JobRow, cause error) {
	if job.Kind != runWorkflowJobKind {
		return
	}
	if job.Attempt < job.MaxAttempts {
		return // attempts remain; let River retry
	}
	runID, ok := decodeRunID(ctx, job, h.Logger)
	if !ok {
		return
	}
	log := h.Logger.With("run_id", runID, "job_id", job.ID, "attempt", job.Attempt)
	finalized, err := finalizeRunFailed(ctx, h.Pool, runID, nil, cause, log)
	if err != nil {
		// The periodic reaper is the backstop: the run stays stranded until then.
		log.ErrorContext(ctx, "run reaper: finalize FAILED on discarded job failed", "error", err)
		return
	}
	if finalized {
		log.WarnContext(ctx, "run reaper: finalized run FAILED after job exhausted retries", "cause", cause)
	}
}

// ReapStrandedRunsArgs is the (empty) arg struct for the stranded-run reaper
// periodic job. Kind is the on-disk dispatch key — wire contract.
type ReapStrandedRunsArgs struct{}

// Kind implements river.JobArgs.
func (ReapStrandedRunsArgs) Kind() string { return "reap_stranded_runs" }

const (
	// defaultReapStrandedRunsMax bounds how many discarded jobs one reaper tick
	// processes, so a large discard backlog can't make a single tick run
	// unboundedly long. A tick that hits the cap logs a warning; the next tick
	// continues where it left off (JobList is ordered by ascending id and finalize
	// is idempotent, so re-scanning is safe and cheap).
	defaultReapStrandedRunsMax = 1000
	// reapStrandedRunsPageSize is the JobList page size the reaper paginates in.
	reapStrandedRunsPageSize = 100
)

// JobLister is the slice of the River client the reaper depends on: listing jobs
// by state + kind. Both *river.Client and *riverpro.Client satisfy it. Declared
// as an interface so the reaper's core is testable with a real client passed in.
type JobLister interface {
	JobList(ctx context.Context, params *river.JobListParams) (*river.JobListResult, error)
}

// ReapStrandedRunsWorker is the periodic backstop for run stranding. Each tick it
// lists DISCARDED workflow_run jobs and finalizes any whose run is still
// non-terminal, catching the runs RunWorkflowErrorHandler missed.
//
// It targets DISCARDED jobs only, which is why it does not double-handle with
// River's built-in JobRescuer: the rescuer re-queues or discards jobs stuck
// RUNNING past their timeout on a crashed worker — it acts on *running* jobs. A
// crashed-mid-execution run is therefore rescued (re-run) or eventually discarded
// by River first; only once a job is DISCARDED does this reaper consider its run
// stranded. The two never contend for the same job.
type ReapStrandedRunsWorker struct {
	river.WorkerDefaults[ReapStrandedRunsArgs]

	// Pool is the database pool used to finalize stranded runs. Required.
	Pool *pgxpool.Pool
	// Logger is the structured logger. Required.
	Logger *slog.Logger
	// MaxPerTick bounds discarded jobs processed per tick. <= 0 uses the default.
	MaxPerTick int
}

var _ river.Worker[ReapStrandedRunsArgs] = (*ReapStrandedRunsWorker)(nil)

// Work resolves the River client from the worker context — the blessed way to
// reach the client inside a Work method — and delegates to reapStrandedRuns.
func (w *ReapStrandedRunsWorker) Work(ctx context.Context, _ *river.Job[ReapStrandedRunsArgs]) error {
	client, err := river.ClientFromContextSafely[pgx.Tx](ctx)
	if err != nil {
		return fmt.Errorf("reap stranded runs: resolve river client: %w", err)
	}
	return reapStrandedRuns(ctx, client, w.Pool, w.MaxPerTick, w.Logger)
}

// reapStrandedRuns paginates DISCARDED workflow_run jobs and finalizes each whose
// run is still non-terminal, up to maxPerTick jobs. It is the testable core of
// ReapStrandedRunsWorker.Work, taking the JobLister explicitly. A per-run
// finalize failure is logged and skipped (the next tick retries it — finalize is
// idempotent) rather than aborting the whole tick.
func reapStrandedRuns(ctx context.Context, jobs JobLister, pool *pgxpool.Pool, maxPerTick int, log *slog.Logger) error {
	if maxPerTick <= 0 {
		maxPerTick = defaultReapStrandedRunsMax
	}

	var cursor *river.JobListCursor
	scanned, reaped := 0, 0
	hitCap := false
	for {
		remaining := maxPerTick - scanned
		if remaining <= 0 {
			hitCap = true
			break
		}
		want := min(reapStrandedRunsPageSize, remaining)
		params := river.NewJobListParams().
			States(rivertype.JobStateDiscarded).
			Kinds(runWorkflowJobKind).
			OrderBy(river.JobListOrderByID, river.SortOrderAsc).
			First(want)
		if cursor != nil {
			params = params.After(cursor)
		}
		res, err := jobs.JobList(ctx, params)
		if err != nil {
			return fmt.Errorf("list discarded workflow_run jobs: %w", err)
		}
		for _, job := range res.Jobs {
			scanned++
			if reapOne(ctx, pool, job, log) {
				reaped++
			}
		}
		// Fewer jobs than requested (or no cursor) means the discard queue is
		// exhausted. A full page that was capped by `remaining` falls through to
		// the remaining<=0 guard on the next iteration, which sets hitCap.
		if len(res.Jobs) < want || res.LastCursor == nil {
			break
		}
		cursor = res.LastCursor
	}

	if reaped > 0 {
		log.InfoContext(ctx, "run reaper: finalized stranded runs", "reaped", reaped, "scanned", scanned)
	}
	if hitCap {
		// Never truncate silently: the next tick continues from ascending id.
		log.WarnContext(ctx, "run reaper: hit per-tick cap; remaining discarded jobs (if any) handled next tick",
			"cap", maxPerTick, "reaped", reaped)
	}
	return nil
}

// reapOne finalizes a single discarded job's run FAILED, returning whether it
// actually wrote (false when the run was already terminal/gone or the args
// couldn't be decoded).
func reapOne(ctx context.Context, pool *pgxpool.Pool, job *rivertype.JobRow, log *slog.Logger) bool {
	runID, ok := decodeRunID(ctx, job, log)
	if !ok {
		return false
	}
	rlog := log.With("run_id", runID, "job_id", job.ID)
	finalized, err := finalizeRunFailed(ctx, pool, runID, nil, errRunJobDiscarded, rlog)
	if err != nil {
		// One run's failure must not abort the tick; the next tick retries it.
		rlog.ErrorContext(ctx, "run reaper: finalize stranded run failed", "error", err)
		return false
	}
	if finalized {
		rlog.WarnContext(ctx, "run reaper: finalized stranded run FAILED (job discarded after exhausting retries)")
	}
	return finalized
}

// errRunJobDiscarded is the cause recorded on a run finalized by the periodic
// reaper — the run's job was discarded after exhausting its retries.
var errRunJobDiscarded = fmt.Errorf("run job discarded after exhausting retries")

// decodeRunID pulls the RunID out of a workflow_run job's encoded args. A decode
// failure is logged (the run may stay stranded) and reported as not-ok rather
// than aborting the caller — one malformed job must not stop the reaper.
func decodeRunID(ctx context.Context, job *rivertype.JobRow, log *slog.Logger) (uuid.UUID, bool) {
	var args runjob.Args
	if err := json.Unmarshal(job.EncodedArgs, &args); err != nil {
		log.ErrorContext(ctx, "run reaper: cannot decode workflow_run job args; run may be stranded",
			"job_id", job.ID, "error", err)
		return uuid.Nil, false
	}
	return args.RunID, true
}
