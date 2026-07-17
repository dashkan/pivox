package workflows_test

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	workflowsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/workflows/v1"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
)

// drainWorkflowNames follows next_page_token to completion, returning every
// workflow's resource name across all pages. Fails if the page loop runs away.
func drainWorkflowNames(t *testing.T, ctx context.Context, client workflowsv1.WorkflowsClient, req *workflowsv1.ListWorkflowsRequest) []string {
	t.Helper()
	var names []string
	token := ""
	for range 100 {
		req.PageToken = token
		resp, err := client.ListWorkflows(ctx, req)
		require.NoError(t, err)
		for _, w := range resp.GetWorkflows() {
			names = append(names, w.GetName())
		}
		token = resp.GetNextPageToken()
		if token == "" {
			return names
		}
	}
	t.Fatal("pagination did not terminate within 100 pages")
	return nil
}

// drainWorkflowVersionNames follows next_page_token to completion, returning
// every version's resource name across all pages. Fails if the loop runs away.
func drainWorkflowVersionNames(t *testing.T, ctx context.Context, client workflowsv1.WorkflowVersionsClient, req *workflowsv1.ListWorkflowVersionsRequest) []string {
	t.Helper()
	var names []string
	token := ""
	for range 100 {
		req.PageToken = token
		resp, err := client.ListWorkflowVersions(ctx, req)
		require.NoError(t, err)
		for _, v := range resp.GetWorkflowVersions() {
			names = append(names, v.GetName())
		}
		token = resp.GetNextPageToken()
		if token == "" {
			return names
		}
	}
	t.Fatal("pagination did not terminate within 100 pages")
	return nil
}

// TestE2E_ListWorkflows_KeysetBoundary pins the keyset off-by-one: with exactly
// pageSize+1 workflows and a page size that forces one boundary crossing, every
// workflow must be returned exactly once — no row dropped at the boundary, none
// duplicated. This fails against the old rows[pageSize] cursor (which encodes
// the first UN-returned row and then resumes strictly past it, skipping it) and
// passes once the cursor is the last RETURNED row via filter.Paginate.
func TestE2E_ListWorkflows_KeysetBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithWorkflowsServer())
	owned := h.SeedOwnedOrg(t, "wkf-page", "WF Page", "workflows")
	ctx := context.Background()
	client := workflowsv1.NewWorkflowsClient(h.Conn())
	parent := "organizations/" + owned.Slug

	const pageSize = 3
	const total = pageSize + 1 // exactly one boundary crossing
	for i := range total {
		_, err := client.CreateWorkflow(ctx, &workflowsv1.CreateWorkflowRequest{
			Parent:     parent,
			WorkflowId: fmt.Sprintf("w%d", i),
			Workflow:   &workflowsv1.Workflow{},
		})
		require.NoError(t, err)
	}

	got := drainWorkflowNames(t, ctx, client, &workflowsv1.ListWorkflowsRequest{
		Parent:   parent,
		PageSize: pageSize,
	})
	assert.Len(t, got, total, "every workflow returned exactly once across the page boundary (no drop)")
	uniq := map[string]struct{}{}
	for _, n := range got {
		uniq[n] = struct{}{}
	}
	assert.Len(t, uniq, total, "no duplicate workflows across the page boundary")
}

// TestE2E_ListWorkflowVersions_KeysetBoundary pins the same off-by-one for the
// versions child collection: exactly pageSize+1 versions under one workflow, a
// page size forcing one boundary crossing, every version returned exactly once.
func TestE2E_ListWorkflowVersions_KeysetBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t,
		grpcharness.WithOrganizationsServer(),
		grpcharness.WithWorkflowsServer(),
		grpcharness.WithWorkflowVersionsServer())
	owned := h.SeedOwnedOrg(t, "wkf-vpage", "WFV Page", "workflows")
	ctx := context.Background()
	wfClient := workflowsv1.NewWorkflowsClient(h.Conn())
	verClient := workflowsv1.NewWorkflowVersionsClient(h.Conn())

	wf, err := wfClient.CreateWorkflow(ctx, &workflowsv1.CreateWorkflowRequest{
		Parent:     "organizations/" + owned.Slug,
		WorkflowId: "flow",
		Workflow:   &workflowsv1.Workflow{},
	})
	require.NoError(t, err)

	const pageSize = 3
	const total = pageSize + 1 // exactly one boundary crossing
	for n := 1; n <= total; n++ {
		_, err := verClient.CreateWorkflowVersion(ctx, &workflowsv1.CreateWorkflowVersionRequest{
			Parent:          wf.GetName(),
			WorkflowVersion: setStepDefinition("p"+strconv.Itoa(n), "v", `"x"`),
		})
		require.NoError(t, err)
	}

	got := drainWorkflowVersionNames(t, ctx, verClient, &workflowsv1.ListWorkflowVersionsRequest{
		Parent:   wf.GetName(),
		PageSize: pageSize,
	})
	assert.Len(t, got, total, "every version returned exactly once across the page boundary (no drop)")
	uniq := map[string]struct{}{}
	for _, n := range got {
		uniq[n] = struct{}{}
	}
	assert.Len(t, uniq, total, "no duplicate versions across the page boundary")
}
