package connectors_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	workflowsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/workflows/v1"
)

// mkAgentConnector creates a connector with an explicit agent value (empty =
// cloud controller) so a test can seed a mix of cloud and agent-bound rows.
func mkAgentConnector(t *testing.T, ctx context.Context, client workflowsv1.ConnectorsClient, parent, id, agent string) {
	t.Helper()
	_, err := client.CreateConnector(ctx, &workflowsv1.CreateConnectorRequest{
		Parent:      parent,
		ConnectorId: id,
		Connector: &workflowsv1.Connector{
			DisplayName: id,
			Agent:       agent,
			Config:      &workflowsv1.Connector_Http{Http: &workflowsv1.HttpConnector{BaseUrl: "https://x.example.com"}},
		},
	})
	require.NoError(t, err)
}

// TestE2E_ListConnectors_AgentsInUseFacet pins the "agents in use" facet:
// distinct, sorted, non-empty agent values over the base scope, spanning
// org-direct + space rows for an org-level list and only the space's rows for a
// space-level list.
func TestE2E_ListConnectors_AgentsInUseFacet(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := rollupHarness(t)
	owned := h.SeedOwnedOrg(t, "facet", "Facet Co", "connectors")
	space := h.SeedOwnedSpace(t, owned.Slug, "team", "Team")
	ctx := context.Background()
	client := workflowsv1.NewConnectorsClient(h.Conn())

	orgParent := "organizations/" + owned.Slug
	spaceParent := orgParent + "/spaces/" + space.Slug

	// Org-direct: one cloud (agent=''), one on agent A.
	mkAgentConnector(t, ctx, client, orgParent, "org-cloud", "")
	mkAgentConnector(t, ctx, client, orgParent, "org-a", "agent-A")
	// Space: one cloud, one on agent B, one also on agent A (dup across scope).
	mkAgentConnector(t, ctx, client, spaceParent, "sp-cloud", "")
	mkAgentConnector(t, ctx, client, spaceParent, "sp-b", "agent-B")
	mkAgentConnector(t, ctx, client, spaceParent, "sp-a", "agent-A")

	// Org-level facet: distinct non-empty agents across org + space, sorted.
	orgResp, err := client.ListConnectors(ctx, &workflowsv1.ListConnectorsRequest{Parent: orgParent})
	require.NoError(t, err)
	assert.Equal(t, []string{"agent-A", "agent-B"}, orgResp.GetAgentsInUse(),
		"org rollup facet is the sorted distinct non-empty agent set across org + all spaces")

	// Space-level facet: only that space's agents (A and B), not scoped elsewhere.
	spaceResp, err := client.ListConnectors(ctx, &workflowsv1.ListConnectorsRequest{Parent: spaceParent})
	require.NoError(t, err)
	assert.Equal(t, []string{"agent-A", "agent-B"}, spaceResp.GetAgentsInUse(),
		"space facet lists only that space's agents")
}

// TestE2E_ListConnectors_AgentsInUseFacet_SpaceScopedSubset proves the space
// facet is a strict subset of the org facet: an agent used ONLY org-direct does
// not appear in a space's facet, and vice versa.
func TestE2E_ListConnectors_AgentsInUseFacet_SpaceScopedSubset(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := rollupHarness(t)
	owned := h.SeedOwnedOrg(t, "facetsub", "FacetSub Co", "connectors")
	space := h.SeedOwnedSpace(t, owned.Slug, "team", "Team")
	ctx := context.Background()
	client := workflowsv1.NewConnectorsClient(h.Conn())

	orgParent := "organizations/" + owned.Slug
	spaceParent := orgParent + "/spaces/" + space.Slug

	mkAgentConnector(t, ctx, client, orgParent, "org-only", "org-agent")
	mkAgentConnector(t, ctx, client, spaceParent, "sp-only", "space-agent")

	orgResp, err := client.ListConnectors(ctx, &workflowsv1.ListConnectorsRequest{Parent: orgParent})
	require.NoError(t, err)
	assert.Equal(t, []string{"org-agent", "space-agent"}, orgResp.GetAgentsInUse(),
		"org rollup sees both the org-direct and the space agent")

	spaceResp, err := client.ListConnectors(ctx, &workflowsv1.ListConnectorsRequest{Parent: spaceParent})
	require.NoError(t, err)
	assert.Equal(t, []string{"space-agent"}, spaceResp.GetAgentsInUse(),
		"space facet excludes the org-direct-only agent")
}

// TestE2E_ListConnectors_AgentsInUseFacet_AllCloud pins that an all-cloud scope
// (every connector agent=”) yields an empty facet, never a [""] entry.
func TestE2E_ListConnectors_AgentsInUseFacet_AllCloud(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := rollupHarness(t)
	owned := h.SeedOwnedOrg(t, "facetcloud", "FacetCloud Co", "connectors")
	ctx := context.Background()
	client := workflowsv1.NewConnectorsClient(h.Conn())
	orgParent := "organizations/" + owned.Slug

	mkAgentConnector(t, ctx, client, orgParent, "c1", "")
	mkAgentConnector(t, ctx, client, orgParent, "c2", "")

	resp, err := client.ListConnectors(ctx, &workflowsv1.ListConnectorsRequest{Parent: orgParent})
	require.NoError(t, err)
	assert.Empty(t, resp.GetAgentsInUse(), "an all-cloud scope has no agents in use (no empty-string entry)")
}

// TestE2E_ListConnectors_AgentsInUseFacet_IndependentOfFilter pins that the
// facet is computed over the base scope, NOT the request filter: a filter that
// narrows the page to a single row still reports the full in-scope agent set.
func TestE2E_ListConnectors_AgentsInUseFacet_IndependentOfFilter(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := rollupHarness(t)
	owned := h.SeedOwnedOrg(t, "facetfltr", "FacetFltr Co", "connectors")
	ctx := context.Background()
	client := workflowsv1.NewConnectorsClient(h.Conn())
	orgParent := "organizations/" + owned.Slug

	mkAgentConnector(t, ctx, client, orgParent, "keep", "agent-A")
	mkAgentConnector(t, ctx, client, orgParent, "drop", "agent-B")

	// The filter narrows the page to the single "keep" row, but the facet must
	// still report BOTH agents in scope.
	resp, err := client.ListConnectors(ctx, &workflowsv1.ListConnectorsRequest{
		Parent: orgParent, Filter: `displayName = "keep"`,
	})
	require.NoError(t, err)
	require.Len(t, resp.GetConnectors(), 1, "precondition: the filter narrowed the page to one row")
	assert.Equal(t, []string{"agent-A", "agent-B"}, resp.GetAgentsInUse(),
		"the facet spans the base scope, unaffected by the request filter")
}
