package workflows_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"riverqueue.com/riverpro"

	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/engine/runjob"
	workflowsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/workflows/v1"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
	"github.com/dashkan/pivox/internal/workers"
)

// seededRun creates an org + promoted workflow and a PENDING run (which also
// enqueues one workflow_run river_job), returning the run's full resource name,
// its uuid, and the runs client.
func seededRun(t *testing.T, ctx context.Context, h *grpcharness.Harness, slug string) (runName string, runID uuid.UUID, runClient workflowsv1.WorkflowRunsClient) {
	t.Helper()
	owned := h.SeedOwnedOrg(t, slug, "Stranded Co", "workflows")
	wfClient := workflowsv1.NewWorkflowsClient(h.Conn())
	verClient := workflowsv1.NewWorkflowVersionsClient(h.Conn())
	runClient = workflowsv1.NewWorkflowRunsClient(h.Conn())
	wf, _ := newPromotedWorkflow(t, ctx, wfClient, verClient, owned.Slug, "flow")
	run, err := runClient.RunWorkflow(ctx, &workflowsv1.RunWorkflowRequest{Name: wf.GetName()})
	require.NoError(t, err)
	return run.GetName(), runIDFromName(t, run.GetName()), runClient
}

// runStateOf reads a run's current state through the API (interceptor chain).
func runStateOf(t *testing.T, ctx context.Context, runClient workflowsv1.WorkflowRunsClient, runName string) *workflowsv1.WorkflowRun {
	t.Helper()
	got, err := runClient.GetWorkflowRun(ctx, &workflowsv1.GetWorkflowRunRequest{Name: runName})
	require.NoError(t, err)
	return got
}

// forceRunState drives a run row directly to want, bypassing the executor, so a
// test can place it in the stranded (RUNNING) or already-terminal state it needs.
func forceRunState(t *testing.T, ctx context.Context, h *grpcharness.Harness, runID uuid.UUID, want string) {
	t.Helper()
	end := pgtype.Timestamptz{}
	if runjob.IsTerminalState(want) {
		end = pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
	}
	_, err := h.Queries.UpdateWorkflowRunState(ctx, db.UpdateWorkflowRunStateParams{
		ID:        runID,
		State:     want,
		StartTime: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
		EndTime:   end,
	})
	require.NoError(t, err)
}

// discardedJobRow forges the rivertype.JobRow River hands an ErrorHandler on a
// discarded workflow_run job: the run's args encoded as JSON, at attempt
// maxAttempts+attemptDelta (0 → the final attempt River discards on; negative →
// a non-final attempt).
func discardedJobRow(t *testing.T, runID uuid.UUID, attemptDelta int) *rivertype.JobRow {
	t.Helper()
	const maxAttempts = 5
	enc, err := json.Marshal(runjob.Args{RunID: runID})
	require.NoError(t, err)
	return &rivertype.JobRow{
		ID:          424242,
		Kind:        runjob.Args{}.Kind(),
		Attempt:     maxAttempts + attemptDelta,
		MaxAttempts: maxAttempts,
		EncodedArgs: enc,
	}
}

// setWorkflowJobState flips the single enqueued workflow_run river_job to a given
// state directly, simulating River having discarded it without running anything.
func setWorkflowJobState(t *testing.T, ctx context.Context, h *grpcharness.Harness, state string) {
	t.Helper()
	tag, err := h.Pool.Exec(ctx,
		`UPDATE river.river_job SET state = $1, finalized_at = now() WHERE kind = $2`,
		state, runjob.Args{}.Kind())
	require.NoError(t, err)
	require.Equal(t, int64(1), tag.RowsAffected(), "expected exactly one workflow_run job to update")
}

// startReaper starts a River client with only the stranded-run reaper registered.
func startReaper(t *testing.T, h *grpcharness.Harness) *riverpro.Client[pgx.Tx] {
	t.Helper()
	return h.StartRiverWorkers(t, func(rw *river.Workers) {
		river.AddWorker(rw, &workers.ReapStrandedRunsWorker{Pool: h.Pool, Logger: grpcharness.SilentLogger()})
	})
}

// waitJobCompleted blocks until a river_job reaches 'completed', proving the
// reaper tick finished before a test asserts its (non-)effect on the run.
func waitJobCompleted(t *testing.T, ctx context.Context, h *grpcharness.Harness, jobID int64) {
	t.Helper()
	require.Eventually(t, func() bool {
		var state string
		err := h.Pool.QueryRow(ctx, `SELECT state FROM river.river_job WHERE id = $1`, jobID).Scan(&state)
		require.NoError(t, err)
		return state == "completed"
	}, 15*time.Second, 100*time.Millisecond, "reaper job should complete")
}

// TestE2E_DiscardHandler_FinalizesRunOnFinalAttempt: a RUNNING run whose job
// errors on its LAST attempt (River about to discard) is finalized FAILED by the
// ErrorHandler, with the error recorded.
func TestE2E_DiscardHandler_FinalizesRunOnFinalAttempt(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := runHarness(t)
	ctx := context.Background()
	runName, runID, runClient := seededRun(t, ctx, h, "dh-final")
	forceRunState(t, ctx, h, runID, runjob.StateRunning)

	handler := &workers.RunWorkflowErrorHandler{Pool: h.Pool, Logger: grpcharness.SilentLogger()}
	res := handler.HandleError(ctx, discardedJobRow(t, runID, 0), errors.New("upstream 503 exhausted"))
	assert.Nil(t, res, "handler must not alter River's retry/discard decision")

	got := runStateOf(t, ctx, runClient, runName)
	assert.Equal(t, workflowsv1.State_FAILED, got.GetState())
	require.NotNil(t, got.GetError())
	assert.Contains(t, got.GetError().GetMessage(), "upstream 503 exhausted")
	assert.NotNil(t, got.GetEndTime())
}

// TestE2E_DiscardHandler_NonFinalAttemptNoOp: an error on a non-final attempt is
// left to River's retry schedule — the run stays RUNNING, un-finalized.
func TestE2E_DiscardHandler_NonFinalAttemptNoOp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := runHarness(t)
	ctx := context.Background()
	runName, runID, runClient := seededRun(t, ctx, h, "dh-nonfinal")
	forceRunState(t, ctx, h, runID, runjob.StateRunning)

	handler := &workers.RunWorkflowErrorHandler{Pool: h.Pool, Logger: grpcharness.SilentLogger()}
	// attemptDelta -2 → Attempt (3) < MaxAttempts (5): not the final attempt.
	handler.HandleError(ctx, discardedJobRow(t, runID, -2), errors.New("transient blip"))

	got := runStateOf(t, ctx, runClient, runName)
	assert.Equal(t, workflowsv1.State_RUNNING, got.GetState(), "a non-final attempt must not finalize the run")
	assert.Nil(t, got.GetError())
	assert.Nil(t, got.GetEndTime())
}

// TestE2E_DiscardHandler_PanicFinalizesOnFinalAttempt: a panic that exhausts the
// attempts strands the run exactly like an error, so HandlePanic finalizes it.
func TestE2E_DiscardHandler_PanicFinalizesOnFinalAttempt(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := runHarness(t)
	ctx := context.Background()
	runName, runID, runClient := seededRun(t, ctx, h, "dh-panic")
	forceRunState(t, ctx, h, runID, runjob.StateRunning)

	handler := &workers.RunWorkflowErrorHandler{Pool: h.Pool, Logger: grpcharness.SilentLogger()}
	res := handler.HandlePanic(ctx, discardedJobRow(t, runID, 0), "nil map write", "stacktrace")
	assert.Nil(t, res)

	got := runStateOf(t, ctx, runClient, runName)
	assert.Equal(t, workflowsv1.State_FAILED, got.GetState())
	require.NotNil(t, got.GetError())
	assert.Contains(t, got.GetError().GetMessage(), "panicked")
}

// TestE2E_DiscardHandler_DoesNotClobberTerminal: the handler must never overwrite
// a run that already reached a terminal state — a business CANCELLED run stays
// CANCELLED even when a late final-attempt error arrives.
func TestE2E_DiscardHandler_DoesNotClobberTerminal(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := runHarness(t)
	ctx := context.Background()
	runName, runID, runClient := seededRun(t, ctx, h, "dh-terminal")
	forceRunState(t, ctx, h, runID, runjob.StateCancelled)

	handler := &workers.RunWorkflowErrorHandler{Pool: h.Pool, Logger: grpcharness.SilentLogger()}
	handler.HandleError(ctx, discardedJobRow(t, runID, 0), errors.New("too late"))

	got := runStateOf(t, ctx, runClient, runName)
	assert.Equal(t, workflowsv1.State_CANCELLED, got.GetState(), "finalize must not clobber a terminal run")
	assert.Nil(t, got.GetError(), "no FAILED error is recorded over CANCELLED")
}

// TestE2E_Reaper_FinalizesStrandedDiscardedRun: the periodic reaper finalizes a
// RUNNING run whose workflow_run job has been discarded. Runs the real
// ReapStrandedRunsWorker.Work end-to-end (the client resolves via
// ClientFromContext) by inserting a reaper job.
func TestE2E_Reaper_FinalizesStrandedDiscardedRun(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := runHarness(t)
	ctx := context.Background()
	runName, runID, runClient := seededRun(t, ctx, h, "reap-run")
	forceRunState(t, ctx, h, runID, runjob.StateRunning)
	// Discard the job BEFORE starting the client so River never fetches it.
	setWorkflowJobState(t, ctx, h, string(rivertype.JobStateDiscarded))

	client := startReaper(t, h)
	_, err := client.Insert(ctx, workers.ReapStrandedRunsArgs{}, nil)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return runStateOf(t, ctx, runClient, runName).GetState() == workflowsv1.State_FAILED
	}, 15*time.Second, 100*time.Millisecond, "reaper should finalize the stranded run FAILED")

	got := runStateOf(t, ctx, runClient, runName)
	require.NotNil(t, got.GetError())
	assert.Contains(t, got.GetError().GetMessage(), "discarded after exhausting retries")
	assert.NotNil(t, got.GetEndTime())
}

// TestE2E_Reaper_LeavesTerminalRunUntouched: a discarded job whose run is already
// terminal (SUCCEEDED) is a no-op — the reaper never overwrites it.
func TestE2E_Reaper_LeavesTerminalRunUntouched(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := runHarness(t)
	ctx := context.Background()
	runName, runID, runClient := seededRun(t, ctx, h, "reap-term")
	forceRunState(t, ctx, h, runID, runjob.StateSucceeded)
	setWorkflowJobState(t, ctx, h, string(rivertype.JobStateDiscarded))

	client := startReaper(t, h)
	insertRes, err := client.Insert(ctx, workers.ReapStrandedRunsArgs{}, nil)
	require.NoError(t, err)
	waitJobCompleted(t, ctx, h, insertRes.Job.ID)

	got := runStateOf(t, ctx, runClient, runName)
	assert.Equal(t, workflowsv1.State_SUCCEEDED, got.GetState(), "reaper must not overwrite a terminal run")
}

// TestE2E_Reaper_IgnoresActiveJob: a RUNNING run whose job is NOT discarded (still
// scheduled/active) must NOT be reaped — the reaper's filter is DISCARDED only.
func TestE2E_Reaper_IgnoresActiveJob(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := runHarness(t)
	ctx := context.Background()
	runName, runID, runClient := seededRun(t, ctx, h, "reap-active")
	forceRunState(t, ctx, h, runID, runjob.StateRunning)
	// Park the job far in the future as 'scheduled' — active, not discarded, and
	// not due, so the started client won't fetch it.
	_, err := h.Pool.Exec(ctx,
		`UPDATE river.river_job SET state = 'scheduled', scheduled_at = now() + interval '1 hour' WHERE kind = $1`,
		runjob.Args{}.Kind())
	require.NoError(t, err)

	client := startReaper(t, h)
	insertRes, err := client.Insert(ctx, workers.ReapStrandedRunsArgs{}, nil)
	require.NoError(t, err)
	waitJobCompleted(t, ctx, h, insertRes.Job.ID)

	got := runStateOf(t, ctx, runClient, runName)
	assert.Equal(t, workflowsv1.State_RUNNING, got.GetState(), "a run with a non-discarded job must not be reaped")
	assert.Nil(t, got.GetError())
}
