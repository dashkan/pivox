package connectors_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	typespb "github.com/dashkan/pivox/internal/pkg/gen/pivox/types"
	workflowsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/workflows/v1"
)

// bucketMap flattens a FacetResult into key→count for order-independent asserts.
func bucketMap(fr *typespb.FacetResult) map[string]int64 {
	m := make(map[string]int64, len(fr.GetBuckets()))
	for _, b := range fr.GetBuckets() {
		m[b.GetKey()] = b.GetCount()
	}
	return m
}

// TestE2E_ListConnectors_Facets_CountsAndTotal pins (a) terms-facet counts over
// the base scope + filter and (c) the exact total_count.
func TestE2E_ListConnectors_Facets_CountsAndTotal(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := rollupHarness(t)
	owned := h.SeedOwnedOrg(t, "facetc", "FacetCounts Co", "connectors")
	ctx := context.Background()
	client := workflowsv1.NewConnectorsClient(h.Conn())
	orgParent := "organizations/" + owned.Slug

	// 3 on agent-A, 2 on agent-B, 1 cloud (agent="").
	mkAgentConnector(t, ctx, client, orgParent, "a1", "agent-A")
	mkAgentConnector(t, ctx, client, orgParent, "a2", "agent-A")
	mkAgentConnector(t, ctx, client, orgParent, "a3", "agent-A")
	mkAgentConnector(t, ctx, client, orgParent, "b1", "agent-B")
	mkAgentConnector(t, ctx, client, orgParent, "b2", "agent-B")
	mkAgentConnector(t, ctx, client, orgParent, "c1", "")

	resp, err := client.ListConnectors(ctx, &workflowsv1.ListConnectorsRequest{
		Parent: orgParent,
		Aggs:   []string{"agent"},
	})
	require.NoError(t, err)

	assert.EqualValues(t, 6, resp.GetTotalCount(), "total_count is the exact base-scope row count")
	assert.False(t, resp.GetTotalIsEstimate(), "List tier total is exact, never an estimate")

	got := bucketMap(resp.GetFacets()["agent"])
	assert.Equal(t, map[string]int64{"agent-A": 3, "agent-B": 2, "": 1}, got,
		"agent facet buckets every distinct value with its count, cloud ('') included")
}

// TestE2E_ListConnectors_Facets_SelfExcluding pins (b): with agent=agent-A
// filtered, the agent facet (self-exclusion drops its OWN active filter) still
// lists agent-B, while the space facet — self-excluding too, but on a field the
// filter does NOT touch — is still narrowed by the active agent filter. List
// terms facets are always self-excluding, so both aggs are plain field ids.
func TestE2E_ListConnectors_Facets_SelfExcluding(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := rollupHarness(t)
	owned := h.SeedOwnedOrg(t, "facetse", "FacetSelfExcl Co", "connectors")
	s1 := h.SeedOwnedSpace(t, owned.Slug, "s1", "Space One")
	s2 := h.SeedOwnedSpace(t, owned.Slug, "s2", "Space Two")
	ctx := context.Background()
	client := workflowsv1.NewConnectorsClient(h.Conn())
	orgParent := "organizations/" + owned.Slug
	p1 := orgParent + "/spaces/" + s1.Slug
	p2 := orgParent + "/spaces/" + s2.Slug

	// agent-A lives in s1 and s2; agent-B lives only in s2.
	mkAgentConnector(t, ctx, client, p1, "a-s1", "agent-A")
	mkAgentConnector(t, ctx, client, p2, "a-s2", "agent-A")
	mkAgentConnector(t, ctx, client, p2, "b-s2", "agent-B")

	resp, err := client.ListConnectors(ctx, &workflowsv1.ListConnectorsRequest{
		Parent: orgParent,
		Filter: `agent = "agent-A"`,
		Aggs:   []string{"agent", "space"},
	})
	require.NoError(t, err)

	// The page itself honors the filter: only the two agent-A connectors.
	assert.Len(t, resp.GetConnectors(), 2, "the list page is filtered to agent-A")
	// total_count is over base+filter (agent-A), so 2, not 3.
	assert.EqualValues(t, 2, resp.GetTotalCount(), "total_count reflects the active filter")

	// Self-excluding agent facet: drops its OWN agent filter, so agent-B is still
	// visible (and selectable) alongside agent-A.
	agentFacet := bucketMap(resp.GetFacets()["agent"])
	assert.Equal(t, map[string]int64{"agent-A": 2, "agent-B": 1}, agentFacet,
		"self-excluding agent facet ignores its own filter — siblings stay visible")

	// The space facet self-excludes its OWN field, but the filter constrains
	// `agent`, not `space` — so space is still narrowed by agent=agent-A: only s1
	// and s2 have agent-A connectors, one each. (b-s2 is excluded.)
	spaceFacet := bucketMap(resp.GetFacets()["space"])
	assert.Equal(t, map[string]int64{s1.Row.ID.String(): 1, s2.Row.ID.String(): 1}, spaceFacet,
		"a facet on a field the filter does not touch is still narrowed by that filter")
}

// TestE2E_ListConnectors_Facets_UnknownFieldRejected pins (d): a facet field
// not in the Facetable allowlist is InvalidArgument.
func TestE2E_ListConnectors_Facets_UnknownFieldRejected(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := rollupHarness(t)
	owned := h.SeedOwnedOrg(t, "facetbad", "FacetBad Co", "connectors")
	ctx := context.Background()
	client := workflowsv1.NewConnectorsClient(h.Conn())

	_, err := client.ListConnectors(ctx, &workflowsv1.ListConnectorsRequest{
		Parent: "organizations/" + owned.Slug,
		Aggs:   []string{"description"}, // filterable but NOT facetable
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err), "unknown facet field → InvalidArgument")
}

// TestE2E_ListConnectors_Facets_NoAggsZeroCost pins (e): a request with no aggs
// returns empty facets and a zero total_count (no aggregation performed).
func TestE2E_ListConnectors_Facets_NoAggsZeroCost(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := rollupHarness(t)
	owned := h.SeedOwnedOrg(t, "facetnone", "FacetNone Co", "connectors")
	ctx := context.Background()
	client := workflowsv1.NewConnectorsClient(h.Conn())
	orgParent := "organizations/" + owned.Slug

	mkAgentConnector(t, ctx, client, orgParent, "x1", "agent-A")

	resp, err := client.ListConnectors(ctx, &workflowsv1.ListConnectorsRequest{Parent: orgParent})
	require.NoError(t, err)
	assert.Empty(t, resp.GetFacets(), "no aggs → no facets computed")
	assert.Zero(t, resp.GetTotalCount(), "no aggs → total_count not computed (zero)")
}

// TestE2E_ListConnectors_Facets_TermsSize pins top-N truncation by the `:size`
// suffix on an agg string.
func TestE2E_ListConnectors_Facets_TermsSize(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := rollupHarness(t)
	owned := h.SeedOwnedOrg(t, "facetsize", "FacetSize Co", "connectors")
	ctx := context.Background()
	client := workflowsv1.NewConnectorsClient(h.Conn())
	orgParent := "organizations/" + owned.Slug

	// 3 agents, descending counts A(3) > B(2) > C(1); size=2 keeps the top two.
	for _, id := range []string{"a1", "a2", "a3"} {
		mkAgentConnector(t, ctx, client, orgParent, id, "agent-A")
	}
	mkAgentConnector(t, ctx, client, orgParent, "b1", "agent-B")
	mkAgentConnector(t, ctx, client, orgParent, "b2", "agent-B")
	mkAgentConnector(t, ctx, client, orgParent, "c1", "agent-C")

	resp, err := client.ListConnectors(ctx, &workflowsv1.ListConnectorsRequest{
		Parent: orgParent,
		Aggs:   []string{"agent:2"},
	})
	require.NoError(t, err)

	buckets := resp.GetFacets()["agent"].GetBuckets()
	require.Len(t, buckets, 2, "size=2 keeps only the top two buckets")
	assert.Equal(t, "agent-A", buckets[0].GetKey(), "buckets are count-desc")
	assert.EqualValues(t, 3, buckets[0].GetCount())
	assert.Equal(t, "agent-B", buckets[1].GetKey())
	assert.EqualValues(t, 2, buckets[1].GetCount())
}
