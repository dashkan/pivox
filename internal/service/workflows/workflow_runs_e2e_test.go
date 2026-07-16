package workflows_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	workflowsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/workflows/v1"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
)

// runIDFromName extracts the leaf run uuid from a WorkflowRun resource name
// (".../workflows/{wf}/runs/{run}").
func runIDFromName(t *testing.T, name string) uuid.UUID {
	t.Helper()
	idx := strings.LastIndex(name, "/runs/")
	require.GreaterOrEqual(t, idx, 0, "run name must contain /runs/: %s", name)
	id, err := uuid.Parse(name[idx+len("/runs/"):])
	require.NoError(t, err, "run leaf must be a uuid: %s", name)
	return id
}

// newPromotedWorkflow creates a workflow, mints a version, and promotes it,
// returning the workflow and version. The caller must already be authenticated
// as an owner of orgSlug.
func newPromotedWorkflow(
	t *testing.T,
	ctx context.Context,
	wfClient workflowsv1.WorkflowsClient,
	verClient workflowsv1.WorkflowVersionsClient,
	orgSlug, workflowID string,
) (*workflowsv1.Workflow, *workflowsv1.WorkflowVersion) {
	t.Helper()
	wf, err := wfClient.CreateWorkflow(ctx, &workflowsv1.CreateWorkflowRequest{
		Parent:     "organizations/" + orgSlug,
		WorkflowId: workflowID,
		Workflow:   &workflowsv1.Workflow{DisplayName: workflowID},
	})
	require.NoError(t, err)
	ver, err := verClient.CreateWorkflowVersion(ctx, &workflowsv1.CreateWorkflowVersionRequest{
		Parent:          wf.GetName(),
		WorkflowVersion: setStepDefinition("p", "v", `"x"`),
	})
	require.NoError(t, err)
	_, err = wfClient.PromoteWorkflowVersion(ctx, &workflowsv1.PromoteWorkflowVersionRequest{
		Name: wf.GetName(), Version: ver.GetName(),
	})
	require.NoError(t, err)
	return wf, ver
}

func runHarness(t *testing.T) *grpcharness.Harness {
	t.Helper()
	return grpcharness.New(t,
		grpcharness.WithOrganizationsServer(),
		grpcharness.WithWorkflowsServer(),
		grpcharness.WithWorkflowVersionsServer(),
		grpcharness.WithWorkflowRunsServer())
}

// TestE2E_WorkflowRun_Lifecycle covers the Phase-5 run lifecycle: RunWorkflow
// creates a PENDING run pinned to the promoted version with a MANUAL trigger,
// Get/List surface it, and Cancel drives it to CANCELLED (a second cancel is
// rejected as not-active). No execution happens — this is the API layer only.
func TestE2E_WorkflowRun_Lifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := runHarness(t)
	owned := h.SeedOwnedOrg(t, "wfr-life", "WFR Co", "workflows")
	ctx := context.Background()
	wfClient := workflowsv1.NewWorkflowsClient(h.Conn())
	verClient := workflowsv1.NewWorkflowVersionsClient(h.Conn())
	runClient := workflowsv1.NewWorkflowRunsClient(h.Conn())

	wf, ver := newPromotedWorkflow(t, ctx, wfClient, verClient, owned.Slug, "flow")

	run, err := runClient.RunWorkflow(ctx, &workflowsv1.RunWorkflowRequest{
		Name:    wf.GetName(),
		Subject: "assets/a1",
	})
	require.NoError(t, err)
	assert.Equal(t, workflowsv1.State_PENDING, run.GetState(), "a new run is PENDING (nothing executes in Phase 5)")
	assert.Equal(t, workflowsv1.RunTriggerKind_MANUAL, run.GetTrigger().GetKind(), "RunWorkflow fires a MANUAL trigger")
	assert.Equal(t, ver.GetName(), run.GetVersion(), "run pins the promoted version")
	assert.Equal(t, "assets/a1", run.GetSubject())
	assert.True(t, strings.HasPrefix(run.GetName(), wf.GetName()+"/runs/"), "run name nests under the workflow")

	// triggered_by is recorded in the DB (the harness wires no AuditResolver,
	// so it is not inflated into the proto Actor — assert the column instead).
	dbRun, err := h.Queries.GetWorkflowRun(ctx, runIDFromName(t, run.GetName()))
	require.NoError(t, err)
	assert.True(t, dbRun.TriggeredBy.Valid, "triggered_by must record the caller")
	assert.Equal(t, uuid.UUID(dbRun.TriggeredBy.Bytes), owned.Owner.IdentityID)

	got, err := runClient.GetWorkflowRun(ctx, &workflowsv1.GetWorkflowRunRequest{Name: run.GetName()})
	require.NoError(t, err)
	assert.Equal(t, run.GetName(), got.GetName())
	assert.Equal(t, workflowsv1.State_PENDING, got.GetState())

	listed, err := runClient.ListWorkflowRuns(ctx, &workflowsv1.ListWorkflowRunsRequest{Parent: wf.GetName()})
	require.NoError(t, err)
	require.Len(t, listed.GetWorkflowRuns(), 1)
	assert.Equal(t, run.GetName(), listed.GetWorkflowRuns()[0].GetName())

	cancelled, err := runClient.CancelWorkflowRun(ctx, &workflowsv1.CancelWorkflowRunRequest{Name: run.GetName()})
	require.NoError(t, err)
	assert.Equal(t, workflowsv1.State_CANCELLED, cancelled.GetState())
	assert.NotNil(t, cancelled.GetEndTime(), "cancel sets end_time")

	// A second cancel on a now-terminal run is rejected.
	_, err = runClient.CancelWorkflowRun(ctx, &workflowsv1.CancelWorkflowRunRequest{Name: run.GetName()})
	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err), "cancelling a terminal run must be FailedPrecondition")
}

// TestE2E_WorkflowRun_NoPromotedVersion pins that running a workflow with no
// promoted version (and no explicit version) is a FailedPrecondition.
func TestE2E_WorkflowRun_NoPromotedVersion(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := runHarness(t)
	owned := h.SeedOwnedOrg(t, "wfr-nover", "WFR NoVer", "workflows")
	ctx := context.Background()
	wfClient := workflowsv1.NewWorkflowsClient(h.Conn())
	runClient := workflowsv1.NewWorkflowRunsClient(h.Conn())

	wf, err := wfClient.CreateWorkflow(ctx, &workflowsv1.CreateWorkflowRequest{
		Parent: "organizations/" + owned.Slug, WorkflowId: "draft", Workflow: &workflowsv1.Workflow{},
	})
	require.NoError(t, err)

	_, err = runClient.RunWorkflow(ctx, &workflowsv1.RunWorkflowRequest{Name: wf.GetName()})
	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
}

// TestE2E_WorkflowRun_ExplicitVersion covers pinning a specific version: one
// that belongs to the workflow runs; one that belongs to a different workflow
// is a FailedPrecondition (a run can't pin a foreign workflow's version).
func TestE2E_WorkflowRun_ExplicitVersion(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := runHarness(t)
	owned := h.SeedOwnedOrg(t, "wfr-expv", "WFR ExpV", "workflows")
	ctx := context.Background()
	wfClient := workflowsv1.NewWorkflowsClient(h.Conn())
	verClient := workflowsv1.NewWorkflowVersionsClient(h.Conn())
	runClient := workflowsv1.NewWorkflowRunsClient(h.Conn())

	wfA, verA := newPromotedWorkflow(t, ctx, wfClient, verClient, owned.Slug, "a")
	_, verB := newPromotedWorkflow(t, ctx, wfClient, verClient, owned.Slug, "b")

	// Explicit version of the same workflow → ok, and the run pins it.
	run, err := runClient.RunWorkflow(ctx, &workflowsv1.RunWorkflowRequest{
		Name: wfA.GetName(), Version: verA.GetName(),
	})
	require.NoError(t, err)
	assert.Equal(t, verA.GetName(), run.GetVersion())

	// A version belonging to another workflow → FailedPrecondition.
	_, err = runClient.RunWorkflow(ctx, &workflowsv1.RunWorkflowRequest{
		Name: wfA.GetName(), Version: verB.GetName(),
	})
	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err),
		"pinning another workflow's version must be FailedPrecondition")
}

// TestE2E_WorkflowRun_ValidateOnly pins the AIP validate_only contract on
// RunWorkflow: the call succeeds (returning the would-be run) but nothing
// persists.
func TestE2E_WorkflowRun_ValidateOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := runHarness(t)
	owned := h.SeedOwnedOrg(t, "wfr-vo", "WFR VO", "workflows")
	ctx := context.Background()
	wfClient := workflowsv1.NewWorkflowsClient(h.Conn())
	verClient := workflowsv1.NewWorkflowVersionsClient(h.Conn())
	runClient := workflowsv1.NewWorkflowRunsClient(h.Conn())

	wf, _ := newPromotedWorkflow(t, ctx, wfClient, verClient, owned.Slug, "flow")

	dry, err := runClient.RunWorkflow(ctx, &workflowsv1.RunWorkflowRequest{
		Name: wf.GetName(), ValidateOnly: true,
	})
	require.NoError(t, err)
	assert.Equal(t, workflowsv1.State_PENDING, dry.GetState())

	listed, err := runClient.ListWorkflowRuns(ctx, &workflowsv1.ListWorkflowRunsRequest{Parent: wf.GetName()})
	require.NoError(t, err)
	assert.Empty(t, listed.GetWorkflowRuns(), "validate_only must not persist the run")
}

// TestE2E_WorkflowRun_ScopeIsolation pins that a run uuid can't be read or
// cancelled through a different org's workflow name prefix — cross-scope
// access is NotFound, not a leak.
func TestE2E_WorkflowRun_ScopeIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := runHarness(t)
	// Caller owns org A; then bootstraps org B (owning both).
	a := h.SeedOwnedOrg(t, "wfr-iso-a", "Iso A", "iso")
	ctx := context.Background()
	op, err := apiv1.NewOrganizationsClient(h.Conn()).CreateOrganization(ctx, &apiv1.CreateOrganizationRequest{
		OrganizationId: "wfr-iso-b",
		Organization:   &apiv1.Organization{DisplayName: "Iso B"},
	})
	require.NoError(t, err)
	require.True(t, op.GetDone())

	wfClient := workflowsv1.NewWorkflowsClient(h.Conn())
	verClient := workflowsv1.NewWorkflowVersionsClient(h.Conn())
	runClient := workflowsv1.NewWorkflowRunsClient(h.Conn())

	wfB, _ := newPromotedWorkflow(t, ctx, wfClient, verClient, "wfr-iso-b", "b-flow")
	runB, err := runClient.RunWorkflow(ctx, &workflowsv1.RunWorkflowRequest{Name: wfB.GetName()})
	require.NoError(t, err)

	// Reconstruct B's run name under A's prefix: same workflow slug + run uuid,
	// wrong org slug. A has no "b-flow" workflow, so the scoped parent lookup
	// finds nothing.
	runUUID := runIDFromName(t, runB.GetName())
	crossName := "organizations/" + a.Slug + "/workflows/b-flow/runs/" + runUUID.String()

	_, err = runClient.GetWorkflowRun(ctx, &workflowsv1.GetWorkflowRunRequest{Name: crossName})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err), "cross-scope read must be NotFound")

	_, err = runClient.CancelWorkflowRun(ctx, &workflowsv1.CancelWorkflowRunRequest{Name: crossName})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err), "cross-scope cancel must be NotFound")
}
