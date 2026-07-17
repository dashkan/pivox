package workflows_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	db "github.com/dashkan/pivox/internal/db/generated"
	workflowsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/workflows/v1"
)

// orderedRunNames collects a run page's resource names in returned order.
func orderedRunNames(runs []*workflowsv1.WorkflowRun) []string {
	out := make([]string, len(runs))
	for i, r := range runs {
		out[i] = r.GetName()
	}
	return out
}

// TestE2E_ListWorkflowRuns_OrderByAndStateFilter proves ListWorkflowRuns honors
// AIP-132 order_by after the migration onto filter.BuildListQuery, while the
// state filter still narrows. It fails against the pre-migration handler, which
// ignored order_by entirely (runs were always id/creation order, so desc == asc).
// The state-filter half is a preservation check: it stayed green across the
// migration (the bespoke parser already honored `state = "…"`).
func TestE2E_ListWorkflowRuns_OrderByAndStateFilter(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := runHarness(t)
	owned := h.SeedOwnedOrg(t, "wfr-ordf", "WFR OrdF", "workflows")
	ctx := context.Background()
	wfClient := workflowsv1.NewWorkflowsClient(h.Conn())
	verClient := workflowsv1.NewWorkflowVersionsClient(h.Conn())
	runClient := workflowsv1.NewWorkflowRunsClient(h.Conn())

	wf, _ := newPromotedWorkflow(t, ctx, wfClient, verClient, owned.Slug, "flow")

	const total = 4
	created := make([]string, 0, total)
	for range total {
		run, err := runClient.RunWorkflow(ctx, &workflowsv1.RunWorkflowRequest{Name: wf.GetName()})
		require.NoError(t, err)
		created = append(created, run.GetName())
	}

	asc, err := runClient.ListWorkflowRuns(ctx, &workflowsv1.ListWorkflowRunsRequest{Parent: wf.GetName(), OrderBy: "createTime asc"})
	require.NoError(t, err)
	desc, err := runClient.ListWorkflowRuns(ctx, &workflowsv1.ListWorkflowRunsRequest{Parent: wf.GetName(), OrderBy: "createTime desc"})
	require.NoError(t, err)

	ascN := orderedRunNames(asc.GetWorkflowRuns())
	descN := orderedRunNames(desc.GetWorkflowRuns())
	require.Len(t, ascN, total)
	require.Len(t, descN, total)
	// (create_time, id) is unique per row, so createTime DESC must be the exact
	// reverse of createTime ASC — this is what proves order_by is honored.
	rev := make([]string, total)
	for i, n := range descN {
		rev[total-1-i] = n
	}
	assert.Equal(t, ascN, rev, "createTime desc must be the exact reverse of asc (proves order_by honored, not ignored)")
	assert.NotEqual(t, ascN, descN, "desc must differ from asc")

	// State filter still narrows: drive one run to RUNNING (no executor in the
	// API-layer test) and filter for it.
	_, err = h.Queries.UpdateWorkflowRunState(ctx, db.UpdateWorkflowRunStateParams{
		ID:    runIDFromName(t, created[0]),
		State: workflowsv1.State_RUNNING.String(),
	})
	require.NoError(t, err)

	running, err := runClient.ListWorkflowRuns(ctx, &workflowsv1.ListWorkflowRunsRequest{Parent: wf.GetName(), Filter: `state = "RUNNING"`})
	require.NoError(t, err)
	require.Len(t, running.GetWorkflowRuns(), 1, "state filter must narrow to the RUNNING run")
	assert.Equal(t, created[0], running.GetWorkflowRuns()[0].GetName())
	assert.Equal(t, workflowsv1.State_RUNNING, running.GetWorkflowRuns()[0].GetState())
}
