package engine

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	workflowsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/workflows/v1"
)

// --- proto builders -------------------------------------------------------

func seq(steps ...*workflowsv1.Step) *workflowsv1.Sequence {
	return &workflowsv1.Sequence{Steps: steps}
}

func setStep(id string, assignments map[string]string) *workflowsv1.Step {
	return &workflowsv1.Step{
		Id: id,
		Kind: &workflowsv1.Step_Activity{
			Activity: &workflowsv1.Activity{
				Kind: &workflowsv1.Activity_Set{
					Set: &workflowsv1.SetActivity{Assignments: assignments},
				},
			},
		},
	}
}

// httpStep builds a step in the http-activity slot; tests use it to inject a
// custom [Activity] (registered as DispatcherConfig.HTTP) without depending on
// real HTTP behavior, which does not exist until 6c.
func httpStep(id string) *workflowsv1.Step {
	return &workflowsv1.Step{
		Id: id,
		Kind: &workflowsv1.Step_Activity{
			Activity: &workflowsv1.Activity{
				Kind: &workflowsv1.Activity_Http{Http: &workflowsv1.HttpActivity{}},
			},
		},
	}
}

func branch(when string, then *workflowsv1.Sequence) *workflowsv1.Branch {
	return &workflowsv1.Branch{When: when, Then: then}
}

func conditionStep(id string, otherwise *workflowsv1.Sequence, branches ...*workflowsv1.Branch) *workflowsv1.Step {
	return &workflowsv1.Step{
		Id: id,
		Kind: &workflowsv1.Step_Condition{
			Condition: &workflowsv1.Condition{Branches: branches, Otherwise: otherwise},
		},
	}
}

func parallelStep(id string, branches ...*workflowsv1.Sequence) *workflowsv1.Step {
	return &workflowsv1.Step{
		Id: id,
		Kind: &workflowsv1.Step_Parallel{
			Parallel: &workflowsv1.Parallel{Branches: branches},
		},
	}
}

// --- harness --------------------------------------------------------------

func newTestInterpreter(t *testing.T, custom DispatcherConfig) *Interpreter {
	t.Helper()
	eval, err := NewEvaluator()
	require.NoError(t, err)
	custom.Set = NewSetActivity(SetActivityConfig{Evaluator: eval})
	disp := NewDispatcher(custom)
	return NewInterpreter(InterpreterConfig{Evaluator: eval, Dispatcher: disp})
}

func statuses(states []StepState) map[string]StepStatus {
	out := make(map[string]StepStatus, len(states))
	for _, s := range states {
		out[s.ID] = s.Status
	}
	return out
}

// --- tests ----------------------------------------------------------------

func TestInterpreter_LinearSequenceFlowsForward(t *testing.T) {
	t.Parallel()

	it := newTestInterpreter(t, DispatcherConfig{})
	rc := NewRunContext(RunContextConfig{})
	reporter := NewInMemoryReporter()

	root := seq(
		setStep("a", map[string]string{"x": "1 + 1"}),
		setStep("b", map[string]string{"y": "vars.x + steps.a.output.x"}),
	)

	res, err := it.Run(context.Background(), root, rc, reporter)
	require.NoError(t, err)

	assert.Equal(t, RunStatusCompleted, res.Status)
	assert.Equal(t, map[string]any{"x": int64(2), "y": int64(4)}, res.Output)

	// Reporter captured start+finish for each step, in order.
	states := reporter.States()
	require.Len(t, states, 4)
	assert.Equal(t, []string{"a", "a", "b", "b"}, []string{states[0].ID, states[1].ID, states[2].ID, states[3].ID})
	assert.Equal(t, StepStatusRunning, states[0].Status)
	assert.Equal(t, StepStatusSucceeded, states[1].Status)
	assert.Equal(t, StepStatusSucceeded, states[3].Status)

	// Result carries one final state per step.
	assert.Equal(t, map[string]StepStatus{"a": StepStatusSucceeded, "b": StepStatusSucceeded}, statuses(res.Steps))
}

func TestInterpreter_SetOutputIsAssignedMap(t *testing.T) {
	t.Parallel()

	it := newTestInterpreter(t, DispatcherConfig{})
	rc := NewRunContext(RunContextConfig{Params: map[string]any{"who": "world"}})

	root := seq(setStep("greet", map[string]string{
		"msg": `"hi " + params.who`,
		"num": "6 * 7",
	}))

	res, err := it.Run(context.Background(), root, rc, nil)
	require.NoError(t, err)
	require.Len(t, res.Steps, 1)
	assert.Equal(t, map[string]any{"msg": "hi world", "num": int64(42)}, res.Steps[0].Output)
}

func TestInterpreter_ConditionFirstTrueWins(t *testing.T) {
	t.Parallel()

	it := newTestInterpreter(t, DispatcherConfig{})
	rc := NewRunContext(RunContextConfig{Params: map[string]any{"n": int64(2)}})
	reporter := NewInMemoryReporter()

	root := seq(conditionStep("cond",
		seq(setStep("else", map[string]string{"r": `"other"`})),
		branch("params.n == 1", seq(setStep("one", map[string]string{"r": `"one"`}))),
		branch("params.n == 2", seq(setStep("two", map[string]string{"r": `"two"`}))),
		branch("params.n == 2", seq(setStep("dead", map[string]string{"r": `"dead"`}))),
	))

	res, err := it.Run(context.Background(), root, rc, reporter)
	require.NoError(t, err)
	assert.Equal(t, "two", res.Output["r"])

	// Only the first matching branch's step ran.
	assert.Equal(t, map[string]StepStatus{"two": StepStatusSucceeded}, statuses(res.Steps))
}

func TestInterpreter_ConditionOtherwiseFallback(t *testing.T) {
	t.Parallel()

	it := newTestInterpreter(t, DispatcherConfig{})
	rc := NewRunContext(RunContextConfig{Params: map[string]any{"n": int64(99)}})

	root := seq(conditionStep("cond",
		seq(setStep("else", map[string]string{"r": `"other"`})),
		branch("params.n == 1", seq(setStep("one", map[string]string{"r": `"one"`}))),
	))

	res, err := it.Run(context.Background(), root, rc, nil)
	require.NoError(t, err)
	assert.Equal(t, "other", res.Output["r"])
	assert.Equal(t, map[string]StepStatus{"else": StepStatusSucceeded}, statuses(res.Steps))
}

func TestInterpreter_ConditionNoMatchNoOtherwiseIsNoOp(t *testing.T) {
	t.Parallel()

	it := newTestInterpreter(t, DispatcherConfig{})
	rc := NewRunContext(RunContextConfig{Params: map[string]any{"n": int64(99)}})

	root := seq(conditionStep("cond", nil,
		branch("params.n == 1", seq(setStep("one", map[string]string{"r": `"one"`}))),
	))

	res, err := it.Run(context.Background(), root, rc, nil)
	require.NoError(t, err)
	assert.Equal(t, RunStatusCompleted, res.Status)
	assert.Empty(t, res.Output)
	assert.Empty(t, res.Steps)
}

func TestInterpreter_ConditionWhenNotBoolIsTerminal(t *testing.T) {
	t.Parallel()

	it := newTestInterpreter(t, DispatcherConfig{})
	rc := NewRunContext(RunContextConfig{})

	root := seq(conditionStep("cond", nil,
		branch(`"not a bool"`, seq(setStep("x", map[string]string{"r": "1"}))),
	))

	res, err := it.Run(context.Background(), root, rc, nil)
	require.Error(t, err)
	assert.Equal(t, RunStatusFailed, res.Status)
	assert.Contains(t, err.Error(), "did not evaluate to a bool")
	assert.False(t, IsRetryable(err))
}

func TestInterpreter_ParallelRunsConcurrentlyAndJoins(t *testing.T) {
	t.Parallel()

	// barrier fails unless both branches execute concurrently: each branch
	// blocks until the other arrives. Serial execution would time out.
	bar := &barrierActivity{n: 2, reached: make(chan struct{})}
	it := newTestInterpreter(t, DispatcherConfig{HTTP: bar})
	rc := NewRunContext(RunContextConfig{})
	reporter := NewInMemoryReporter()

	root := seq(parallelStep("par",
		seq(httpStep("left")),
		seq(httpStep("right")),
	))

	res, err := it.Run(context.Background(), root, rc, reporter)
	require.NoError(t, err)
	assert.Equal(t, RunStatusCompleted, res.Status)

	// Both branches finished (join) and recorded their outputs.
	assert.Equal(t,
		map[string]StepStatus{"left": StepStatusSucceeded, "right": StepStatusSucceeded},
		statuses(res.Steps),
	)
	assert.Equal(t, map[string]any{"left": "left", "right": "right"}, stepOutputs(rc))
}

func TestInterpreter_NestedConditionParallelSet(t *testing.T) {
	t.Parallel()

	it := newTestInterpreter(t, DispatcherConfig{})
	rc := NewRunContext(RunContextConfig{Params: map[string]any{"go": true}})

	root := seq(conditionStep("gate", nil,
		branch("params.go", seq(
			parallelStep("par",
				seq(setStep("l", map[string]string{"left": "10"})),
				seq(setStep("r", map[string]string{"right": "20"})),
			),
		)),
	))

	res, err := it.Run(context.Background(), root, rc, nil)
	require.NoError(t, err)
	assert.Equal(t, int64(10), res.Output["left"])
	assert.Equal(t, int64(20), res.Output["right"])
}

func TestInterpreter_ActivityErrorPropagatesAsFailedRun(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	it := newTestInterpreter(t, DispatcherConfig{HTTP: &fixedErrorActivity{err: boom}})
	rc := NewRunContext(RunContextConfig{})
	reporter := NewInMemoryReporter()

	root := seq(
		setStep("ok", map[string]string{"a": "1"}),
		httpStep("bad"),
		setStep("never", map[string]string{"b": "2"}),
	)

	res, err := it.Run(context.Background(), root, rc, reporter)
	require.ErrorIs(t, err, boom)
	assert.Equal(t, RunStatusFailed, res.Status)
	assert.False(t, IsRetryable(err))

	// "ok" succeeded, "bad" failed, "never" did not run.
	assert.Equal(t,
		map[string]StepStatus{"ok": StepStatusSucceeded, "bad": StepStatusFailed},
		statuses(res.Steps),
	)
	assert.Equal(t, StepStatusFailed, statuses(reporter.States())["bad"])
}

func TestInterpreter_RetryableActivityErrorClassified(t *testing.T) {
	t.Parallel()

	it := newTestInterpreter(t, DispatcherConfig{
		HTTP: &fixedErrorActivity{err: Retryable(errors.New("upstream 503"))},
	})
	rc := NewRunContext(RunContextConfig{})

	res, err := it.Run(context.Background(), seq(httpStep("flaky")), rc, nil)
	require.Error(t, err)
	// The run failed this attempt, but the error is flagged retryable for 6b.
	assert.Equal(t, RunStatusFailed, res.Status)
	assert.True(t, IsRetryable(err))
}

func TestInterpreter_ParallelBranchErrorCancelsSiblings(t *testing.T) {
	t.Parallel()

	boom := errors.New("branch boom")
	sib := &ctxWaitActivity{completed: make(chan struct{})}
	it := newTestInterpreter(t, DispatcherConfig{HTTP: sib, RunWorkflow: &fixedErrorActivity{err: boom}})
	rc := NewRunContext(RunContextConfig{})

	// runWorkflowStep uses the run_workflow slot for the failing branch.
	root := seq(parallelStep("par",
		seq(runWorkflowStep("failer")),
		seq(httpStep("sibling")),
	))

	res, err := it.Run(context.Background(), root, rc, nil)
	require.ErrorIs(t, err, boom)
	assert.Equal(t, RunStatusFailed, res.Status)

	// The sibling observed cancellation rather than completing.
	select {
	case <-sib.completed:
		t.Fatal("sibling should have been cancelled, not completed")
	default:
	}
}

func TestInterpreter_ContextCancelledYieldsCancelledOutcome(t *testing.T) {
	t.Parallel()

	it := newTestInterpreter(t, DispatcherConfig{})
	rc := NewRunContext(RunContextConfig{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled before the walk starts

	res, err := it.Run(ctx, seq(setStep("a", map[string]string{"x": "1"})), rc, nil)
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, RunStatusCancelled, res.Status)
	assert.Empty(t, res.Steps, "no step should run under a pre-cancelled context")
}

func TestInterpreter_DuplicateStepIDIsTerminal(t *testing.T) {
	t.Parallel()

	it := newTestInterpreter(t, DispatcherConfig{})
	rc := NewRunContext(RunContextConfig{})
	reporter := NewInMemoryReporter()

	root := seq(
		setStep("dup", map[string]string{"a": "1"}),
		setStep("dup", map[string]string{"b": "2"}),
	)

	res, err := it.Run(context.Background(), root, rc, reporter)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `duplicate step id "dup"`)
	assert.Equal(t, RunStatusFailed, res.Status)
	assert.False(t, IsRetryable(err))
	assert.Empty(t, reporter.States(), "validation runs before any step")
}

func TestInterpreter_DuplicateStepIDAcrossNesting(t *testing.T) {
	t.Parallel()

	it := newTestInterpreter(t, DispatcherConfig{})
	rc := NewRunContext(RunContextConfig{})

	root := seq(
		setStep("x", map[string]string{"a": "1"}),
		parallelStep("par", seq(setStep("x", map[string]string{"b": "2"}))),
	)

	_, err := it.Run(context.Background(), root, rc, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `duplicate step id "x"`)
}

func TestInterpreter_UnsetActivityKindIsTerminal(t *testing.T) {
	t.Parallel()

	it := newTestInterpreter(t, DispatcherConfig{})
	rc := NewRunContext(RunContextConfig{})

	emptyActivity := &workflowsv1.Step{
		Id:   "empty",
		Kind: &workflowsv1.Step_Activity{Activity: &workflowsv1.Activity{}},
	}

	_, err := it.Run(context.Background(), seq(emptyActivity), rc, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unset or unknown activity kind")
	assert.False(t, IsRetryable(err))
}

func TestInterpreter_UnregisteredActivityIsTerminal(t *testing.T) {
	t.Parallel()

	// Only Set is registered; an http step has no handler.
	it := newTestInterpreter(t, DispatcherConfig{})
	rc := NewRunContext(RunContextConfig{})

	_, err := it.Run(context.Background(), seq(httpStep("h")), rc, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not registered")
}

// --- test activities ------------------------------------------------------

func runWorkflowStep(id string) *workflowsv1.Step {
	return &workflowsv1.Step{
		Id: id,
		Kind: &workflowsv1.Step_Activity{
			Activity: &workflowsv1.Activity{
				Kind: &workflowsv1.Activity_RunWorkflow{RunWorkflow: &workflowsv1.RunWorkflowActivity{}},
			},
		},
	}
}

// stepOutputs reads back the step outputs a RunContext accumulated.
func stepOutputs(rc *RunContext) map[string]any {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	out := map[string]any{}
	for id, v := range rc.steps {
		out[id] = v.(map[string]any)[stepOutputKey]
	}
	return out
}

// barrierActivity succeeds only if n instances execute concurrently.
type barrierActivity struct {
	n       int
	mu      sync.Mutex
	count   int
	reached chan struct{}
}

func (b *barrierActivity) Execute(ctx context.Context, _ *RunContext, step *workflowsv1.Step) (any, error) {
	b.mu.Lock()
	b.count++
	if b.count == b.n {
		close(b.reached)
	}
	b.mu.Unlock()

	select {
	case <-b.reached:
		return step.GetId(), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(2 * time.Second):
		return nil, errors.New("barrier timeout: branches did not run concurrently")
	}
}

// fixedErrorActivity always fails with a fixed error.
type fixedErrorActivity struct{ err error }

func (a *fixedErrorActivity) Execute(context.Context, *RunContext, *workflowsv1.Step) (any, error) {
	return nil, a.err
}

// ctxWaitActivity blocks until ctx is cancelled (or a safety timeout). It
// records completion only if it finishes without cancellation.
type ctxWaitActivity struct{ completed chan struct{} }

func (a *ctxWaitActivity) Execute(ctx context.Context, _ *RunContext, _ *workflowsv1.Step) (any, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(2 * time.Second):
		close(a.completed)
		return "done", nil
	}
}
