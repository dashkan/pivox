package engine

import (
	"context"
	"fmt"

	workflowsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/workflows/v1"
)

// SetActivity is the `set` activity: the data-transformation primitive. Each
// SetActivity.Assignments entry maps a variable name to a CEL expression;
// SetActivity evaluates every expression against the current run context, then
// writes each result with rc.SetVar. Its step output is the map of assigned
// name -> value.
//
// All expressions are evaluated against the same pre-set snapshot before any
// SetVar is applied. Because SetActivity.Assignments is a proto map (unordered),
// this makes the activity deterministic: one assignment cannot observe another
// from the same `set`. Cross-assignment references are an authoring mistake —
// split them across two `set` steps if ordering is required.
type SetActivity struct {
	eval *Evaluator
}

var _ Activity = (*SetActivity)(nil)

// SetActivityConfig configures a [SetActivity].
type SetActivityConfig struct {
	// Evaluator is the shared CEL evaluator. Required.
	Evaluator *Evaluator
}

// NewSetActivity builds a SetActivity. It panics if the evaluator is missing —
// a startup-time programmer error, per the repo constructor convention.
func NewSetActivity(cfg SetActivityConfig) *SetActivity {
	if cfg.Evaluator == nil {
		panic("engine: SetActivityConfig.Evaluator is required")
	}
	return &SetActivity{eval: cfg.Evaluator}
}

// Execute implements [Activity].
func (a *SetActivity) Execute(ctx context.Context, rc *RunContext, step *workflowsv1.Step) (any, error) {
	set := step.GetActivity().GetSet()
	if set == nil {
		return nil, fmt.Errorf("engine: step %q is not a set activity", step.GetId())
	}

	assignments := set.GetAssignments()
	results := make(map[string]any, len(assignments))

	// Evaluate every expression against the current context first; a CEL error
	// is terminal (a bad expression won't validate on retry).
	for name, expr := range assignments {
		value, err := a.eval.EvalAny(ctx, expr, rc)
		if err != nil {
			return nil, fmt.Errorf("engine: step %q assignment %q: %w", step.GetId(), name, err)
		}
		results[name] = value
	}

	// Apply all writes after evaluation so intra-set ordering is irrelevant.
	for name, value := range results {
		rc.SetVar(name, value)
	}

	return results, nil
}
