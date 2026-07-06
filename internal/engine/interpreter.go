package engine

import (
	"context"
	"errors"
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
// be nil), and returns the [Result] plus the propagated error. A nil error is a
// completed run; a context cancellation yields [RunStatusCancelled]; any other
// error yields [RunStatusFailed]. Step ids must be unique within the version —
// a duplicate is a terminal error returned before any step runs.
func (it *Interpreter) Run(
	ctx context.Context,
	root *workflowsv1.Sequence,
	rc *RunContext,
	reporter StepReporter,
) (Result, error) {
	if reporter == nil {
		reporter = nopReporter{}
	}

	if err := validateUniqueStepIDs(root); err != nil {
		return Result{Status: RunStatusFailed, Output: rc.VarsSnapshot()}, err
	}

	r := &run{
		eval:     it.eval,
		dispatch: it.dispatch,
		rc:       rc,
		reporter: reporter,
	}

	err := r.walkSequence(ctx, root)

	result := Result{
		Steps:  r.snapshotStates(),
		Output: rc.VarsSnapshot(),
	}
	switch {
	case err == nil:
		result.Status = RunStatusCompleted
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		result.Status = RunStatusCancelled
	default:
		result.Status = RunStatusFailed
	}
	return result, err
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
		r.recordState(StepState{
			ID:         step.GetId(),
			Status:     StepStatusFailed,
			Err:        err,
			StartedAt:  startedAt,
			FinishedAt: finishedAt,
		})
		r.reporter.StepFailed(ctx, step.GetId(), err, startedAt, finishedAt)
		return err
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

// validateUniqueStepIDs enforces that every step id in the version is unique.
// The interpreter relies on this to key step outputs (steps.<id>.output); a
// collision is a terminal definition error.
func validateUniqueStepIDs(root *workflowsv1.Sequence) error {
	seen := map[string]struct{}{}
	return checkSequenceIDs(root, seen)
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
		}
	}
	return nil
}

// nopReporter is the default [StepReporter] when the caller passes nil.
type nopReporter struct{}

func (nopReporter) StepStarted(context.Context, string, time.Time)                  {}
func (nopReporter) StepFinished(context.Context, string, any, time.Time, time.Time) {}
func (nopReporter) StepFailed(context.Context, string, error, time.Time, time.Time) {}
