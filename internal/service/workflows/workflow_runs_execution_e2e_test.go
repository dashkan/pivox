package workflows_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/riverqueue/river"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/engine"
	"github.com/dashkan/pivox/internal/engine/runjob"
	workflowsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/workflows/v1"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
	"github.com/dashkan/pivox/internal/workers"
)

// setStep is a leaf `set` activity step with a single assignment.
func setStep(id, varName, celExpr string) *workflowsv1.Step {
	return &workflowsv1.Step{
		Id: id,
		Kind: &workflowsv1.Step_Activity{Activity: &workflowsv1.Activity{
			Kind: &workflowsv1.Activity_Set{Set: &workflowsv1.SetActivity{
				Assignments: map[string]string{varName: celExpr},
			}},
		}},
	}
}

// richSetDefinition exercises all three structural forms with set leaves: a
// plain set, a condition (whose true branch runs), and a parallel of two sets.
// Terminal vars: a=hello, b=world, c=1, d=2 → the run's output.
func richSetDefinition() *workflowsv1.WorkflowVersion {
	return &workflowsv1.WorkflowVersion{
		Note: "rich",
		Root: &workflowsv1.Sequence{Steps: []*workflowsv1.Step{
			setStep("s1", "a", `"hello"`),
			{
				Id: "cond",
				Kind: &workflowsv1.Step_Condition{Condition: &workflowsv1.Condition{
					Branches: []*workflowsv1.Branch{{
						When: `vars.a == "hello"`,
						Then: &workflowsv1.Sequence{Steps: []*workflowsv1.Step{setStep("sb", "b", `"world"`)}},
					}},
					Otherwise: &workflowsv1.Sequence{Steps: []*workflowsv1.Step{setStep("sb_else", "b", `"nope"`)}},
				}},
			},
			{
				Id: "par",
				Kind: &workflowsv1.Step_Parallel{Parallel: &workflowsv1.Parallel{
					Branches: []*workflowsv1.Sequence{
						{Steps: []*workflowsv1.Step{setStep("sc", "c", `1`)}},
						{Steps: []*workflowsv1.Step{setStep("sd", "d", `2`)}},
					},
				}},
			},
		}},
	}
}

// createPromotedDef creates a workflow, mints the given version definition, and
// promotes it. Caller must be authenticated as an owner of orgSlug.
func createPromotedDef(
	t *testing.T,
	ctx context.Context,
	wfClient workflowsv1.WorkflowsClient,
	verClient workflowsv1.WorkflowVersionsClient,
	orgSlug, workflowID string,
	def *workflowsv1.WorkflowVersion,
) *workflowsv1.Workflow {
	t.Helper()
	wf, err := wfClient.CreateWorkflow(ctx, &workflowsv1.CreateWorkflowRequest{
		Parent:     "organizations/" + orgSlug,
		WorkflowId: workflowID,
		Workflow:   &workflowsv1.Workflow{DisplayName: workflowID},
	})
	require.NoError(t, err)
	ver, err := verClient.CreateWorkflowVersion(ctx, &workflowsv1.CreateWorkflowVersionRequest{
		Parent: wf.GetName(), WorkflowVersion: def,
	})
	require.NoError(t, err)
	_, err = wfClient.PromoteWorkflowVersion(ctx, &workflowsv1.PromoteWorkflowVersionRequest{
		Name: wf.GetName(), Version: ver.GetName(),
	})
	require.NoError(t, err)
	return wf
}

// realInterpreter builds the production 6b interpreter (set-only dispatcher).
func realInterpreter(t *testing.T) *engine.Interpreter {
	t.Helper()
	eval, err := engine.NewEvaluator()
	require.NoError(t, err)
	return engine.NewInterpreter(engine.InterpreterConfig{
		Evaluator: eval,
		Dispatcher: engine.NewDispatcher(engine.DispatcherConfig{
			Set: engine.NewSetActivity(engine.SetActivityConfig{Evaluator: eval}),
		}),
	})
}

// stubSetActivity is a `set` handler that returns a fixed error, letting a test
// drive the retryable-vs-terminal split without a real infra fault.
type stubSetActivity struct{ err error }

func (a stubSetActivity) Execute(context.Context, *engine.RunContext, *workflowsv1.Step) (any, error) {
	return nil, a.err
}

// interpreterWithSet builds an interpreter whose `set` handler is the given
// activity (a stub). The real definition still walks; only the leaf is stubbed.
func interpreterWithSet(t *testing.T, set engine.Activity) *engine.Interpreter {
	t.Helper()
	eval, err := engine.NewEvaluator()
	require.NoError(t, err)
	return engine.NewInterpreter(engine.InterpreterConfig{
		Evaluator:  eval,
		Dispatcher: engine.NewDispatcher(engine.DispatcherConfig{Set: set}),
	})
}

// runJobCount returns how many workflow_run river_job rows exist, regardless of
// state. Direct SQL: rivertest.RequireInserted has a schema-resolution gap
// outside an explicitly schema-aware client (see the org-lifecycle river test).
func runJobCount(t *testing.T, ctx context.Context, h *grpcharness.Harness) int {
	t.Helper()
	var n int
	err := h.Pool.QueryRow(ctx,
		`SELECT count(*) FROM river.river_job WHERE kind = $1`, runjob.Args{}.Kind()).Scan(&n)
	require.NoError(t, err)
	return n
}

// waitRunState polls GetWorkflowRun until it reaches want (or times out).
func waitRunState(
	t *testing.T,
	ctx context.Context,
	client workflowsv1.WorkflowRunsClient,
	name string,
	want workflowsv1.State,
	timeout time.Duration,
) *workflowsv1.WorkflowRun {
	t.Helper()
	var last *workflowsv1.WorkflowRun
	require.Eventually(t, func() bool {
		got, err := client.GetWorkflowRun(ctx, &workflowsv1.GetWorkflowRunRequest{Name: name})
		if err != nil {
			return false
		}
		last = got
		return got.GetState() == want
	}, timeout, 50*time.Millisecond, "run never reached %s (last=%v)", want, func() workflowsv1.State {
		if last == nil {
			return workflowsv1.State_STATE_UNSPECIFIED
		}
		return last.GetState()
	}())
	return last
}

// TestE2E_RunWorkflow_EnqueuesJob pins the transactional enqueue: RunWorkflow
// enqueues exactly one execution job in the same tx as the run insert, and
// validate_only enqueues nothing.
func TestE2E_RunWorkflow_EnqueuesJob(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := runHarness(t)
	owned := h.SeedOwnedOrg(t, "wfr-enq", "WFR Enq", "workflows")
	ctx := context.Background()
	wfClient := workflowsv1.NewWorkflowsClient(h.Conn())
	verClient := workflowsv1.NewWorkflowVersionsClient(h.Conn())
	runClient := workflowsv1.NewWorkflowRunsClient(h.Conn())

	wf, _ := newPromotedWorkflow(t, ctx, wfClient, verClient, owned.Slug, "flow")

	// validate_only: run against real constraints but roll back — no job.
	_, err := runClient.RunWorkflow(ctx, &workflowsv1.RunWorkflowRequest{Name: wf.GetName(), ValidateOnly: true})
	require.NoError(t, err)
	assert.Equal(t, 0, runJobCount(t, ctx, h), "validate_only must enqueue nothing")

	// Real run: exactly one job enqueued, committed with the run row.
	run, err := runClient.RunWorkflow(ctx, &workflowsv1.RunWorkflowRequest{Name: wf.GetName()})
	require.NoError(t, err)
	assert.Equal(t, workflowsv1.State_PENDING, run.GetState())
	assert.Equal(t, 1, runJobCount(t, ctx, h), "RunWorkflow must enqueue exactly one execution job")
}

// TestE2E_RunWorkflow_ExecutesToSucceeded runs a set-only definition (with a
// condition and a parallel of sets) end-to-end through the real worker: the run
// reaches SUCCEEDED, steps are checkpointed, and the output is the final vars.
func TestE2E_RunWorkflow_ExecutesToSucceeded(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := runHarness(t)
	h.StartRiverWorkers(t, func(rw *river.Workers) {
		river.AddWorker(rw, &workers.RunWorkflowWorker{
			Pool: h.Pool, Interpreter: realInterpreter(t), Logger: grpcharness.SilentLogger(),
		})
	})
	owned := h.SeedOwnedOrg(t, "wfr-exec", "WFR Exec", "workflows")
	ctx := context.Background()
	wfClient := workflowsv1.NewWorkflowsClient(h.Conn())
	verClient := workflowsv1.NewWorkflowVersionsClient(h.Conn())
	runClient := workflowsv1.NewWorkflowRunsClient(h.Conn())

	wf := createPromotedDef(t, ctx, wfClient, verClient, owned.Slug, "rich", richSetDefinition())

	run, err := runClient.RunWorkflow(ctx, &workflowsv1.RunWorkflowRequest{Name: wf.GetName()})
	require.NoError(t, err)

	done := waitRunState(t, ctx, runClient, run.GetName(), workflowsv1.State_SUCCEEDED, 15*time.Second)

	// Output = final vars snapshot.
	out := done.GetOutput().GetFields()
	assert.Equal(t, "hello", out["a"].GetStringValue())
	assert.Equal(t, "world", out["b"].GetStringValue(), "the condition's true branch ran")
	assert.Equal(t, float64(1), out["c"].GetNumberValue())
	assert.Equal(t, float64(2), out["d"].GetNumberValue())

	// Steps: one entry per set leaf that ran (s1, sb, sc, sd); the else set and
	// the structural cond/par nodes never appear.
	states := map[string]workflowsv1.State{}
	for _, s := range done.GetSteps() {
		states[s.GetStepId()] = s.GetState()
	}
	assert.Equal(t, workflowsv1.State_SUCCEEDED, states["s1"])
	assert.Equal(t, workflowsv1.State_SUCCEEDED, states["sb"])
	assert.Equal(t, workflowsv1.State_SUCCEEDED, states["sc"])
	assert.Equal(t, workflowsv1.State_SUCCEEDED, states["sd"])
	assert.NotContains(t, states, "sb_else", "the condition's else branch must not run")
	assert.Len(t, done.GetSteps(), 4)

	assert.NotNil(t, done.GetStartTime(), "SUCCEEDED run has a start_time")
	assert.NotNil(t, done.GetEndTime(), "SUCCEEDED run has an end_time")
}

// TestE2E_RunWorkflow_TerminalFailure pins that a terminal activity failure
// marks the run FAILED and the job is NOT retried (it completes).
func TestE2E_RunWorkflow_TerminalFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := runHarness(t)
	h.StartRiverWorkers(t, func(rw *river.Workers) {
		river.AddWorker(rw, &workers.RunWorkflowWorker{
			Pool:        h.Pool,
			Interpreter: interpreterWithSet(t, stubSetActivity{err: errors.New("bad activity config")}),
			Logger:      grpcharness.SilentLogger(),
		})
	})
	owned := h.SeedOwnedOrg(t, "wfr-fail", "WFR Fail", "workflows")
	ctx := context.Background()
	wfClient := workflowsv1.NewWorkflowsClient(h.Conn())
	verClient := workflowsv1.NewWorkflowVersionsClient(h.Conn())
	runClient := workflowsv1.NewWorkflowRunsClient(h.Conn())

	wf, _ := newPromotedWorkflow(t, ctx, wfClient, verClient, owned.Slug, "flow")
	run, err := runClient.RunWorkflow(ctx, &workflowsv1.RunWorkflowRequest{Name: wf.GetName()})
	require.NoError(t, err)

	failed := waitRunState(t, ctx, runClient, run.GetName(), workflowsv1.State_FAILED, 15*time.Second)
	require.NotNil(t, failed.GetError())
	assert.Contains(t, failed.GetError().GetMessage(), "bad activity config")
	assert.NotNil(t, failed.GetEndTime())

	// A terminal failure must not schedule a retry: the job completes, and no
	// further attempt ever flips the run.
	require.Eventually(t, func() bool {
		var errored int
		err := h.Pool.QueryRow(ctx,
			`SELECT count(*) FROM river.river_job WHERE kind = $1 AND state = 'completed'`,
			runjob.Args{}.Kind()).Scan(&errored)
		require.NoError(t, err)
		return errored == 1
	}, 10*time.Second, 50*time.Millisecond, "terminal-failure job should complete, not retry")
}

// TestE2E_RunWorkflow_RetryableFailure pins that a retryable error is returned
// to River (the job is scheduled for retry) and the run is NOT marked FAILED —
// it stays RUNNING pending the next attempt.
func TestE2E_RunWorkflow_RetryableFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := runHarness(t)
	h.StartRiverWorkers(t, func(rw *river.Workers) {
		river.AddWorker(rw, &workers.RunWorkflowWorker{
			Pool:        h.Pool,
			Interpreter: interpreterWithSet(t, stubSetActivity{err: engine.Retryable(errors.New("upstream 503"))}),
			Logger:      grpcharness.SilentLogger(),
		})
	})
	owned := h.SeedOwnedOrg(t, "wfr-retry", "WFR Retry", "workflows")
	ctx := context.Background()
	wfClient := workflowsv1.NewWorkflowsClient(h.Conn())
	verClient := workflowsv1.NewWorkflowVersionsClient(h.Conn())
	runClient := workflowsv1.NewWorkflowRunsClient(h.Conn())

	wf, _ := newPromotedWorkflow(t, ctx, wfClient, verClient, owned.Slug, "flow")
	run, err := runClient.RunWorkflow(ctx, &workflowsv1.RunWorkflowRequest{Name: wf.GetName()})
	require.NoError(t, err)

	// The first attempt errors and River records it + schedules a retry. Wait
	// for that error to land, then assert the run is RUNNING — never FAILED.
	require.Eventually(t, func() bool {
		var withErrors int
		err := h.Pool.QueryRow(ctx,
			`SELECT count(*) FROM river.river_job WHERE kind = $1 AND array_length(errors, 1) >= 1`,
			runjob.Args{}.Kind()).Scan(&withErrors)
		require.NoError(t, err)
		return withErrors == 1
	}, 15*time.Second, 50*time.Millisecond, "retryable attempt should record an error and reschedule")

	got, err := runClient.GetWorkflowRun(ctx, &workflowsv1.GetWorkflowRunRequest{Name: run.GetName()})
	require.NoError(t, err)
	assert.Equal(t, workflowsv1.State_RUNNING, got.GetState(),
		"a retryable failure must NOT mark the run FAILED")
	assert.Nil(t, got.GetError(), "no terminal error is recorded on a retryable failure")
	assert.Nil(t, got.GetEndTime(), "a run pending retry has no end_time")
}

// TestE2E_RunWorkflow_CancelledRunIsNoOp pins the terminal-run guard + the
// finalize-respects-CANCELLED path: a run cancelled before its job executes is
// a no-op — the worker never runs the definition and never clobbers CANCELLED.
func TestE2E_RunWorkflow_CancelledRunIsNoOp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := runHarness(t)
	owned := h.SeedOwnedOrg(t, "wfr-cancel", "WFR Cancel", "workflows")
	ctx := context.Background()
	wfClient := workflowsv1.NewWorkflowsClient(h.Conn())
	verClient := workflowsv1.NewWorkflowVersionsClient(h.Conn())
	runClient := workflowsv1.NewWorkflowRunsClient(h.Conn())

	wf := createPromotedDef(t, ctx, wfClient, verClient, owned.Slug, "rich", richSetDefinition())

	// Enqueue the run, then cancel it BEFORE starting any worker — the job sits
	// in the queue while the run row goes CANCELLED.
	run, err := runClient.RunWorkflow(ctx, &workflowsv1.RunWorkflowRequest{Name: wf.GetName()})
	require.NoError(t, err)
	cancelled, err := runClient.CancelWorkflowRun(ctx, &workflowsv1.CancelWorkflowRunRequest{Name: run.GetName()})
	require.NoError(t, err)
	require.Equal(t, workflowsv1.State_CANCELLED, cancelled.GetState())

	// Now start the worker: it picks up the pending job, sees the run is
	// already terminal, and no-ops.
	h.StartRiverWorkers(t, func(rw *river.Workers) {
		river.AddWorker(rw, &workers.RunWorkflowWorker{
			Pool: h.Pool, Interpreter: realInterpreter(t), Logger: grpcharness.SilentLogger(),
		})
	})

	// The job completes (no error, no retry) and the run stays CANCELLED with no
	// output — proving the definition never executed.
	require.Eventually(t, func() bool {
		var completed int
		err := h.Pool.QueryRow(ctx,
			`SELECT count(*) FROM river.river_job WHERE kind = $1 AND state = 'completed'`,
			runjob.Args{}.Kind()).Scan(&completed)
		require.NoError(t, err)
		return completed == 1
	}, 10*time.Second, 50*time.Millisecond, "cancelled-run job should complete as a no-op")

	got, err := runClient.GetWorkflowRun(ctx, &workflowsv1.GetWorkflowRunRequest{Name: run.GetName()})
	require.NoError(t, err)
	assert.Equal(t, workflowsv1.State_CANCELLED, got.GetState(), "worker must not resurrect a cancelled run")
	assert.Nil(t, got.GetOutput(), "no steps ran, so there is no output")
}

// TestE2E_RunWorkflow_AlreadyRunningReExecutes pins the documented already-
// RUNNING semantics: a job re-delivered against a run left RUNNING by a crashed
// prior attempt re-executes from the top (begin sees RUNNING, not terminal) and
// drives the run to SUCCEEDED. Simulated by forcing the run to RUNNING before
// the worker picks up the enqueued job.
func TestE2E_RunWorkflow_AlreadyRunningReExecutes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := runHarness(t)
	owned := h.SeedOwnedOrg(t, "wfr-rerun", "WFR Rerun", "workflows")
	ctx := context.Background()
	wfClient := workflowsv1.NewWorkflowsClient(h.Conn())
	verClient := workflowsv1.NewWorkflowVersionsClient(h.Conn())
	runClient := workflowsv1.NewWorkflowRunsClient(h.Conn())

	wf, _ := newPromotedWorkflow(t, ctx, wfClient, verClient, owned.Slug, "flow")

	// Enqueue the run, then force it to RUNNING before any worker runs — the
	// state a crashed prior attempt would have left behind.
	run, err := runClient.RunWorkflow(ctx, &workflowsv1.RunWorkflowRequest{Name: wf.GetName()})
	require.NoError(t, err)
	_, err = h.Queries.UpdateWorkflowRunState(ctx, db.UpdateWorkflowRunStateParams{
		ID:        runIDFromName(t, run.GetName()),
		State:     "RUNNING",
		StartTime: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
	})
	require.NoError(t, err)

	// Now run the worker: it should re-execute the RUNNING run to completion.
	h.StartRiverWorkers(t, func(rw *river.Workers) {
		river.AddWorker(rw, &workers.RunWorkflowWorker{
			Pool: h.Pool, Interpreter: realInterpreter(t), Logger: grpcharness.SilentLogger(),
		})
	})

	done := waitRunState(t, ctx, runClient, run.GetName(), workflowsv1.State_SUCCEEDED, 15*time.Second)
	assert.Equal(t, "x", done.GetOutput().GetFields()["v"].GetStringValue(),
		"the re-executed run produces its output")
}

// gateSetActivity is a `set` handler that signals when it starts and then blocks
// until released (or ctx cancels). It lets a test cancel a run WHILE the worker
// is mid-execution, exercising the finalize-respects-CANCELLED re-check.
type gateSetActivity struct {
	startOnce sync.Once
	started   chan struct{}
	release   chan struct{}
}

func (a *gateSetActivity) Execute(ctx context.Context, _ *engine.RunContext, _ *workflowsv1.Step) (any, error) {
	a.startOnce.Do(func() { close(a.started) })
	select {
	case <-a.release:
		return map[string]any{"done": true}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// TestE2E_RunWorkflow_CancelDuringExecution pins finalize-respects-CANCELLED for
// the mid-execution case: a run cancelled while the worker is running its
// definition must stay CANCELLED — the worker completes but its finalize
// re-reads the row and refuses to overwrite the terminal state.
func TestE2E_RunWorkflow_CancelDuringExecution(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	gate := &gateSetActivity{started: make(chan struct{}), release: make(chan struct{})}
	h := runHarness(t)
	h.StartRiverWorkers(t, func(rw *river.Workers) {
		river.AddWorker(rw, &workers.RunWorkflowWorker{
			Pool: h.Pool, Interpreter: interpreterWithSet(t, gate), Logger: grpcharness.SilentLogger(),
		})
	})
	owned := h.SeedOwnedOrg(t, "wfr-cxe", "WFR CxE", "workflows")
	ctx := context.Background()
	wfClient := workflowsv1.NewWorkflowsClient(h.Conn())
	verClient := workflowsv1.NewWorkflowVersionsClient(h.Conn())
	runClient := workflowsv1.NewWorkflowRunsClient(h.Conn())

	wf, _ := newPromotedWorkflow(t, ctx, wfClient, verClient, owned.Slug, "flow")
	run, err := runClient.RunWorkflow(ctx, &workflowsv1.RunWorkflowRequest{Name: wf.GetName()})
	require.NoError(t, err)

	// Wait until the worker is inside the activity, then cancel mid-execution.
	select {
	case <-gate.started:
	case <-time.After(15 * time.Second):
		t.Fatal("activity never started")
	}
	cancelled, err := runClient.CancelWorkflowRun(ctx, &workflowsv1.CancelWorkflowRunRequest{Name: run.GetName()})
	require.NoError(t, err)
	require.Equal(t, workflowsv1.State_CANCELLED, cancelled.GetState())

	// Release the activity: the interpreter completes and the worker runs its
	// finalize, which must see CANCELLED and NOT write SUCCEEDED.
	close(gate.release)

	require.Eventually(t, func() bool {
		var completed int
		err := h.Pool.QueryRow(ctx,
			`SELECT count(*) FROM river.river_job WHERE kind = $1 AND state = 'completed'`,
			runjob.Args{}.Kind()).Scan(&completed)
		require.NoError(t, err)
		return completed == 1
	}, 10*time.Second, 50*time.Millisecond, "worker should complete after release")

	got, err := runClient.GetWorkflowRun(ctx, &workflowsv1.GetWorkflowRunRequest{Name: run.GetName()})
	require.NoError(t, err)
	assert.Equal(t, workflowsv1.State_CANCELLED, got.GetState(),
		"a cancel that lands mid-execution must survive the worker's finalize")
	assert.Nil(t, got.GetOutput(), "finalize must not write the completed run's output over CANCELLED")
}
