package engine

import (
	"context"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	workflowsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/workflows/v1"
)

// RunStatus is the terminal outcome of an interpreted workflow run.
type RunStatus int

const (
	// RunStatusUnspecified is the zero value; never returned by Run.
	RunStatusUnspecified RunStatus = iota
	// RunStatusCompleted means the walk finished with no error.
	RunStatusCompleted
	// RunStatusFailed means a step returned an error (business failure or an
	// infra fault). Retryability is a separate axis — query the returned error
	// with [IsRetryable].
	RunStatusFailed
	// RunStatusCancelled means the run stopped because ctx was cancelled or its
	// deadline expired, distinct from a business failure.
	RunStatusCancelled
)

// Result is the outcome of [Interpreter.Run].
type Result struct {
	// Status is the terminal outcome.
	Status RunStatus
	// Steps holds one final [StepState] per activity step that finished or
	// failed, in completion order.
	Steps []StepState
	// Output is the run's output: a snapshot of the run variables (`vars`) at
	// termination. This is the day-1 output convention — a workflow "returns"
	// whatever it left in vars.
	Output map[string]any
}

// Interpreter tree-walks a workflow definition. It is constructed once with its
// fixed engine dependencies (the CEL evaluator and the activity dispatcher) and
// then invoked per run with a fresh [RunContext] and [StepReporter].
type Interpreter struct {
	eval     *Evaluator
	dispatch *Dispatcher
}

// InterpreterConfig configures a new [Interpreter].
type InterpreterConfig struct {
	// Evaluator is the shared CEL evaluator used for condition predicates.
	// Required.
	Evaluator *Evaluator
	// Dispatcher routes activity steps to their handlers. Required.
	Dispatcher *Dispatcher
}

// NewInterpreter builds an Interpreter. It panics on missing required
// dependencies — a startup-time programmer error, per the repo convention.
func NewInterpreter(cfg InterpreterConfig) *Interpreter {
	if cfg.Evaluator == nil {
		panic("engine: InterpreterConfig.Evaluator is required")
	}
	if cfg.Dispatcher == nil {
		panic("engine: InterpreterConfig.Dispatcher is required")
	}
	return &Interpreter{
		eval:     cfg.Evaluator,
		dispatch: cfg.Dispatcher,
	}
}

// Run walks root against rc, emitting per-step lifecycle to reporter (which may
// be nil), and returns the [Result] plus the propagated error.
//
// Outcomes:
//
//   - A nil error, or an `end` success-terminate signal, yields
//     [RunStatusCompleted]. An `end` resolves to success even though it unwinds
//     like an error, and Run returns a nil error for it — the signal never
//     surfaces to the caller.
//   - A context cancellation yields [RunStatusCancelled].
//   - Any other error yields [RunStatusFailed]. When the failure is an uncaught
//     terminal error (not retryable), errorSeq — the workflow's
//     `error_sequence`, which may be nil — runs with the failure exposed in CEL
//     scope for cleanup/notify/compensate, and the run is FAILED regardless of
//     the sequence's own outcome. errorSeq does NOT run on success, on an `end`,
//     on cancellation, or on a retryable (infra) fault (the worker re-runs the
//     whole job for those, so cleanup would double-fire).
//
// Step ids must be unique across root and errorSeq — a duplicate is a terminal
// error returned before any step runs.
func (it *Interpreter) Run(
	ctx context.Context,
	root *workflowsv1.Sequence,
	errorSeq *workflowsv1.Sequence,
	rc *RunContext,
	reporter StepReporter,
) (Result, error) {
	if reporter == nil {
		reporter = nopReporter{}
	}

	// Install the sub-run capability on ctx so a run_workflow activity can recurse
	// back into this interpreter without the Dispatcher holding it — that would be
	// a construction cycle (the Interpreter is built with a Dispatcher that
	// contains the activity). Re-installing on each (including nested) Run is
	// idempotent. See [subRunner].
	ctx = withSubRunner(ctx, it)

	if err := validateUniqueStepIDs(root, errorSeq); err != nil {
		return Result{Status: RunStatusFailed, Output: rc.VarsSnapshot()}, err
	}

	r := &run{
		eval:     it.eval,
		dispatch: it.dispatch,
		rc:       rc,
		reporter: reporter,
	}

	err := r.walkSequence(ctx, root)

	switch {
	case err == nil:
		return r.result(RunStatusCompleted), nil
	case isEndSignal(err):
		// `end` unwinds to a successful run; the signal is not a caller error.
		return r.result(RunStatusCompleted), nil
	case isContextError(err):
		return r.result(RunStatusCancelled), err
	case IsRetryable(err):
		// Infra fault: the worker hands the whole job back to River, so the
		// error_sequence must NOT run — it would re-fire on the next attempt.
		return r.result(RunStatusFailed), err
	default:
		// Uncaught terminal failure: run the error_sequence (if any) with the
		// failure in CEL scope, then FAIL regardless of its own outcome.
		r.runErrorSequence(ctx, errorSeq, err)
		return r.result(RunStatusFailed), err
	}
}

// result snapshots the current walk state into a [Result] with the given status.
func (r *run) result(status RunStatus) Result {
	return Result{
		Status: status,
		Steps:  r.snapshotStates(),
		Output: r.rc.VarsSnapshot(),
	}
}

// runErrorSequence walks errorSeq (the workflow-level error handler) with cause
// exposed as the CEL `error` record. Its purpose is best-effort cleanup, so its
// own outcome is discarded — the run fails either way. A nil/empty errorSeq is a
// no-op.
func (r *run) runErrorSequence(ctx context.Context, errorSeq *workflowsv1.Sequence, cause error) {
	if len(errorSeq.GetSteps()) == 0 {
		return
	}
	ectx := withErrorScope(ctx, buildErrorValue(cause))
	// Discard the error_sequence's outcome: the run is FAILED regardless.
	_ = r.walkSequence(ectx, errorSeq)
}

// run holds the per-invocation walk state. Its states slice is written from
// multiple goroutines (Parallel branches), so it is mutex-guarded.
type run struct {
	eval     *Evaluator
	dispatch *Dispatcher
	rc       *RunContext
	reporter StepReporter

	mu     sync.Mutex
	states []StepState
}

func (r *run) walkSequence(ctx context.Context, seq *workflowsv1.Sequence) error {
	for _, step := range seq.GetSteps() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := r.walkStep(ctx, step); err != nil {
			return err
		}
	}
	return nil
}

func (r *run) walkStep(ctx context.Context, step *workflowsv1.Step) error {
	switch step.GetKind().(type) {
	case *workflowsv1.Step_Activity:
		return r.runActivity(ctx, step)
	case *workflowsv1.Step_Condition:
		return r.runCondition(ctx, step)
	case *workflowsv1.Step_Parallel:
		return r.runParallel(ctx, step)
	case *workflowsv1.Step_Try:
		return r.runTry(ctx, step)
	default:
		return fmt.Errorf("engine: step %q has an unset or unknown kind", step.GetId())
	}
}

func (r *run) runActivity(ctx context.Context, step *workflowsv1.Step) error {
	startedAt := time.Now()
	r.reporter.StepStarted(ctx, step.GetId(), startedAt)

	output, err := r.dispatch.Execute(ctx, r.rc, step)
	finishedAt := time.Now()
	if err != nil {
		// An `end` activity raises the success-terminate signal, not a failure:
		// record the step as succeeded (it fired), then propagate the signal so
		// it unwinds every enclosing block.
		if isEndSignal(err) {
			r.recordState(StepState{
				ID:         step.GetId(),
				Status:     StepStatusSucceeded,
				StartedAt:  startedAt,
				FinishedAt: finishedAt,
			})
			r.reporter.StepFinished(ctx, step.GetId(), nil, startedAt, finishedAt)
			return err
		}

		// Any other activity error is a catchable failure. Wrap it with the
		// throwing step id so a catch / error_sequence can populate error.step;
		// the wrapper preserves the cause for errors.Is/As (retryability, HTTP
		// detail) and message-based inspection.
		thrown := &thrownError{stepID: step.GetId(), cause: err}
		r.recordState(StepState{
			ID:         step.GetId(),
			Status:     StepStatusFailed,
			Err:        thrown,
			StartedAt:  startedAt,
			FinishedAt: finishedAt,
		})
		r.reporter.StepFailed(ctx, step.GetId(), thrown, startedAt, finishedAt)
		return thrown
	}

	r.rc.SetStepOutput(step.GetId(), output)
	r.recordState(StepState{
		ID:         step.GetId(),
		Status:     StepStatusSucceeded,
		Output:     output,
		StartedAt:  startedAt,
		FinishedAt: finishedAt,
	})
	r.reporter.StepFinished(ctx, step.GetId(), output, startedAt, finishedAt)
	return nil
}

// runTry runs the Try body, and on a catchable failure runs the catch block with
// the error exposed in CEL scope. An `end` signal and context cancellation are
// NOT catchable — they unwind straight through. If the catch completes and
// rethrow is false the failure is HANDLED and the flow continues; if rethrow is
// true the ORIGINAL error is re-raised after the catch runs; if the catch itself
// raises, that new error (or `end`) propagates.
func (r *run) runTry(ctx context.Context, step *workflowsv1.Step) error {
	try := step.GetTry()

	bodyErr := r.walkSequence(ctx, try.GetBody())
	if bodyErr == nil {
		return nil
	}
	if isEndSignal(bodyErr) || isContextError(bodyErr) {
		return bodyErr
	}

	// Run the catch handler with the caught error resolvable as `error`.
	catchCtx := withErrorScope(ctx, buildErrorValue(bodyErr))
	if catchErr := r.walkSequence(catchCtx, try.GetCatch()); catchErr != nil {
		// The catch raised its own failure (or `end`); that supersedes the
		// original and propagates.
		return catchErr
	}

	if try.GetRethrow() {
		return bodyErr
	}
	return nil
}

func (r *run) runCondition(ctx context.Context, step *workflowsv1.Step) error {
	cond := step.GetCondition()
	for _, branch := range cond.GetBranches() {
		match, err := r.eval.EvalBool(ctx, branch.GetWhen(), r.rc)
		if err != nil {
			return err
		}
		if match {
			return r.walkSequence(ctx, branch.GetThen())
		}
	}
	// No branch matched; run the else block if present, otherwise a no-op.
	if otherwise := cond.GetOtherwise(); otherwise != nil {
		return r.walkSequence(ctx, otherwise)
	}
	return nil
}

func (r *run) runParallel(ctx context.Context, step *workflowsv1.Step) error {
	// errgroup.WithContext cancels gctx on the first branch error; siblings
	// observe cancellation at their next ctx check (between steps, during CEL
	// eval, and inside activities that honor ctx). Wait joins all branches and
	// returns the first error.
	g, gctx := errgroup.WithContext(ctx)
	for _, branch := range step.GetParallel().GetBranches() {
		g.Go(func() error {
			return r.walkSequence(gctx, branch)
		})
	}
	return g.Wait()
}

func (r *run) recordState(s StepState) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.states = append(r.states, s)
}

func (r *run) snapshotStates() []StepState {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]StepState, len(r.states))
	copy(out, r.states)
	return out
}

// validateUniqueStepIDs enforces that every step id across root and errorSeq is
// unique. The interpreter relies on this to key step outputs (steps.<id>.output)
// — including a catch block or the error_sequence, which read prior outputs — so
// a collision is a terminal definition error. errorSeq may be nil.
func validateUniqueStepIDs(root, errorSeq *workflowsv1.Sequence) error {
	seen := map[string]struct{}{}
	if err := checkSequenceIDs(root, seen); err != nil {
		return err
	}
	return checkSequenceIDs(errorSeq, seen)
}

func checkSequenceIDs(seq *workflowsv1.Sequence, seen map[string]struct{}) error {
	for _, step := range seq.GetSteps() {
		id := step.GetId()
		if _, dup := seen[id]; dup {
			return fmt.Errorf("engine: duplicate step id %q", id)
		}
		seen[id] = struct{}{}

		switch step.GetKind().(type) {
		case *workflowsv1.Step_Condition:
			cond := step.GetCondition()
			for _, branch := range cond.GetBranches() {
				if err := checkSequenceIDs(branch.GetThen(), seen); err != nil {
					return err
				}
			}
			if err := checkSequenceIDs(cond.GetOtherwise(), seen); err != nil {
				return err
			}
		case *workflowsv1.Step_Parallel:
			for _, branch := range step.GetParallel().GetBranches() {
				if err := checkSequenceIDs(branch, seen); err != nil {
					return err
				}
			}
		case *workflowsv1.Step_Try:
			try := step.GetTry()
			if err := checkSequenceIDs(try.GetBody(), seen); err != nil {
				return err
			}
			if err := checkSequenceIDs(try.GetCatch(), seen); err != nil {
				return err
			}
		}
	}
	return nil
}

// nopReporter is the default [StepReporter] when the caller passes nil.
type nopReporter struct{}

func (nopReporter) StepStarted(context.Context, string, time.Time)                  {}
func (nopReporter) StepFinished(context.Context, string, any, time.Time, time.Time) {}
func (nopReporter) StepFailed(context.Context, string, error, time.Time, time.Time) {}
