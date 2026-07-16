package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/encoding/protojson"

	db "github.com/dashkan/pivox/internal/db/generated"
	workflowsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/workflows/v1"
)

// maxSubWorkflowDepth caps how deep run_workflow recursion may nest, counting
// the root run as depth 1. Reaching the cap fails the offending run_workflow
// step terminally, bounding stack growth (and runaway fan-in) independently of
// the cycle guard — a long acyclic chain is refused even though it has no cycle.
const maxSubWorkflowDepth = 16

// subWorkflowTriggerKind is the marker a sub-run sees at `trigger.kind`,
// mirroring the worker's top-level {"kind":"MANUAL"} convention. A sub-run is
// never fired by an event; it is invoked in-process by a run_workflow activity.
var subWorkflowTriggerKind = workflowsv1.RunTriggerKind_SUB_WORKFLOW.String()

// subRunner is the interpreter capability the run_workflow activity needs:
// recurse into a sub-workflow in-process, within the SAME run. It is carried on
// ctx (installed by [Interpreter.Run]) rather than injected at construction —
// that would be a construction cycle, since the Interpreter is built with a
// Dispatcher that already contains this activity. *Interpreter satisfies it via
// its existing Run, so no extra method or nil-able setter is needed.
type subRunner interface {
	Run(
		ctx context.Context,
		root *workflowsv1.Sequence,
		errorSeq *workflowsv1.Sequence,
		rc *RunContext,
		reporter StepReporter,
	) (Result, error)
}

var _ subRunner = (*Interpreter)(nil)

type subRunnerKey struct{}

// withSubRunner installs the sub-run capability on ctx. Called by
// [Interpreter.Run] at the top of every walk.
func withSubRunner(ctx context.Context, r subRunner) context.Context {
	return context.WithValue(ctx, subRunnerKey{}, r)
}

// subRunnerFrom returns the sub-run capability carried on ctx, if any.
func subRunnerFrom(ctx context.Context) (subRunner, bool) {
	r, ok := ctx.Value(subRunnerKey{}).(subRunner)
	return r, ok
}

type workflowStackKey struct{}

// withWorkflowStack returns a context carrying the given call stack of workflow
// ids currently executing (root first, deepest last). A fresh slice per push
// keeps sibling branches from aliasing each other's stack.
func withWorkflowStack(ctx context.Context, stack []uuid.UUID) context.Context {
	return context.WithValue(ctx, workflowStackKey{}, stack)
}

// workflowStackFrom returns the call stack of workflow ids currently executing,
// or nil at the top level (nothing has recursed yet).
func workflowStackFrom(ctx context.Context) []uuid.UUID {
	stack, _ := ctx.Value(workflowStackKey{}).([]uuid.UUID)
	return stack
}

// WorkflowStore is the narrow read surface the run_workflow activity needs to
// resolve a target: the workflow container (for scope + promoted-version
// pointer) and either the promoted version or a pinned version by number.
// db.Querier satisfies it in production.
type WorkflowStore interface {
	// GetWorkflowByParent resolves the target container by its resource-name
	// slug, SCOPED to the run's org+space (the scoped lookup is the cross-scope
	// guard). GetWorkflowVersion / GetWorkflowVersionByNumber then resolve the
	// version by the container's internal uuid / monotonic number.
	GetWorkflowByParent(ctx context.Context, arg db.GetWorkflowByParentParams) (db.Workflow, error)
	GetWorkflowVersion(ctx context.Context, id uuid.UUID) (db.WorkflowVersion, error)
	GetWorkflowVersionByNumber(ctx context.Context, arg db.GetWorkflowVersionByNumberParams) (db.WorkflowVersion, error)
}

// db.Querier is the production WorkflowStore.
var _ WorkflowStore = db.Querier(nil)

// RunWorkflowActivity is the `run_workflow` activity: the composition primitive
// by which a workflow invokes another workflow as a sub-workflow. The sub-run
// executes IN-PROCESS — synchronous recursion into the interpreter within the
// same run — not as a separate WorkflowRun or River job.
//
// The sub-run is isolated: it sees ONLY the parameters passed to it (each a CEL
// expression evaluated on the PARENT env), with its own empty vars/steps and a
// SUB_WORKFLOW trigger marker. Its scope is inherited from the parent — for day
// one the sub-run executes under the parent run's scope; a least-privilege
// sub-workflow identity is deferred with the KC-run-identity work, so a
// cross-scope target is refused today.
//
// Recursion is bounded by a cycle guard and a depth cap carried on ctx (see
// [maxSubWorkflowDepth]). The sub-run's output (its final vars) becomes this
// step's output, read by the parent as steps.<id>.output; a sub-run failure is
// this activity's error, catchable by a parent Try.
type RunWorkflowActivity struct {
	eval  *Evaluator
	store WorkflowStore
}

var _ Activity = (*RunWorkflowActivity)(nil)

// RunWorkflowActivityConfig configures a [RunWorkflowActivity].
type RunWorkflowActivityConfig struct {
	// Evaluator is the shared run-context CEL evaluator, used to evaluate the
	// activity's `parameters` on the parent env. Required.
	Evaluator *Evaluator
	// Store resolves the target workflow/version. Required.
	Store WorkflowStore
}

// NewRunWorkflowActivity builds a RunWorkflowActivity. It panics on a missing
// required dependency, per the repo constructor convention.
func NewRunWorkflowActivity(cfg RunWorkflowActivityConfig) *RunWorkflowActivity {
	if cfg.Evaluator == nil {
		panic("engine: RunWorkflowActivityConfig.Evaluator is required")
	}
	if cfg.Store == nil {
		panic("engine: RunWorkflowActivityConfig.Store is required")
	}
	return &RunWorkflowActivity{eval: cfg.Evaluator, store: cfg.Store}
}

// Execute implements [Activity].
func (a *RunWorkflowActivity) Execute(ctx context.Context, rc *RunContext, step *workflowsv1.Step) (any, error) {
	spec := step.GetActivity().GetRunWorkflow()
	if spec == nil {
		return nil, fmt.Errorf("engine: step %q is not a run_workflow activity", step.GetId())
	}

	runner, ok := subRunnerFrom(ctx)
	if !ok {
		// Interpreter.Run always installs the capability at the top of the walk,
		// so its absence means this ran outside an interpreter walk — a wiring bug.
		return nil, fmt.Errorf("engine: step %q run_workflow has no sub-runner on context", step.GetId())
	}

	target, err := a.resolveTarget(ctx, rc, spec.GetWorkflow())
	if err != nil {
		return nil, err
	}

	// Cycle guard + depth cap, carried on ctx. Seed the stack with the currently
	// executing workflow so a self-call at the top level is detected (at deeper
	// levels the stack is already non-empty, carried in from the enclosing call).
	stack := workflowStackFrom(ctx)
	if len(stack) == 0 {
		stack = []uuid.UUID{rc.WorkflowID()}
	}
	if slices.Contains(stack, target.workflowID) {
		return nil, cycleError(stack, target.workflowID)
	}
	if len(stack) >= maxSubWorkflowDepth {
		return nil, fmt.Errorf(
			"engine: step %q run_workflow exceeds the sub-workflow depth cap (%d)",
			step.GetId(), maxSubWorkflowDepth,
		)
	}
	// Clone before appending so a sibling branch sharing the parent stack can't
	// observe this push (append may reuse the backing array).
	childStack := append(slices.Clone(stack), target.workflowID)
	subCtx := withWorkflowStack(ctx, childStack)

	params, err := a.evalParams(ctx, rc, spec.GetParameters())
	if err != nil {
		return nil, err
	}

	orgID, spaceID := rc.Scope()
	subRC := NewRunContext(RunContextConfig{
		Trigger:    map[string]any{"kind": subWorkflowTriggerKind},
		Params:     params,
		OrgID:      orgID,
		SpaceID:    spaceID,
		WorkflowID: target.workflowID,
	})

	// The sub-run's internal steps are not reported into the parent reporter
	// (nil → nop): the run_workflow step itself is reported by the parent, and
	// its output IS the sub-run's output — a sub-workflow reads as one black-box
	// building-block step. Threading the parent reporter down to prefix nested
	// step ids would require carrying it on every activity dispatch, invasive for
	// one activity's benefit.
	result, err := runner.Run(subCtx, target.root, target.errorSeq, subRC, nil)
	if err != nil {
		// Propagate the sub-run's failure/cancellation as this activity's error:
		// a parent Try can catch it (it composes with the error taxonomy — the
		// wrapped failError/thrownError/HTTP detail survive errors.Is/As), and a
		// retryable infra fault escalates to a whole-job retry, exactly as if the
		// failure had happened at this step directly.
		return nil, err
	}
	// Success: the activity output is the sub-run's Result.Output (its final
	// vars), exposed to the parent as steps.<id>.output.
	return result.Output, nil
}

// resolvedTarget is a resolved run_workflow target: the workflow id (the cycle
// key) plus the version definition to walk.
type resolvedTarget struct {
	workflowID uuid.UUID
	root       *workflowsv1.Sequence
	errorSeq   *workflowsv1.Sequence
}

// resolveTarget resolves the activity's `workflow` reference. A Workflow name
// resolves to the target's promoted live version; a WorkflowVersion name pins
// that exact version. Missing / no promoted version / cross-scope are terminal;
// a transient DB fault is [Retryable].
func (a *RunWorkflowActivity) resolveTarget(ctx context.Context, rc *RunContext, ref string) (resolvedTarget, error) {
	orgID, spaceID := rc.Scope()

	if wfSlug, versionNumber, isVersion := parseVersionRef(ref); isVersion {
		// Pinned version: resolve the parent workflow (scoped, by slug), then
		// load that version by the container's internal uuid + number.
		wf, err := a.scopedWorkflow(ctx, wfSlug, orgID, spaceID, ref)
		if err != nil {
			return resolvedTarget{}, err
		}
		ver, err := a.store.GetWorkflowVersionByNumber(ctx, db.GetWorkflowVersionByNumberParams{
			WorkflowID:    wf.ID,
			VersionNumber: versionNumber,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return resolvedTarget{}, fmt.Errorf("engine: run_workflow target %q not found", ref)
			}
			return resolvedTarget{}, Retryable(fmt.Errorf("engine: load run_workflow version %q: %w", ref, err))
		}
		return targetFromVersion(ref, wf.ID, ver)
	}

	wfSlug, err := parseWorkflowRef(ref)
	if err != nil {
		return resolvedTarget{}, fmt.Errorf("engine: run_workflow target %q is not a valid workflow resource name: %w", ref, err)
	}
	wf, err := a.scopedWorkflow(ctx, wfSlug, orgID, spaceID, ref)
	if err != nil {
		return resolvedTarget{}, err
	}
	if !wf.Version.Valid {
		return resolvedTarget{}, fmt.Errorf("engine: run_workflow target %q has no promoted version", ref)
	}
	ver, err := a.store.GetWorkflowVersion(ctx, wf.Version.Bytes)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return resolvedTarget{}, fmt.Errorf("engine: run_workflow target %q promoted version is unavailable", ref)
		}
		return resolvedTarget{}, Retryable(fmt.Errorf("engine: load run_workflow promoted version %q: %w", ref, err))
	}
	return targetFromVersion(ref, wf.ID, ver)
}

// scopedWorkflow resolves the workflow named by wfSlug within the run's scope
// (org + space). A missing or out-of-scope workflow is reported as not-found so
// a crafted cross-scope reference can't confirm the workflow exists (the scoped
// by-slug lookup is the guard); a transient DB fault is [Retryable].
//
// Same-scope only for day one — the sub-run executes under the PARENT run's
// scope (least-privilege sub-workflow identity is deferred with the
// KC-run-identity work), so a cross-scope target is refused.
func (a *RunWorkflowActivity) scopedWorkflow(ctx context.Context, wfSlug string, orgID, spaceID uuid.UUID, ref string) (db.Workflow, error) {
	wf, err := a.store.GetWorkflowByParent(ctx, db.GetWorkflowByParentParams{
		OrgID:   orgID,
		SpaceID: pgtype.UUID{Bytes: spaceID, Valid: spaceID != uuid.Nil},
		Slug:    wfSlug,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.Workflow{}, fmt.Errorf("engine: run_workflow target %q not found", ref)
		}
		return db.Workflow{}, Retryable(fmt.Errorf("engine: load run_workflow target %q: %w", ref, err))
	}
	return wf, nil
}

// targetFromVersion lifts the root and optional error_sequence out of a version's
// definition JSONB — symmetric with the worker's loadDefinition. A corrupt or
// rootless definition is terminal (it won't heal on retry).
func targetFromVersion(ref string, wfID uuid.UUID, ver db.WorkflowVersion) (resolvedTarget, error) {
	var scratch workflowsv1.WorkflowVersion
	if err := protojson.Unmarshal(ver.Definition, &scratch); err != nil {
		return resolvedTarget{}, fmt.Errorf("engine: run_workflow target %q has a corrupt definition: %w", ref, err)
	}
	root := scratch.GetRoot()
	if root == nil {
		return resolvedTarget{}, fmt.Errorf("engine: run_workflow target %q version has no root sequence", ref)
	}
	return resolvedTarget{workflowID: wfID, root: root, errorSeq: scratch.GetErrorSequence()}, nil
}

// evalParams evaluates each parameter expression against the PARENT run context
// and returns the value map that becomes the sub-run's params. A CEL error is
// terminal (a bad expression won't heal on retry). nil when there are no params.
func (a *RunWorkflowActivity) evalParams(ctx context.Context, rc *RunContext, exprs map[string]string) (map[string]any, error) {
	if len(exprs) == 0 {
		return nil, nil
	}
	out := make(map[string]any, len(exprs))
	for name, expr := range exprs {
		v, err := a.eval.EvalAny(ctx, expr, rc)
		if err != nil {
			return nil, fmt.Errorf("engine: run_workflow parameter %q: %w", name, err)
		}
		out[name] = v
	}
	return out, nil
}

// cycleError formats a terminal cycle error naming the full chain, e.g.
// "workflow cycle detected: A → B → A" where the trailing id is the target that
// closes the cycle.
func cycleError(stack []uuid.UUID, target uuid.UUID) error {
	chain := make([]string, 0, len(stack)+1)
	for _, id := range stack {
		chain = append(chain, id.String())
	}
	chain = append(chain, target.String())
	return fmt.Errorf("engine: workflow cycle detected: %s", strings.Join(chain, " → "))
}

// parseVersionRef splits a WorkflowVersion resource name
// (".../workflows/{wf-slug}/versions/{n}") into the parent workflow slug and the
// monotonic version number. ok is false when ref is not a version name (no
// "/versions/" segment, a non-numeric or non-positive number, or a workflow
// segment with no slug leaf) — the caller then treats it as a Workflow name.
func parseVersionRef(ref string) (workflowSlug string, versionNumber int64, ok bool) {
	const marker = "/versions/"
	idx := strings.LastIndex(ref, marker)
	if idx < 0 {
		return "", 0, false
	}
	num, err := strconv.ParseInt(ref[idx+len(marker):], 10, 64)
	if err != nil || num <= 0 {
		return "", 0, false
	}
	slug, err := parseWorkflowRef(ref[:idx])
	if err != nil {
		return "", 0, false
	}
	return slug, num, true
}

// parseWorkflowRef extracts the slug leaf from a Workflow resource name
// ("organizations/{org}[/spaces/{space}]/workflows/{slug}").
func parseWorkflowRef(ref string) (string, error) {
	idx := strings.LastIndex(ref, "/")
	if idx < 0 || idx == len(ref)-1 {
		return "", errors.New("missing workflow slug segment")
	}
	return ref[idx+1:], nil
}
