package engine

import (
	"context"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"

	db "github.com/dashkan/pivox/internal/db/generated"
	workflowsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/workflows/v1"
)

// --- run_workflow fake store ----------------------------------------------

// fakeWorkflowStore is an in-memory [WorkflowStore] for the run_workflow tests.
// It keeps the activity unit-testable in isolation — no real DB — while
// exercising the exact db.Querier signatures the production path uses.
type fakeWorkflowStore struct {
	workflows     map[uuid.UUID]db.Workflow
	versionsByID  map[uuid.UUID]db.WorkflowVersion
	versionsByNum map[string]db.WorkflowVersion // key: workflowID + "/" + number
}

var _ WorkflowStore = (*fakeWorkflowStore)(nil)

func newFakeWorkflowStore() *fakeWorkflowStore {
	return &fakeWorkflowStore{
		workflows:     map[uuid.UUID]db.Workflow{},
		versionsByID:  map[uuid.UUID]db.WorkflowVersion{},
		versionsByNum: map[string]db.WorkflowVersion{},
	}
}

// GetWorkflowByParent resolves a workflow by its (org, space, slug) — the
// scoped-by-slug lookup the production engine now uses. It scans registered
// workflows for an exact org + space + slug match (space compared NULL-aware,
// mirroring `space_id IS NOT DISTINCT FROM`), so a slug that exists only in
// another scope is not found.
func (s *fakeWorkflowStore) GetWorkflowByParent(_ context.Context, arg db.GetWorkflowByParentParams) (db.Workflow, error) {
	for _, wf := range s.workflows {
		if wf.OrgID == arg.OrgID && sameFakeSpace(wf.SpaceID, arg.SpaceID) && wf.Slug == arg.Slug {
			return wf, nil
		}
	}
	return db.Workflow{}, pgx.ErrNoRows
}

// sameFakeSpace compares two nullable spaces NULL-aware (both unset match; both
// set match on value), mirroring Postgres `IS NOT DISTINCT FROM`.
func sameFakeSpace(a, b pgtype.UUID) bool {
	if a.Valid != b.Valid {
		return false
	}
	return !a.Valid || a.Bytes == b.Bytes
}

// wfSlug is the deterministic slug the fake assigns a workflow: a short,
// non-uuid string derived from the id. Being non-uuid, it proves the engine
// resolves by the slug leaf (a uuid.Parse of it would fail).
func wfSlug(id uuid.UUID) string {
	return "wf-" + id.String()[:8]
}

func (s *fakeWorkflowStore) GetWorkflowVersion(_ context.Context, id uuid.UUID) (db.WorkflowVersion, error) {
	ver, ok := s.versionsByID[id]
	if !ok {
		return db.WorkflowVersion{}, pgx.ErrNoRows
	}
	return ver, nil
}

func (s *fakeWorkflowStore) GetWorkflowVersionByNumber(_ context.Context, arg db.GetWorkflowVersionByNumberParams) (db.WorkflowVersion, error) {
	ver, ok := s.versionsByNum[arg.WorkflowID.String()+"/"+strconv.FormatInt(arg.VersionNumber, 10)]
	if !ok {
		return db.WorkflowVersion{}, pgx.ErrNoRows
	}
	return ver, nil
}

// addWorkflow registers an org-scoped workflow with one version (number 1),
// promoted, whose definition is root. Returns the workflow id.
func (s *fakeWorkflowStore) addWorkflow(t *testing.T, orgID uuid.UUID, root *workflowsv1.Sequence) uuid.UUID {
	t.Helper()
	return s.addVersionedWorkflow(t, orgID, 1, true, root, nil)
}

// addVersionedWorkflow registers an org-scoped workflow carrying a single
// version with the given number and definition. promoted controls whether the
// workflow points at it as the live version.
func (s *fakeWorkflowStore) addVersionedWorkflow(
	t *testing.T,
	orgID uuid.UUID,
	number int64,
	promoted bool,
	root, errorSeq *workflowsv1.Sequence,
) uuid.UUID {
	t.Helper()
	wfID := uuid.New()
	verID := uuid.New()
	def, err := protojson.Marshal(&workflowsv1.WorkflowVersion{Root: root, ErrorSequence: errorSeq})
	require.NoError(t, err)
	ver := db.WorkflowVersion{ID: verID, WorkflowID: wfID, VersionNumber: number, Definition: def}
	s.versionsByID[verID] = ver
	s.versionsByNum[wfID.String()+"/"+strconv.FormatInt(number, 10)] = ver
	wf := db.Workflow{ID: wfID, OrgID: orgID, Slug: wfSlug(wfID)}
	if promoted {
		wf.Version = pgtype.UUID{Bytes: verID, Valid: true}
	}
	s.workflows[wfID] = wf
	return wfID
}

// workflowRef builds the run_workflow target name for a registered workflow —
// its slug leaf (not the uuid), matching how clients now name workflows.
func workflowRef(id uuid.UUID) string {
	return "organizations/o/workflows/" + wfSlug(id)
}

func versionRef(wfID uuid.UUID, number int64) string {
	return "organizations/o/workflows/" + wfSlug(wfID) + "/versions/" + strconv.FormatInt(number, 10)
}

// newRunWorkflowInterpreter builds an interpreter wired with set + run_workflow.
func newRunWorkflowInterpreter(t *testing.T, store WorkflowStore) *Interpreter {
	t.Helper()
	eval, err := NewEvaluator()
	require.NoError(t, err)
	disp := NewDispatcher(DispatcherConfig{
		Set:         NewSetActivity(SetActivityConfig{Evaluator: eval}),
		RunWorkflow: NewRunWorkflowActivity(RunWorkflowActivityConfig{Evaluator: eval, Store: store}),
	})
	return NewInterpreter(InterpreterConfig{Evaluator: eval, Dispatcher: disp})
}

// --- tests ----------------------------------------------------------------

func TestRunWorkflow_HappyPath_PassesParamsAndIsolatesParentState(t *testing.T) {
	t.Parallel()

	org := uuid.New()
	store := newFakeWorkflowStore()

	// The sub-workflow reads ONLY its params and proves it cannot see parent
	// state: `"n" in vars` is false because the sub starts with empty vars.
	subRoot := seq(setStep("subset", map[string]string{
		"out":  "params.v",
		"also": `"n" in vars`,
	}))
	targetID := store.addWorkflow(t, org, subRoot)

	it := newRunWorkflowInterpreter(t, store)
	parentWfID := uuid.New()
	rc := NewRunContext(RunContextConfig{OrgID: org, WorkflowID: parentWfID})

	// params.v is evaluated on the PARENT env, so it sees the parent's vars.n.
	root := seq(
		setStep("seed", map[string]string{"n": "5"}),
		runWorkflowStep("callSub", workflowRef(targetID), map[string]string{"v": "vars.n * 2"}),
		setStep("read", map[string]string{"final": "steps.callSub.output.out"}),
	)

	res, err := it.Run(context.Background(), root, nil, rc, nil)
	require.NoError(t, err)
	require.Equal(t, RunStatusCompleted, res.Status)

	// The parent read the sub-run output through steps.<id>.output.
	assert.Equal(t, int64(10), res.Output["final"])

	// The run_workflow step's output IS the sub-run's Result.Output (final vars):
	// out from the passed param, also=false proving isolation from parent vars.
	var callStep StepState
	for _, s := range res.Steps {
		if s.ID == "callSub" {
			callStep = s
		}
	}
	assert.Equal(t, map[string]any{"out": int64(10), "also": false}, callStep.Output)
}

func TestRunWorkflow_DirectCycle_SelfCallIsTerminal(t *testing.T) {
	t.Parallel()

	org := uuid.New()
	store := newFakeWorkflowStore()

	// Register a workflow whose root calls itself. We know its id up front so the
	// run executes under that same workflow id (seeding the cycle guard).
	wfID := uuid.New()
	verID := uuid.New()
	def, err := protojson.Marshal(&workflowsv1.WorkflowVersion{
		Root: seq(runWorkflowStep("recur", "", nil)),
	})
	require.NoError(t, err)
	ver := db.WorkflowVersion{ID: verID, WorkflowID: wfID, VersionNumber: 1, Definition: def}
	store.versionsByID[verID] = ver
	store.versionsByNum[wfID.String()+"/1"] = ver
	store.workflows[wfID] = db.Workflow{ID: wfID, OrgID: org, Slug: wfSlug(wfID), Version: pgtype.UUID{Bytes: verID, Valid: true}}

	// Patch the self-reference now that we have the id.
	root := seq(runWorkflowStep("recur", workflowRef(wfID), nil))

	it := newRunWorkflowInterpreter(t, store)
	rc := NewRunContext(RunContextConfig{OrgID: org, WorkflowID: wfID})

	res, err := it.Run(context.Background(), root, nil, rc, nil)
	require.Error(t, err)
	assert.Equal(t, RunStatusFailed, res.Status)
	assert.False(t, IsRetryable(err))
	assert.Contains(t, err.Error(), "workflow cycle detected")
	assert.Contains(t, err.Error(), wfID.String()+" → "+wfID.String())
}

func TestRunWorkflow_IndirectCycle_ABAIsTerminal(t *testing.T) {
	t.Parallel()

	org := uuid.New()
	store := newFakeWorkflowStore()

	// A → B → A. Register both with known ids so each can reference the other.
	aID, bID := uuid.New(), uuid.New()
	register := func(id, callID uuid.UUID) {
		verID := uuid.New()
		def, err := protojson.Marshal(&workflowsv1.WorkflowVersion{
			Root: seq(runWorkflowStep("call", workflowRef(callID), nil)),
		})
		require.NoError(t, err)
		ver := db.WorkflowVersion{ID: verID, WorkflowID: id, VersionNumber: 1, Definition: def}
		store.versionsByID[verID] = ver
		store.versionsByNum[id.String()+"/1"] = ver
		store.workflows[id] = db.Workflow{ID: id, OrgID: org, Slug: wfSlug(id), Version: pgtype.UUID{Bytes: verID, Valid: true}}
	}
	register(aID, bID)
	register(bID, aID)

	it := newRunWorkflowInterpreter(t, store)
	rc := NewRunContext(RunContextConfig{OrgID: org, WorkflowID: aID})

	root := seq(runWorkflowStep("call", workflowRef(bID), nil))
	res, err := it.Run(context.Background(), root, nil, rc, nil)
	require.Error(t, err)
	assert.Equal(t, RunStatusFailed, res.Status)
	assert.False(t, IsRetryable(err))
	// The reported chain names the full cycle: A → B → A.
	assert.Contains(t, err.Error(), aID.String()+" → "+bID.String()+" → "+aID.String())
}

func TestRunWorkflow_DepthCapExceeded_IsTerminal(t *testing.T) {
	t.Parallel()

	org := uuid.New()
	store := newFakeWorkflowStore()

	// Build a linear, acyclic chain w0 → w1 → ... deeper than the cap so the
	// cycle guard never fires — only the depth cap does.
	const depth = maxSubWorkflowDepth + 4
	ids := make([]uuid.UUID, depth+1)
	verIDs := make([]uuid.UUID, depth+1)
	for i := range ids {
		ids[i] = uuid.New()
		verIDs[i] = uuid.New()
	}
	for i := 0; i <= depth; i++ {
		var root *workflowsv1.Sequence
		if i < depth {
			root = seq(runWorkflowStep("next", workflowRef(ids[i+1]), nil))
		} else {
			root = seq(setStep("leaf", map[string]string{"done": "true"}))
		}
		def, err := protojson.Marshal(&workflowsv1.WorkflowVersion{Root: root})
		require.NoError(t, err)
		ver := db.WorkflowVersion{ID: verIDs[i], WorkflowID: ids[i], VersionNumber: 1, Definition: def}
		store.versionsByID[verIDs[i]] = ver
		store.versionsByNum[ids[i].String()+"/1"] = ver
		store.workflows[ids[i]] = db.Workflow{ID: ids[i], OrgID: org, Slug: wfSlug(ids[i]), Version: pgtype.UUID{Bytes: verIDs[i], Valid: true}}
	}

	it := newRunWorkflowInterpreter(t, store)
	rc := NewRunContext(RunContextConfig{OrgID: org, WorkflowID: ids[0]})

	root := seq(runWorkflowStep("next", workflowRef(ids[1]), nil))
	res, err := it.Run(context.Background(), root, nil, rc, nil)
	require.Error(t, err)
	assert.Equal(t, RunStatusFailed, res.Status)
	assert.False(t, IsRetryable(err))
	assert.Contains(t, err.Error(), "depth cap")
}

func TestRunWorkflow_UnknownWorkflow_IsTerminal(t *testing.T) {
	t.Parallel()

	org := uuid.New()
	store := newFakeWorkflowStore()
	it := newRunWorkflowInterpreter(t, store)
	rc := NewRunContext(RunContextConfig{OrgID: org, WorkflowID: uuid.New()})

	root := seq(runWorkflowStep("call", workflowRef(uuid.New()), nil))
	res, err := it.Run(context.Background(), root, nil, rc, nil)
	require.Error(t, err)
	assert.Equal(t, RunStatusFailed, res.Status)
	assert.False(t, IsRetryable(err))
	assert.Contains(t, err.Error(), "not found")
}

func TestRunWorkflow_NoPromotedVersion_IsTerminal(t *testing.T) {
	t.Parallel()

	org := uuid.New()
	store := newFakeWorkflowStore()
	// A workflow with a version present but NOT promoted.
	targetID := store.addVersionedWorkflow(t, org, 1, false, seq(setStep("x", map[string]string{"a": "1"})), nil)

	it := newRunWorkflowInterpreter(t, store)
	rc := NewRunContext(RunContextConfig{OrgID: org, WorkflowID: uuid.New()})

	root := seq(runWorkflowStep("call", workflowRef(targetID), nil))
	res, err := it.Run(context.Background(), root, nil, rc, nil)
	require.Error(t, err)
	assert.Equal(t, RunStatusFailed, res.Status)
	assert.False(t, IsRetryable(err))
	assert.Contains(t, err.Error(), "no promoted version")
}

func TestRunWorkflow_CrossScope_IsTerminalNotFound(t *testing.T) {
	t.Parallel()

	store := newFakeWorkflowStore()
	// Target lives in a DIFFERENT org than the run's scope.
	targetOrg := uuid.New()
	targetID := store.addWorkflow(t, targetOrg, seq(setStep("x", map[string]string{"a": "1"})))

	it := newRunWorkflowInterpreter(t, store)
	runOrg := uuid.New()
	rc := NewRunContext(RunContextConfig{OrgID: runOrg, WorkflowID: uuid.New()})

	root := seq(runWorkflowStep("call", workflowRef(targetID), nil))
	res, err := it.Run(context.Background(), root, nil, rc, nil)
	require.Error(t, err)
	assert.Equal(t, RunStatusFailed, res.Status)
	assert.False(t, IsRetryable(err))
	assert.Contains(t, err.Error(), "not found")
}

func TestRunWorkflow_ExplicitVersionPin_RunsThatVersion(t *testing.T) {
	t.Parallel()

	org := uuid.New()
	store := newFakeWorkflowStore()

	// One workflow with two versions: version 1 is promoted (live), version 2 is
	// a distinct draft. Pinning version 2 must run version 2, not the live one.
	wfID := uuid.New()
	ver1ID, ver2ID := uuid.New(), uuid.New()
	def1, err := protojson.Marshal(&workflowsv1.WorkflowVersion{
		Root: seq(setStep("v", map[string]string{"which": `"v1"`})),
	})
	require.NoError(t, err)
	def2, err := protojson.Marshal(&workflowsv1.WorkflowVersion{
		Root: seq(setStep("v", map[string]string{"which": `"v2"`})),
	})
	require.NoError(t, err)
	v1 := db.WorkflowVersion{ID: ver1ID, WorkflowID: wfID, VersionNumber: 1, Definition: def1}
	v2 := db.WorkflowVersion{ID: ver2ID, WorkflowID: wfID, VersionNumber: 2, Definition: def2}
	store.versionsByID[ver1ID] = v1
	store.versionsByID[ver2ID] = v2
	store.versionsByNum[wfID.String()+"/1"] = v1
	store.versionsByNum[wfID.String()+"/2"] = v2
	store.workflows[wfID] = db.Workflow{ID: wfID, OrgID: org, Slug: wfSlug(wfID), Version: pgtype.UUID{Bytes: ver1ID, Valid: true}}

	it := newRunWorkflowInterpreter(t, store)
	rc := NewRunContext(RunContextConfig{OrgID: org, WorkflowID: uuid.New()})

	root := seq(
		runWorkflowStep("call", versionRef(wfID, 2), nil),
		setStep("read", map[string]string{"which": "steps.call.output.which"}),
	)
	res, err := it.Run(context.Background(), root, nil, rc, nil)
	require.NoError(t, err)
	assert.Equal(t, "v2", res.Output["which"])
}

func TestRunWorkflow_SubRunFailure_IsCatchableByParentTry(t *testing.T) {
	t.Parallel()

	org := uuid.New()
	store := newFakeWorkflowStore()
	// The sub-workflow fails via a `fail` activity.
	targetID := store.addWorkflow(t, org, seq(failStep("boom", "sub blew up")))

	it := newRunWorkflowInterpreter(t, store)
	rc := NewRunContext(RunContextConfig{OrgID: org, WorkflowID: uuid.New()})

	// A parent Try wraps the run_workflow call; the catch records the failure.
	root := seq(tryStep(
		"guard",
		seq(runWorkflowStep("callSub", workflowRef(targetID), nil)),
		seq(setStep("handle", map[string]string{
			"caughtMessage": "error.message",
			"caughtStep":    "error.step",
			"caughtCode":    "error.code",
		})),
		false,
	))

	res, err := it.Run(context.Background(), root, nil, rc, nil)
	require.NoError(t, err) // caught → the run completes
	assert.Equal(t, RunStatusCompleted, res.Status)
	assert.Equal(t, "sub blew up", res.Output["caughtMessage"])
	// The failure surfaces at the run_workflow step from the parent's view.
	assert.Equal(t, "callSub", res.Output["caughtStep"])
	assert.Equal(t, errorCodeFail, res.Output["caughtCode"])
}
