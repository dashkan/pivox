package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	workflowsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/workflows/v1"
)

// --- proto builders for the error-handling constructs ---------------------

func failStep(id, message string) *workflowsv1.Step {
	return &workflowsv1.Step{
		Id: id,
		Kind: &workflowsv1.Step_Activity{
			Activity: &workflowsv1.Activity{
				Kind: &workflowsv1.Activity_Fail{Fail: &workflowsv1.FailActivity{Message: message}},
			},
		},
	}
}

func endStep(id string) *workflowsv1.Step {
	return &workflowsv1.Step{
		Id: id,
		Kind: &workflowsv1.Step_Activity{
			Activity: &workflowsv1.Activity{
				Kind: &workflowsv1.Activity_End{End: &workflowsv1.EndActivity{}},
			},
		},
	}
}

func tryStep(id string, body, catch *workflowsv1.Sequence, rethrow bool) *workflowsv1.Step {
	return &workflowsv1.Step{
		Id: id,
		Kind: &workflowsv1.Step_Try{
			Try: &workflowsv1.Try{Body: body, Catch: catch, Rethrow: rethrow},
		},
	}
}

// fakeHTTPError is a terminal error carrying HTTP detail. It satisfies the
// engine's unexported httpErrorDetail interface (this test is white-box), so it
// stands in for connector.ResponseError without importing connector.
type fakeHTTPError struct {
	status int
	body   []byte
}

func (e *fakeHTTPError) Error() string    { return "http request failed" }
func (e *fakeHTTPError) HTTPStatus() int  { return e.status }
func (e *fakeHTTPError) HTTPBody() []byte { return e.body }

// --- Try -------------------------------------------------------------------

func TestTry_BodySucceeds_CatchSkipped(t *testing.T) {
	t.Parallel()

	it := newTestInterpreter(t, DispatcherConfig{})
	rc := NewRunContext(RunContextConfig{})

	root := seq(
		tryStep("t",
			seq(setStep("body", map[string]string{"ran": "true"})),
			seq(setStep("handler", map[string]string{"handled": "true"})),
			false,
		),
		setStep("after", map[string]string{"after": "true"}),
	)

	res, err := it.Run(context.Background(), root, nil, rc, nil)
	require.NoError(t, err)
	assert.Equal(t, RunStatusCompleted, res.Status)
	assert.Equal(t, true, res.Output["ran"])
	assert.Equal(t, true, res.Output["after"])
	assert.NotContains(t, res.Output, "handled", "catch must not run when body succeeds")
}

func TestTry_BodyFails_CatchHandlesAndFlowContinues(t *testing.T) {
	t.Parallel()

	it := newTestInterpreter(t, DispatcherConfig{})
	rc := NewRunContext(RunContextConfig{})

	root := seq(
		tryStep("t",
			seq(failStep("boom", "kaboom")),
			seq(setStep("handler", map[string]string{"handled": "true"})),
			false,
		),
		setStep("after", map[string]string{"after": "true"}),
	)

	res, err := it.Run(context.Background(), root, nil, rc, nil)
	require.NoError(t, err, "a handled failure is not a run error")
	assert.Equal(t, RunStatusCompleted, res.Status)
	assert.Equal(t, true, res.Output["handled"])
	assert.Equal(t, true, res.Output["after"], "flow continues past the Try after handling")
}

func TestTry_CatchReadsErrorScope(t *testing.T) {
	t.Parallel()

	it := newTestInterpreter(t, DispatcherConfig{})
	rc := NewRunContext(RunContextConfig{})

	root := seq(tryStep("t",
		seq(failStep("boom", "kaboom")),
		seq(setStep("handler", map[string]string{
			"msg":  "error.message",
			"code": "error.code",
			"step": "error.step",
		})),
		false,
	))

	res, err := it.Run(context.Background(), root, nil, rc, nil)
	require.NoError(t, err)
	assert.Equal(t, "kaboom", res.Output["msg"])
	assert.Equal(t, "FAIL", res.Output["code"])
	assert.Equal(t, "boom", res.Output["step"])
}

func TestTry_CatchReadsHTTPErrorDetail(t *testing.T) {
	t.Parallel()

	httpErr := &fakeHTTPError{status: 404, body: []byte(`{"error":"not found"}`)}
	it := newTestInterpreter(t, DispatcherConfig{HTTP: &fixedErrorActivity{err: httpErr}})
	rc := NewRunContext(RunContextConfig{})

	root := seq(tryStep("t",
		seq(httpStep("call")),
		seq(setStep("handler", map[string]string{
			"st":   "error.status",
			"bd":   "error.body",
			"code": "error.code",
		})),
		false,
	))

	res, err := it.Run(context.Background(), root, nil, rc, nil)
	require.NoError(t, err)
	assert.Equal(t, int64(404), res.Output["st"])
	assert.Equal(t, `{"error":"not found"}`, res.Output["bd"])
	// A non-fail activity failure classifies as ACTIVITY_FAILED.
	assert.Equal(t, "ACTIVITY_FAILED", res.Output["code"])
}

func TestTry_ErrorNotResolvableInNormalStep(t *testing.T) {
	t.Parallel()

	it := newTestInterpreter(t, DispatcherConfig{})
	rc := NewRunContext(RunContextConfig{})

	// `error` is only declared in the catch environment; referencing it in a
	// normal step must fail to COMPILE (not merely resolve to nothing).
	root := seq(setStep("bad", map[string]string{"x": "error.message"}))

	res, err := it.Run(context.Background(), root, nil, rc, nil)
	require.Error(t, err)
	assert.Equal(t, RunStatusFailed, res.Status)
	assert.Contains(t, err.Error(), "compiling CEL expression")
	assert.False(t, IsRetryable(err))
}

func TestTry_RethrowReRaisesOriginalAfterCatch(t *testing.T) {
	t.Parallel()

	it := newTestInterpreter(t, DispatcherConfig{})
	rc := NewRunContext(RunContextConfig{})

	root := seq(
		tryStep("t",
			seq(failStep("boom", "original failure")),
			seq(setStep("handler", map[string]string{"handled": "true"})),
			true, // rethrow
		),
		setStep("after", map[string]string{"after": "true"}),
	)

	res, err := it.Run(context.Background(), root, nil, rc, nil)
	require.Error(t, err)
	assert.Equal(t, RunStatusFailed, res.Status)
	assert.Contains(t, err.Error(), "original failure", "the original error is re-raised")
	assert.Equal(t, true, res.Output["handled"], "the catch still ran before rethrow")
	assert.NotContains(t, res.Output, "after", "rethrow re-raises, so the flow does not continue")
}

func TestTry_CatchFailurePropagates(t *testing.T) {
	t.Parallel()

	it := newTestInterpreter(t, DispatcherConfig{})
	rc := NewRunContext(RunContextConfig{})

	root := seq(tryStep("t",
		seq(failStep("boom", "first failure")),
		seq(failStep("boom2", "second failure")),
		false,
	))

	res, err := it.Run(context.Background(), root, nil, rc, nil)
	require.Error(t, err)
	assert.Equal(t, RunStatusFailed, res.Status)
	assert.Contains(t, err.Error(), "second failure", "the catch's own failure supersedes the original")
}

func TestTry_NestedInnerCatchesOuterUnaware(t *testing.T) {
	t.Parallel()

	it := newTestInterpreter(t, DispatcherConfig{})
	rc := NewRunContext(RunContextConfig{})

	root := seq(tryStep("outer",
		seq(
			tryStep("inner",
				seq(failStep("boom", "inner failure")),
				seq(setStep("innerHandler", map[string]string{"inner": "true"})),
				false,
			),
			setStep("afterInner", map[string]string{"afterInner": "true"}),
		),
		seq(setStep("outerHandler", map[string]string{"outer": "true"})),
		false,
	))

	res, err := it.Run(context.Background(), root, nil, rc, nil)
	require.NoError(t, err)
	assert.Equal(t, true, res.Output["inner"], "inner catch handled the failure")
	assert.Equal(t, true, res.Output["afterInner"], "inner Try's siblings still run")
	assert.NotContains(t, res.Output, "outer", "outer catch never sees a failure the inner handled")
}

// --- Fail ------------------------------------------------------------------

func TestFail_UncaughtFailsRun(t *testing.T) {
	t.Parallel()

	it := newTestInterpreter(t, DispatcherConfig{})
	rc := NewRunContext(RunContextConfig{})

	root := seq(
		failStep("boom", "explicit failure"),
		setStep("after", map[string]string{"after": "true"}),
	)

	res, err := it.Run(context.Background(), root, nil, rc, nil)
	require.Error(t, err)
	assert.Equal(t, RunStatusFailed, res.Status)
	assert.Contains(t, err.Error(), "explicit failure")
	assert.False(t, IsRetryable(err), "a fail is never retryable")
	assert.Equal(t, map[string]StepStatus{"boom": StepStatusFailed}, statuses(res.Steps))
}

func TestFail_DefaultMessageWhenEmpty(t *testing.T) {
	t.Parallel()

	it := newTestInterpreter(t, DispatcherConfig{})
	rc := NewRunContext(RunContextConfig{})

	res, err := it.Run(context.Background(), seq(failStep("boom", "")), nil, rc, nil)
	require.Error(t, err)
	assert.Equal(t, RunStatusFailed, res.Status)
	assert.Contains(t, err.Error(), defaultFailMessage)
}

func TestFail_InsideTryIsCaught(t *testing.T) {
	t.Parallel()

	it := newTestInterpreter(t, DispatcherConfig{})
	rc := NewRunContext(RunContextConfig{})

	root := seq(
		tryStep("t",
			seq(failStep("boom", "x")),
			seq(setStep("handler", map[string]string{"handled": "true"})),
			false,
		),
		setStep("after", map[string]string{"after": "true"}),
	)

	res, err := it.Run(context.Background(), root, nil, rc, nil)
	require.NoError(t, err)
	assert.Equal(t, RunStatusCompleted, res.Status)
	assert.Equal(t, true, res.Output["handled"])
	assert.Equal(t, true, res.Output["after"])
}

func TestFail_InsideCatchPropagates(t *testing.T) {
	t.Parallel()

	it := newTestInterpreter(t, DispatcherConfig{})
	rc := NewRunContext(RunContextConfig{})

	root := seq(tryStep("t",
		seq(setStep("body", map[string]string{"ran": "true"})),
		// This catch never runs (body succeeds); prove a fail inside a catch
		// propagates by making the body fail and the catch fail.
		seq(failStep("catchFail", "catch failed")),
		false,
	))
	// Force the body to fail so the catch runs.
	root.Steps[0].GetTry().Body = seq(failStep("boom", "body failed"))

	res, err := it.Run(context.Background(), root, nil, rc, nil)
	require.Error(t, err)
	assert.Equal(t, RunStatusFailed, res.Status)
	assert.Contains(t, err.Error(), "catch failed", "a fail raised inside the catch propagates")
}

// --- End -------------------------------------------------------------------

func TestEnd_EndsRunSucceededAndSkipsRest(t *testing.T) {
	t.Parallel()

	it := newTestInterpreter(t, DispatcherConfig{})
	rc := NewRunContext(RunContextConfig{})

	root := seq(
		setStep("a", map[string]string{"x": "1"}),
		endStep("end"),
		setStep("b", map[string]string{"y": "2"}),
	)

	res, err := it.Run(context.Background(), root, nil, rc, nil)
	require.NoError(t, err, "end is a success, not an error")
	assert.Equal(t, RunStatusCompleted, res.Status)
	assert.Equal(t, map[string]any{"x": int64(1)}, res.Output, "output is vars at the end; b never ran")
	assert.Equal(t,
		map[string]StepStatus{"a": StepStatusSucceeded, "end": StepStatusSucceeded},
		statuses(res.Steps),
	)
}

func TestEnd_InParallelUnwindsAndSucceeds(t *testing.T) {
	t.Parallel()

	// The sibling blocks until ctx is cancelled; End must cancel it and the run
	// must be SUCCEEDED (not Cancelled/Failed).
	sib := &ctxWaitActivity{completed: make(chan struct{})}
	it := newTestInterpreter(t, DispatcherConfig{HTTP: sib})
	rc := NewRunContext(RunContextConfig{})

	root := seq(
		parallelStep("par",
			seq(endStep("end")),
			seq(httpStep("sibling")),
		),
		setStep("after", map[string]string{"after": "true"}),
	)

	res, err := it.Run(context.Background(), root, nil, rc, nil)
	require.NoError(t, err)
	assert.Equal(t, RunStatusCompleted, res.Status, "end resolves the run to success even from a parallel branch")
	assert.NotContains(t, res.Output, "after", "end unwinds past the parallel; later steps do not run")

	select {
	case <-sib.completed:
		t.Fatal("sibling should have been cancelled by end, not completed")
	default:
	}
}

func TestEnd_NotCaughtByTry(t *testing.T) {
	t.Parallel()

	it := newTestInterpreter(t, DispatcherConfig{})
	rc := NewRunContext(RunContextConfig{})

	root := seq(
		tryStep("t",
			seq(endStep("end")),
			seq(setStep("handler", map[string]string{"handled": "true"})),
			false,
		),
		setStep("after", map[string]string{"after": "true"}),
	)

	res, err := it.Run(context.Background(), root, nil, rc, nil)
	require.NoError(t, err)
	assert.Equal(t, RunStatusCompleted, res.Status)
	assert.NotContains(t, res.Output, "handled", "a Try does not intercept an end")
	assert.NotContains(t, res.Output, "after", "end unwinds past the Try")
}

// --- error_sequence --------------------------------------------------------

func TestErrorSequence_RunsOnUncaughtFailureThenFails(t *testing.T) {
	t.Parallel()

	it := newTestInterpreter(t, DispatcherConfig{})
	rc := NewRunContext(RunContextConfig{})

	root := seq(failStep("boom", "root failed"))
	errorSeq := seq(setStep("cleanup", map[string]string{
		"cleaned": "true",
		"reason":  "error.message",
		"step":    "error.step",
	}))

	res, err := it.Run(context.Background(), root, errorSeq, rc, nil)
	require.Error(t, err)
	assert.Equal(t, RunStatusFailed, res.Status)
	assert.Contains(t, err.Error(), "root failed")
	assert.Equal(t, true, res.Output["cleaned"], "error_sequence ran")
	assert.Equal(t, "root failed", res.Output["reason"], "error is in the error_sequence scope")
	assert.Equal(t, "boom", res.Output["step"])
	// The cleanup step is recorded so the run's step history shows it ran.
	assert.Equal(t, StepStatusSucceeded, statuses(res.Steps)["cleanup"])
}

func TestErrorSequence_DoesNotRunOnSuccess(t *testing.T) {
	t.Parallel()

	it := newTestInterpreter(t, DispatcherConfig{})
	rc := NewRunContext(RunContextConfig{})

	root := seq(setStep("ok", map[string]string{"x": "1"}))
	errorSeq := seq(setStep("cleanup", map[string]string{"cleaned": "true"}))

	res, err := it.Run(context.Background(), root, errorSeq, rc, nil)
	require.NoError(t, err)
	assert.Equal(t, RunStatusCompleted, res.Status)
	assert.NotContains(t, res.Output, "cleaned", "error_sequence does not run on success")
}

func TestErrorSequence_DoesNotRunOnEnd(t *testing.T) {
	t.Parallel()

	it := newTestInterpreter(t, DispatcherConfig{})
	rc := NewRunContext(RunContextConfig{})

	root := seq(endStep("end"))
	errorSeq := seq(setStep("cleanup", map[string]string{"cleaned": "true"}))

	res, err := it.Run(context.Background(), root, errorSeq, rc, nil)
	require.NoError(t, err)
	assert.Equal(t, RunStatusCompleted, res.Status)
	assert.NotContains(t, res.Output, "cleaned", "error_sequence does not run on end")
}

func TestErrorSequence_FailingErrorSequenceStillFails(t *testing.T) {
	t.Parallel()

	it := newTestInterpreter(t, DispatcherConfig{})
	rc := NewRunContext(RunContextConfig{})

	root := seq(failStep("boom", "root failure"))
	errorSeq := seq(failStep("cleanupFail", "cleanup failure"))

	res, err := it.Run(context.Background(), root, errorSeq, rc, nil)
	require.Error(t, err)
	assert.Equal(t, RunStatusFailed, res.Status)
	// The run surfaces the ORIGINAL failure; the error_sequence's own outcome
	// is discarded (but the run still FAILs).
	assert.Contains(t, err.Error(), "root failure")
}

func TestErrorSequence_DoesNotRunOnRetryable(t *testing.T) {
	t.Parallel()

	// A retryable (infra) fault hands the whole job back to River, so the
	// error_sequence must not run — it would re-fire on the next attempt.
	it := newTestInterpreter(t, DispatcherConfig{
		HTTP: &fixedErrorActivity{err: Retryable(errors.New("upstream 503"))},
	})
	rc := NewRunContext(RunContextConfig{})

	root := seq(httpStep("flaky"))
	errorSeq := seq(setStep("cleanup", map[string]string{"cleaned": "true"}))

	res, err := it.Run(context.Background(), root, errorSeq, rc, nil)
	require.Error(t, err)
	assert.Equal(t, RunStatusFailed, res.Status)
	assert.True(t, IsRetryable(err))
	assert.NotContains(t, res.Output, "cleaned", "error_sequence must not run on a retryable fault")
}

// --- validation ------------------------------------------------------------

func TestValidation_DuplicateIDAcrossTryAndErrorSequence(t *testing.T) {
	t.Parallel()

	it := newTestInterpreter(t, DispatcherConfig{})
	rc := NewRunContext(RunContextConfig{})

	// A Try body id and an error_sequence id collide.
	root := seq(tryStep("t",
		seq(setStep("dup", map[string]string{"a": "1"})),
		nil,
		false,
	))
	errorSeq := seq(setStep("dup", map[string]string{"b": "2"}))

	_, err := it.Run(context.Background(), root, errorSeq, rc, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `duplicate step id "dup"`)
}
