package workflows_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/permission"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	workflowsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/workflows/v1"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
)

// scopeRunHarness wires the org, space, workflow, version, and run services so a
// single test can create org- and space-scoped workflows and their runs.
func scopeRunHarness(t *testing.T) *grpcharness.Harness {
	t.Helper()
	return grpcharness.New(t,
		grpcharness.WithOrganizationsServer(),
		grpcharness.WithSpacesServer(),
		grpcharness.WithWorkflowsServer(),
		grpcharness.WithWorkflowVersionsServer(),
		grpcharness.WithWorkflowRunsServer())
}

// newPromotedWorkflowUnder creates a workflow under an arbitrary parent
// ("organizations/{org}" or "organizations/{org}/spaces/{space}"), mints and
// promotes a version, and returns the workflow. The caller must already be
// authenticated with workflows.create at that scope.
func newPromotedWorkflowUnder(
	t *testing.T,
	ctx context.Context,
	wfClient workflowsv1.WorkflowsClient,
	verClient workflowsv1.WorkflowVersionsClient,
	parent, workflowID string,
) *workflowsv1.Workflow {
	t.Helper()
	wf, err := wfClient.CreateWorkflow(ctx, &workflowsv1.CreateWorkflowRequest{
		Parent:     parent,
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
	return wf
}

// runNames collects the resource names of a run page into a set for
// order-independent membership assertions.
func runNames(runs []*workflowsv1.WorkflowRun) map[string]struct{} {
	set := make(map[string]struct{}, len(runs))
	for _, r := range runs {
		set[r.GetName()] = struct{}{}
	}
	return set
}

// TestE2E_ListWorkflowRuns_OrgWildcard covers the AIP-159 `-` wildcard at org
// scope: organizations/{org}/workflows/- lists runs across every org-scoped
// workflow in the org, not just one workflow.
func TestE2E_ListWorkflowRuns_OrgWildcard(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := scopeRunHarness(t)
	owned := h.SeedOwnedOrg(t, "wfr-orgw", "OrgW", "workflows")
	ctx := context.Background()
	wfClient := workflowsv1.NewWorkflowsClient(h.Conn())
	verClient := workflowsv1.NewWorkflowVersionsClient(h.Conn())
	runClient := workflowsv1.NewWorkflowRunsClient(h.Conn())

	orgParent := "organizations/" + owned.Slug
	wfA := newPromotedWorkflowUnder(t, ctx, wfClient, verClient, orgParent, "a")
	wfB := newPromotedWorkflowUnder(t, ctx, wfClient, verClient, orgParent, "b")

	runA, err := runClient.RunWorkflow(ctx, &workflowsv1.RunWorkflowRequest{Name: wfA.GetName()})
	require.NoError(t, err)
	runB, err := runClient.RunWorkflow(ctx, &workflowsv1.RunWorkflowRequest{Name: wfB.GetName()})
	require.NoError(t, err)

	// Wildcard: runs across BOTH workflows in the org.
	listed, err := runClient.ListWorkflowRuns(ctx, &workflowsv1.ListWorkflowRunsRequest{
		Parent: orgParent + "/workflows/-",
	})
	require.NoError(t, err)
	got := runNames(listed.GetWorkflowRuns())
	assert.Len(t, got, 2, "org wildcard lists runs across all org workflows")
	assert.Contains(t, got, runA.GetName())
	assert.Contains(t, got, runB.GetName())

	// The per-workflow path is unchanged: only that workflow's run.
	perWf, err := runClient.ListWorkflowRuns(ctx, &workflowsv1.ListWorkflowRunsRequest{
		Parent: wfA.GetName(),
	})
	require.NoError(t, err)
	require.Len(t, perWf.GetWorkflowRuns(), 1)
	assert.Equal(t, runA.GetName(), perWf.GetWorkflowRuns()[0].GetName())
}

// TestE2E_ListWorkflowRuns_SpaceWildcard covers the `-` wildcard at both scopes
// under the ROLLUP model: the space wildcard returns only that space's runs,
// while the org wildcard returns ALL runs in the org — org-direct runs AND
// space-scoped runs — each rendered with its actual (space-scoped or org-direct)
// resource name.
func TestE2E_ListWorkflowRuns_SpaceWildcard(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := scopeRunHarness(t)
	owned := h.SeedOwnedOrg(t, "wfr-spw", "SpaceW", "workflows")
	space := h.SeedOwnedSpace(t, owned.Slug, "team", "Team")
	ctx := context.Background()
	wfClient := workflowsv1.NewWorkflowsClient(h.Conn())
	verClient := workflowsv1.NewWorkflowVersionsClient(h.Conn())
	runClient := workflowsv1.NewWorkflowRunsClient(h.Conn())

	orgParent := "organizations/" + owned.Slug
	spaceParent := orgParent + "/spaces/" + space.Slug

	wfOrg := newPromotedWorkflowUnder(t, ctx, wfClient, verClient, orgParent, "org-flow")
	wfSpace := newPromotedWorkflowUnder(t, ctx, wfClient, verClient, spaceParent, "space-flow")

	runOrg, err := runClient.RunWorkflow(ctx, &workflowsv1.RunWorkflowRequest{Name: wfOrg.GetName()})
	require.NoError(t, err)
	runSpace, err := runClient.RunWorkflow(ctx, &workflowsv1.RunWorkflowRequest{Name: wfSpace.GetName()})
	require.NoError(t, err)

	// Space wildcard: only the space's run.
	spaceList, err := runClient.ListWorkflowRuns(ctx, &workflowsv1.ListWorkflowRunsRequest{
		Parent: spaceParent + "/workflows/-",
	})
	require.NoError(t, err)
	require.Len(t, spaceList.GetWorkflowRuns(), 1)
	assert.Equal(t, runSpace.GetName(), spaceList.GetWorkflowRuns()[0].GetName(),
		"space wildcard lists the space's run with its space-scoped resource name")

	// Org wildcard (rollup): BOTH the org-direct run and the space run, each with
	// its actual resource name — the space run keeps its space-scoped name.
	orgList, err := runClient.ListWorkflowRuns(ctx, &workflowsv1.ListWorkflowRunsRequest{
		Parent: orgParent + "/workflows/-",
	})
	require.NoError(t, err)
	got := runNames(orgList.GetWorkflowRuns())
	assert.Len(t, got, 2, "org wildcard rolls up org-direct and space runs")
	assert.Contains(t, got, runOrg.GetName(), "org wildcard includes the org-direct run")
	assert.Contains(t, got, runSpace.GetName(),
		"org wildcard includes the space run with its space-scoped name")
	// runSpace.GetName() was minted under the space parent, so asserting it is
	// present proves the org listing rebuilt the space-scoped name (with the
	// space slug), not a flat org-direct name.
	require.Contains(t, runSpace.GetName(), "/spaces/"+space.Slug+"/",
		"precondition: the space run's name is space-scoped")
	// BE-1: the workflow segment of each rolled-up run name is the workflow's
	// slug (not its uuid), reconstructed from the run's workflow_id in the org
	// wildcard's batched slug lookup.
	require.Contains(t, runOrg.GetName(), "/workflows/org-flow/runs/",
		"the org-direct run name carries the workflow slug")
	require.Contains(t, runSpace.GetName(), "/workflows/space-flow/runs/",
		"the space run name carries the workflow slug")
}

// TestE2E_ListWorkflowRuns_WildcardStateFilter covers the AIP-160 state filter
// across the wildcard: only runs in the requested state are returned.
func TestE2E_ListWorkflowRuns_WildcardStateFilter(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := scopeRunHarness(t)
	owned := h.SeedOwnedOrg(t, "wfr-filt", "Filt", "workflows")
	ctx := context.Background()
	wfClient := workflowsv1.NewWorkflowsClient(h.Conn())
	verClient := workflowsv1.NewWorkflowVersionsClient(h.Conn())
	runClient := workflowsv1.NewWorkflowRunsClient(h.Conn())

	orgParent := "organizations/" + owned.Slug
	wfA := newPromotedWorkflowUnder(t, ctx, wfClient, verClient, orgParent, "a")
	wfB := newPromotedWorkflowUnder(t, ctx, wfClient, verClient, orgParent, "b")

	pendingRun, err := runClient.RunWorkflow(ctx, &workflowsv1.RunWorkflowRequest{Name: wfA.GetName()})
	require.NoError(t, err)
	runningRun, err := runClient.RunWorkflow(ctx, &workflowsv1.RunWorkflowRequest{Name: wfB.GetName()})
	require.NoError(t, err)

	// Drive one run to RUNNING directly (no executor in the API-layer test).
	_, err = h.Queries.UpdateWorkflowRunState(ctx, db.UpdateWorkflowRunStateParams{
		ID:    runIDFromName(t, runningRun.GetName()),
		State: workflowsv1.State_RUNNING.String(),
	})
	require.NoError(t, err)

	listed, err := runClient.ListWorkflowRuns(ctx, &workflowsv1.ListWorkflowRunsRequest{
		Parent: orgParent + "/workflows/-",
		Filter: `state = "RUNNING"`,
	})
	require.NoError(t, err)
	require.Len(t, listed.GetWorkflowRuns(), 1, "state filter narrows to RUNNING runs")
	assert.Equal(t, runningRun.GetName(), listed.GetWorkflowRuns()[0].GetName())
	assert.Equal(t, workflowsv1.State_RUNNING, listed.GetWorkflowRuns()[0].GetState())

	// Sanity: without the filter, both runs are listed.
	all, err := runClient.ListWorkflowRuns(ctx, &workflowsv1.ListWorkflowRunsRequest{
		Parent: orgParent + "/workflows/-",
	})
	require.NoError(t, err)
	got := runNames(all.GetWorkflowRuns())
	assert.Contains(t, got, pendingRun.GetName())
	assert.Contains(t, got, runningRun.GetName())

	// An unknown state value now yields an EMPTY page (standard AIP-160): the
	// generic transpiler binds `state = 'BOGUS'`, which matches no rows. The
	// pre-migration bespoke parser rejected it with InvalidArgument; that
	// enum-domain validation was dropped in the move to the shared engine.
	unknown, err := runClient.ListWorkflowRuns(ctx, &workflowsv1.ListWorkflowRunsRequest{
		Parent: orgParent + "/workflows/-",
		Filter: `state = "BOGUS"`,
	})
	require.NoError(t, err)
	assert.Empty(t, unknown.GetWorkflowRuns(), "an unknown state value matches no runs")

	// A structurally invalid filter (unknown field) is still InvalidArgument —
	// the transpiler rejects fields outside the whitelist.
	_, err = runClient.ListWorkflowRuns(ctx, &workflowsv1.ListWorkflowRunsRequest{
		Parent: orgParent + "/workflows/-",
		Filter: `bogusfield = "x"`,
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err), "an unknown filter field is rejected")
}

// TestE2E_ListWorkflowRuns_WildcardPagination covers keyset pagination across
// the wildcard: a page-size-limited call returns a token that fetches the rest,
// with no overlap and full coverage.
func TestE2E_ListWorkflowRuns_WildcardPagination(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := scopeRunHarness(t)
	owned := h.SeedOwnedOrg(t, "wfr-page", "Page", "workflows")
	ctx := context.Background()
	wfClient := workflowsv1.NewWorkflowsClient(h.Conn())
	verClient := workflowsv1.NewWorkflowVersionsClient(h.Conn())
	runClient := workflowsv1.NewWorkflowRunsClient(h.Conn())

	orgParent := "organizations/" + owned.Slug
	wf := newPromotedWorkflowUnder(t, ctx, wfClient, verClient, orgParent, "a")

	const total = 3
	want := make(map[string]struct{}, total)
	for range total {
		run, err := runClient.RunWorkflow(ctx, &workflowsv1.RunWorkflowRequest{Name: wf.GetName()})
		require.NoError(t, err)
		want[run.GetName()] = struct{}{}
	}

	got := make(map[string]struct{}, total)
	var pageToken string
	pages := 0
	for {
		page, err := runClient.ListWorkflowRuns(ctx, &workflowsv1.ListWorkflowRunsRequest{
			Parent:    orgParent + "/workflows/-",
			PageSize:  2,
			PageToken: pageToken,
		})
		require.NoError(t, err)
		assert.LessOrEqual(t, len(page.GetWorkflowRuns()), 2, "page must respect page_size")
		for _, r := range page.GetWorkflowRuns() {
			_, dup := got[r.GetName()]
			assert.False(t, dup, "no run appears on two pages")
			got[r.GetName()] = struct{}{}
		}
		pages++
		require.LessOrEqual(t, pages, total+1, "pagination must terminate")
		pageToken = page.GetNextPageToken()
		if pageToken == "" {
			break
		}
	}
	assert.Equal(t, want, got, "pagination covers every run exactly once")
}

// TestE2E_ListWorkflowRuns_WildcardPermissionDenied covers permission scoping on
// the wildcard: a member who lacks workflows.read at the org scope (a viewer) is
// denied — the check moves to the org/space scope, not a specific workflow.
func TestE2E_ListWorkflowRuns_WildcardPermissionDenied(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := scopeRunHarness(t)
	owned := h.SeedOwnedOrg(t, "wfr-deny", "Deny", "workflows")
	ctx := context.Background()

	// A viewer is an org member but lacks workflows.read (owner/admin/editor
	// only). Seed the viewer, then switch the harness caller to them.
	viewer := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "viewer-wfr-deny"})
	h.SeedMembership(t, owned.Row.ID, viewer, permission.RoleViewer)
	h.SetCaller(viewer)

	runClient := workflowsv1.NewWorkflowRunsClient(h.Conn())
	_, err := runClient.ListWorkflowRuns(ctx, &workflowsv1.ListWorkflowRunsRequest{
		Parent: "organizations/" + owned.Slug + "/workflows/-",
	})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err),
		"a viewer lacking workflows.read at org scope is denied the wildcard listing")
}

// TestE2E_ListWorkflowRuns_OrgWildcardCrossOrgIsolation proves the rollup does
// not cross the org boundary: org A's workflows/- lists only org A's runs, never
// org B's — even when the same caller owns both orgs.
func TestE2E_ListWorkflowRuns_OrgWildcardCrossOrgIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := scopeRunHarness(t)
	a := h.SeedOwnedOrg(t, "wfr-xorg-a", "XOrg A", "workflows")
	ctx := context.Background()

	// The caller bootstraps org B too (owns both) — the strongest isolation case:
	// even a caller entitled in BOTH orgs must not see B's runs under A's parent.
	op, err := apiv1.NewOrganizationsClient(h.Conn()).CreateOrganization(ctx, &apiv1.CreateOrganizationRequest{
		OrganizationId: "wfr-xorg-b",
		Organization:   &apiv1.Organization{DisplayName: "XOrg B"},
	})
	require.NoError(t, err)
	require.True(t, op.GetDone())

	wfClient := workflowsv1.NewWorkflowsClient(h.Conn())
	verClient := workflowsv1.NewWorkflowVersionsClient(h.Conn())
	runClient := workflowsv1.NewWorkflowRunsClient(h.Conn())

	wfA := newPromotedWorkflowUnder(t, ctx, wfClient, verClient, "organizations/"+a.Slug, "a-flow")
	wfB := newPromotedWorkflowUnder(t, ctx, wfClient, verClient, "organizations/wfr-xorg-b", "b-flow")

	runA, err := runClient.RunWorkflow(ctx, &workflowsv1.RunWorkflowRequest{Name: wfA.GetName()})
	require.NoError(t, err)
	runB, err := runClient.RunWorkflow(ctx, &workflowsv1.RunWorkflowRequest{Name: wfB.GetName()})
	require.NoError(t, err)

	listA, err := runClient.ListWorkflowRuns(ctx, &workflowsv1.ListWorkflowRunsRequest{
		Parent: "organizations/" + a.Slug + "/workflows/-",
	})
	require.NoError(t, err)
	got := runNames(listA.GetWorkflowRuns())
	assert.Len(t, got, 1, "org A wildcard sees only org A's runs")
	assert.Contains(t, got, runA.GetName())
	assert.NotContains(t, got, runB.GetName(), "org A wildcard must NOT roll up org B's runs")
}

// TestE2E_ListWorkflowRuns_SpaceScopedMemberIsolation proves the rollup's
// permission boundary: an identity entitled at a SPACE (via space_members) but
// only a low-privilege org viewer at ORG scope may list that space's runs (the
// space wildcard) yet is DENIED the org-wide rollup (the org wildcard).
//
// NOTE ON THE MODEL: a *literal* space-only member (a space_members binding with
// NO org_members row) cannot be tested here — MembershipRequiredInterceptor
// gates every non-bootstrap RPC on ORG membership (ListOrganizationsForIdentity
// counts only org_members), so such a caller is treated as memberless and is
// denied even the space wildcard. The realistic least-privilege shape that both
// clears the membership gate AND proves scope isolation is org-viewer +
// space-editor, used below.
func TestE2E_ListWorkflowRuns_SpaceScopedMemberIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := scopeRunHarness(t)
	owned := h.SeedOwnedOrg(t, "wfr-spmem", "SpMem", "workflows")
	space := h.SeedOwnedSpace(t, owned.Slug, "team", "Team")
	ctx := context.Background()
	wfClient := workflowsv1.NewWorkflowsClient(h.Conn())
	verClient := workflowsv1.NewWorkflowVersionsClient(h.Conn())
	runClient := workflowsv1.NewWorkflowRunsClient(h.Conn())

	orgParent := "organizations/" + owned.Slug
	spaceParent := orgParent + "/spaces/" + space.Slug

	// Owner creates an org-direct run and a space-scoped run.
	wfOrg := newPromotedWorkflowUnder(t, ctx, wfClient, verClient, orgParent, "org-flow")
	wfSpace := newPromotedWorkflowUnder(t, ctx, wfClient, verClient, spaceParent, "space-flow")
	_, err := runClient.RunWorkflow(ctx, &workflowsv1.RunWorkflowRequest{Name: wfOrg.GetName()})
	require.NoError(t, err)
	runSpace, err := runClient.RunWorkflow(ctx, &workflowsv1.RunWorkflowRequest{Name: wfSpace.GetName()})
	require.NoError(t, err)

	// A member: org viewer (clears the membership gate; no workflows.read at org)
	// PLUS space editor (workflows.read at the space only).
	member := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "spacemember-wfr-spmem"})
	h.SeedMembership(t, owned.Row.ID, member, permission.RoleViewer)
	h.SeedSpaceMembership(t, owned.Row.ID, space.Row.ID, member, permission.RoleEditor)
	h.SetCaller(member)

	// Space wildcard: ALLOWED — the member reads the space's runs.
	spaceList, err := runClient.ListWorkflowRuns(ctx, &workflowsv1.ListWorkflowRunsRequest{
		Parent: spaceParent + "/workflows/-",
	})
	require.NoError(t, err)
	got := runNames(spaceList.GetWorkflowRuns())
	assert.Contains(t, got, runSpace.GetName(), "space-editor member reads the space's runs")

	// Org wildcard: DENIED — the member lacks workflows.read at org scope, so it
	// cannot use the rollup to reach org-wide runs.
	_, err = runClient.ListWorkflowRuns(ctx, &workflowsv1.ListWorkflowRunsRequest{
		Parent: orgParent + "/workflows/-",
	})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err),
		"a member entitled only at space scope is denied the org-wide rollup")
}
