package engine

import (
	"context"
	"fmt"

	workflowsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/workflows/v1"
)

// Activity executes one leaf activity of a workflow. The returned value becomes
// the step's output, exposed as `steps.<id>.output` in the run context.
//
// The interface is intentionally minimal: an activity reads its own
// configuration off the Step's oneof (e.g. step.GetActivity().GetSet()) and
// gets everything else — the CEL evaluator, and in later phases the connector
// broker and HTTP client — injected at construction time by the code that
// registers it in the [Dispatcher]. That keeps the signature stable as new
// activity kinds arrive: the interpreter and this interface never change, only
// a new impl and a new [DispatcherConfig] field.
//
// An activity returns a terminal error for bad configuration (that never gets
// more valid on retry) and wraps genuinely transient infra faults with
// [Retryable].
type Activity interface {
	Execute(ctx context.Context, rc *RunContext, step *workflowsv1.Step) (any, error)
}

// Dispatcher routes an activity step to the registered [Activity] for its oneof
// kind. Unregistered kinds and a step that isn't an activity are terminal
// errors. New activity kinds are added by extending [DispatcherConfig] and the
// switch here — the interpreter is untouched.
type Dispatcher struct {
	set         Activity
	httpActor   Activity
	runWorkflow Activity
}

// DispatcherConfig registers the [Activity] implementation for each kind. Every
// field is optional per phase: a nil field means that kind is unregistered, and
// dispatching to it is a terminal error. In 6a only Set is wired.
type DispatcherConfig struct {
	// Set handles `set` activities.
	Set Activity
	// HTTP handles `http` activities (wired in 6c).
	HTTP Activity
	// RunWorkflow handles `run_workflow` activities (wired in 6d).
	RunWorkflow Activity
}

// NewDispatcher builds a Dispatcher from cfg.
func NewDispatcher(cfg DispatcherConfig) *Dispatcher {
	return &Dispatcher{
		set:         cfg.Set,
		httpActor:   cfg.HTTP,
		runWorkflow: cfg.RunWorkflow,
	}
}

// Execute dispatches step's activity to its registered handler.
func (d *Dispatcher) Execute(ctx context.Context, rc *RunContext, step *workflowsv1.Step) (any, error) {
	activity := step.GetActivity()
	if activity == nil {
		return nil, fmt.Errorf("engine: step %q is not an activity", step.GetId())
	}

	switch activity.GetKind().(type) {
	case *workflowsv1.Activity_Set:
		return dispatchTo(ctx, rc, step, d.set, "set")
	case *workflowsv1.Activity_Http:
		return dispatchTo(ctx, rc, step, d.httpActor, "http")
	case *workflowsv1.Activity_RunWorkflow:
		return dispatchTo(ctx, rc, step, d.runWorkflow, "run_workflow")
	case *workflowsv1.Activity_Fail:
		// `fail` and `end` are intrinsic control-flow activities: they carry no
		// external dependencies, so they are handled here rather than registered
		// via DispatcherConfig. `fail` raises a catchable [failError]; the
		// interpreter wraps it with the throwing step id.
		return nil, &failError{message: failMessage(activity.GetFail())}
	case *workflowsv1.Activity_End:
		// `end` raises the success-terminate signal; the interpreter classifies
		// it to a COMPLETED run rather than a failure.
		return nil, errEnd
	default:
		return nil, fmt.Errorf("engine: step %q has an unset or unknown activity kind", step.GetId())
	}
}

// defaultFailMessage is used when a `fail` activity supplies no message.
const defaultFailMessage = "workflow failed via fail activity"

// failMessage returns the `fail` activity's message, or a default when empty.
func failMessage(fail *workflowsv1.FailActivity) string {
	if msg := fail.GetMessage(); msg != "" {
		return msg
	}
	return defaultFailMessage
}

func dispatchTo(
	ctx context.Context,
	rc *RunContext,
	step *workflowsv1.Step,
	activity Activity,
	kind string,
) (any, error) {
	if activity == nil {
		return nil, fmt.Errorf("engine: step %q uses the %q activity, which is not registered", step.GetId(), kind)
	}
	return activity.Execute(ctx, rc, step)
}
