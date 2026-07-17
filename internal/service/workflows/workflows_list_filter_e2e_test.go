package workflows_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	workflowsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/workflows/v1"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
)

// workflowDisplayNames collects a page's workflow display names in returned
// order (order-preserving, unlike the set helpers used elsewhere).
func workflowDisplayNames(wfs []*workflowsv1.Workflow) []string {
	out := make([]string, len(wfs))
	for i, w := range wfs {
		out[i] = w.GetDisplayName()
	}
	return out
}

// TestE2E_ListWorkflows_OrderByAndFilter proves ListWorkflows honors AIP-132
// order_by and AIP-160 filter after the migration onto filter.BuildListQuery.
// It fails against the pre-migration handler, which ignored both: order_by was
// dropped (every list came back id-ordered, so desc == asc) and filter was
// dropped (every list came back unfiltered, so the narrowing assertion saw all
// rows).
func TestE2E_ListWorkflows_OrderByAndFilter(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithWorkflowsServer())
	owned := h.SeedOwnedOrg(t, "wkf-ordf", "WF OrdF", "workflows")
	ctx := context.Background()
	client := workflowsv1.NewWorkflowsClient(h.Conn())
	parent := "organizations/" + owned.Slug

	// Insert display names out of sort order so id-order (creation order) differs
	// from displayName-order — an ignored order_by must not coincidentally look
	// sorted.
	displayNames := []string{"charlie", "alpha", "bravo"}
	for i, dn := range displayNames {
		_, err := client.CreateWorkflow(ctx, &workflowsv1.CreateWorkflowRequest{
			Parent:     parent,
			WorkflowId: fmt.Sprintf("w%d", i),
			Workflow:   &workflowsv1.Workflow{DisplayName: dn},
		})
		require.NoError(t, err)
	}

	asc, err := client.ListWorkflows(ctx, &workflowsv1.ListWorkflowsRequest{Parent: parent, OrderBy: "displayName asc"})
	require.NoError(t, err)
	desc, err := client.ListWorkflows(ctx, &workflowsv1.ListWorkflowsRequest{Parent: parent, OrderBy: "displayName desc"})
	require.NoError(t, err)

	ascDN := workflowDisplayNames(asc.GetWorkflows())
	descDN := workflowDisplayNames(desc.GetWorkflows())
	assert.Equal(t, []string{"alpha", "bravo", "charlie"}, ascDN, "order_by displayName asc must sort ascending")
	require.Len(t, descDN, 3)
	reversed := []string{descDN[2], descDN[1], descDN[0]}
	assert.Equal(t, ascDN, reversed, "order_by displayName desc must be the exact reverse of asc (proves order_by honored, not ignored)")
	assert.NotEqual(t, ascDN, descDN, "desc must differ from asc")

	// filter narrows to the single matching workflow.
	filtered, err := client.ListWorkflows(ctx, &workflowsv1.ListWorkflowsRequest{Parent: parent, Filter: `displayName = "bravo"`})
	require.NoError(t, err)
	require.Len(t, filtered.GetWorkflows(), 1, "filter must narrow to the matching workflow")
	assert.Equal(t, "bravo", filtered.GetWorkflows()[0].GetDisplayName())
}
