package connectors_test

import (
	"context"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	workflowsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/workflows/v1"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
)

// mkConnector creates a connector with the given id (slug) + display name under
// parent, and fails the test on error.
func mkConnector(t *testing.T, ctx context.Context, client workflowsv1.ConnectorsClient, parent, id, displayName string) *workflowsv1.Connector {
	t.Helper()
	c, err := client.CreateConnector(ctx, &workflowsv1.CreateConnectorRequest{
		Parent:      parent,
		ConnectorId: id,
		Connector: &workflowsv1.Connector{
			DisplayName: displayName,
			Config:      &workflowsv1.Connector_Http{Http: &workflowsv1.HttpConnector{BaseUrl: "https://x.example.com"}},
		},
	})
	require.NoError(t, err)
	return c
}

// listDisplayNames returns the display names of a single ListConnectors page.
func listDisplayNames(cs []*workflowsv1.Connector) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.GetDisplayName())
	}
	return out
}

// drainAll follows page tokens to completion, returning every connector name
// (the slug-leaf resource name) across all pages, and fails if any page loop
// runs away.
func drainAll(t *testing.T, ctx context.Context, client workflowsv1.ConnectorsClient, req *workflowsv1.ListConnectorsRequest) []string {
	t.Helper()
	var names []string
	token := ""
	for i := 0; i < 100; i++ {
		req.PageToken = token
		resp, err := client.ListConnectors(ctx, req)
		require.NoError(t, err)
		for _, c := range resp.GetConnectors() {
			names = append(names, c.GetName())
		}
		token = resp.GetNextPageToken()
		if token == "" {
			return names
		}
	}
	t.Fatal("pagination did not terminate within 100 pages")
	return nil
}

func TestE2E_ListConnectors_FilterByDisplayName(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithConnectorsServer())
	owned := h.SeedOwnedOrg(t, "fltr", "Fltr Co", "connectors")
	ctx := context.Background()
	client := workflowsv1.NewConnectorsClient(h.Conn())
	parent := "organizations/" + owned.Slug

	mkConnector(t, ctx, client, parent, "a", "VizRT Hub")
	mkConnector(t, ctx, client, parent, "b", "News Hub")
	mkConnector(t, ctx, client, parent, "c", "Weather Service")
	// Literal-backslash pair: `esc` contains a backslash; `noesc` is the
	// decoy that WOULD be matched if the caller's '\' were consumed as the
	// LIKE escape character instead of matched literally.
	mkConnector(t, ctx, client, parent, "d", `a\b Node`)
	mkConnector(t, ctx, client, parent, "e", "ab Node")

	// Exact match.
	resp, err := client.ListConnectors(ctx, &workflowsv1.ListConnectorsRequest{
		Parent: parent, Filter: `displayName = "VizRT Hub"`,
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"VizRT Hub"}, listDisplayNames(resp.GetConnectors()))

	// Substring (`:`) — both "…Hub" rows, neither the Weather one.
	resp, err = client.ListConnectors(ctx, &workflowsv1.ListConnectorsRequest{
		Parent: parent, Filter: `displayName : "Hub"`,
	})
	require.NoError(t, err)
	got := listDisplayNames(resp.GetConnectors())
	sort.Strings(got)
	assert.Equal(t, []string{"News Hub", "VizRT Hub"}, got)

	// Wildcard `=` also lowers to ILIKE (AllowPartial).
	resp, err = client.ListConnectors(ctx, &workflowsv1.ListConnectorsRequest{
		Parent: parent, Filter: `displayName = "Weather*"`,
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"Weather Service"}, listDisplayNames(resp.GetConnectors()))

	// Literal backslash in the wildcard operand must match LITERALLY. The
	// filter source `a\\b*` unescapes (CEL) to prefix `a\b`, which matches
	// `a\b Node` and must NOT match the `ab Node` decoy — proving the '\' is
	// escaped and the fragment carries `ESCAPE '\'` end-to-end, not consumed
	// as the implicit LIKE escape character.
	resp, err = client.ListConnectors(ctx, &workflowsv1.ListConnectorsRequest{
		Parent: parent, Filter: `displayName = "a\\b*"`,
	})
	require.NoError(t, err)
	assert.Equal(t, []string{`a\b Node`}, listDisplayNames(resp.GetConnectors()))
}

func TestE2E_ListConnectors_EmptyFilterAllInScope(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithConnectorsServer())
	owned := h.SeedOwnedOrg(t, "empty", "Empty Co", "connectors")
	ctx := context.Background()
	client := workflowsv1.NewConnectorsClient(h.Conn())
	parent := "organizations/" + owned.Slug

	for _, id := range []string{"a", "b", "c"} {
		mkConnector(t, ctx, client, parent, id, "conn-"+id)
	}
	resp, err := client.ListConnectors(ctx, &workflowsv1.ListConnectorsRequest{Parent: parent})
	require.NoError(t, err)
	assert.Len(t, resp.GetConnectors(), 3, "empty filter returns all in-scope rows")
}

func TestE2E_ListConnectors_OrderByDisplayName(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithConnectorsServer())
	owned := h.SeedOwnedOrg(t, "ordr", "Ordr Co", "connectors")
	ctx := context.Background()
	client := workflowsv1.NewConnectorsClient(h.Conn())
	parent := "organizations/" + owned.Slug

	// Create in an order that is NOT alphabetical, so sorting by displayName
	// differs from the id (creation) order.
	mkConnector(t, ctx, client, parent, "id1", "charlie")
	mkConnector(t, ctx, client, parent, "id2", "alpha")
	mkConnector(t, ctx, client, parent, "id3", "bravo")

	resp, err := client.ListConnectors(ctx, &workflowsv1.ListConnectorsRequest{
		Parent: parent, OrderBy: "displayName",
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"alpha", "bravo", "charlie"}, listDisplayNames(resp.GetConnectors()))

	resp, err = client.ListConnectors(ctx, &workflowsv1.ListConnectorsRequest{
		Parent: parent, OrderBy: "displayName desc",
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"charlie", "bravo", "alpha"}, listDisplayNames(resp.GetConnectors()))
}

func TestE2E_ListConnectors_OrderByCreateTime(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithConnectorsServer())
	owned := h.SeedOwnedOrg(t, "ordct", "OrdCT Co", "connectors")
	ctx := context.Background()
	client := workflowsv1.NewConnectorsClient(h.Conn())
	parent := "organizations/" + owned.Slug

	mkConnector(t, ctx, client, parent, "id1", "first")
	mkConnector(t, ctx, client, parent, "id2", "second")
	mkConnector(t, ctx, client, parent, "id3", "third")

	resp, err := client.ListConnectors(ctx, &workflowsv1.ListConnectorsRequest{
		Parent: parent, OrderBy: "createTime",
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"first", "second", "third"}, listDisplayNames(resp.GetConnectors()))

	resp, err = client.ListConnectors(ctx, &workflowsv1.ListConnectorsRequest{
		Parent: parent, OrderBy: "createTime desc",
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"third", "second", "first"}, listDisplayNames(resp.GetConnectors()))
}

func TestE2E_ListConnectors_OrderByUpdateTime(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithConnectorsServer())
	owned := h.SeedOwnedOrg(t, "ordut", "OrdUT Co", "connectors")
	ctx := context.Background()
	client := workflowsv1.NewConnectorsClient(h.Conn())
	parent := "organizations/" + owned.Slug

	a := mkConnector(t, ctx, client, parent, "id1", "a")
	mkConnector(t, ctx, client, parent, "id2", "b")
	mkConnector(t, ctx, client, parent, "id3", "c")

	// Touch "a" so its update_time becomes the newest, decoupling updateTime
	// order from createTime order.
	_, err := client.UpdateConnector(ctx, &workflowsv1.UpdateConnectorRequest{
		Connector: &workflowsv1.Connector{Name: a.GetName(), DisplayName: "a2"},
	})
	require.NoError(t, err)

	resp, err := client.ListConnectors(ctx, &workflowsv1.ListConnectorsRequest{
		Parent: parent, OrderBy: "updateTime desc",
	})
	require.NoError(t, err)
	// "a2" (just updated) sorts first by updateTime desc.
	assert.Equal(t, "a2", resp.GetConnectors()[0].GetDisplayName())
}

// TestE2E_ListConnectors_KeysetCoverage_CustomSort pins the hard case: keyset
// pagination under a custom (displayName) sort must return every row exactly
// once across page boundaries — no dupes, no skips — using the compound cursor.
func TestE2E_ListConnectors_KeysetCoverage_CustomSort(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithConnectorsServer())
	owned := h.SeedOwnedOrg(t, "keyset", "Keyset Co", "connectors")
	ctx := context.Background()
	client := workflowsv1.NewConnectorsClient(h.Conn())
	parent := "organizations/" + owned.Slug

	// 7 rows, page size 3 → boundaries at 3 and 6 (an exact multiple case at the
	// last page). Names deliberately out of creation order.
	names := []string{"gg", "aa", "ee", "cc", "bb", "ff", "dd"}
	for i, n := range names {
		mkConnector(t, ctx, client, parent, "id"+string(rune('0'+i)), n)
	}

	got := drainAll(t, ctx, client, &workflowsv1.ListConnectorsRequest{
		Parent: parent, OrderBy: "displayName", PageSize: 3,
	})
	assert.Len(t, got, 7, "every row returned exactly once (no dupes/skips)")
	uniq := map[string]struct{}{}
	for _, n := range got {
		uniq[n] = struct{}{}
	}
	assert.Len(t, uniq, 7, "no duplicate rows across page boundaries")
}

// TestE2E_ListConnectors_KeysetCoverage_DuplicateSortKeys stresses the id
// tiebreaker: many rows share the SAME display name, so the compound cursor
// must fall through to id to avoid dropping or repeating rows at a boundary.
func TestE2E_ListConnectors_KeysetCoverage_DuplicateSortKeys(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithConnectorsServer())
	owned := h.SeedOwnedOrg(t, "dupkey", "DupKey Co", "connectors")
	ctx := context.Background()
	client := workflowsv1.NewConnectorsClient(h.Conn())
	parent := "organizations/" + owned.Slug

	const n = 8
	for i := 0; i < n; i++ {
		mkConnector(t, ctx, client, parent, "id"+string(rune('0'+i)), "same-name")
	}

	got := drainAll(t, ctx, client, &workflowsv1.ListConnectorsRequest{
		Parent: parent, OrderBy: "displayName", PageSize: 3,
	})
	assert.Len(t, got, n, "all rows with identical sort keys are covered")
	uniq := map[string]struct{}{}
	for _, name := range got {
		uniq[name] = struct{}{}
	}
	assert.Len(t, uniq, n, "id tiebreaker prevents dupes across boundaries")
}

// TestE2E_ListConnectors_KeysetCoverage_DefaultIDSort covers the simple
// id-only keyset path (no order_by) across a boundary.
func TestE2E_ListConnectors_KeysetCoverage_DefaultIDSort(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithConnectorsServer())
	owned := h.SeedOwnedOrg(t, "idsort", "IDSort Co", "connectors")
	ctx := context.Background()
	client := workflowsv1.NewConnectorsClient(h.Conn())
	parent := "organizations/" + owned.Slug

	const n = 5
	for i := 0; i < n; i++ {
		mkConnector(t, ctx, client, parent, "id"+string(rune('0'+i)), "c")
	}
	got := drainAll(t, ctx, client, &workflowsv1.ListConnectorsRequest{Parent: parent, PageSize: 2})
	assert.Len(t, got, n)
	uniq := map[string]struct{}{}
	for _, name := range got {
		uniq[name] = struct{}{}
	}
	assert.Len(t, uniq, n, "default id keyset covers all rows once")
}

func TestE2E_ListConnectors_UnknownFieldsRejected(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithConnectorsServer())
	owned := h.SeedOwnedOrg(t, "reject", "Reject Co", "connectors")
	ctx := context.Background()
	client := workflowsv1.NewConnectorsClient(h.Conn())
	parent := "organizations/" + owned.Slug
	mkConnector(t, ctx, client, parent, "a", "a")

	// Unknown filter field → InvalidArgument, not a silent empty result.
	_, err := client.ListConnectors(ctx, &workflowsv1.ListConnectorsRequest{
		Parent: parent, Filter: `secretColumn = "x"`,
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))

	// Unknown order_by field → InvalidArgument (not in the sortable whitelist).
	_, err = client.ListConnectors(ctx, &workflowsv1.ListConnectorsRequest{
		Parent: parent, OrderBy: "slug",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))

	// A garbage page_token → InvalidArgument.
	_, err = client.ListConnectors(ctx, &workflowsv1.ListConnectorsRequest{
		Parent: parent, PageToken: "not-a-real-token",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

// TestE2E_ListConnectors_InjectionInert pins that a SQL-injection payload in a
// filter value is treated as a literal operand: it matches nothing, errors
// nothing, and leaves the other rows intact and listable.
func TestE2E_ListConnectors_InjectionInert(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithConnectorsServer())
	owned := h.SeedOwnedOrg(t, "inject", "Inject Co", "connectors")
	ctx := context.Background()
	client := workflowsv1.NewConnectorsClient(h.Conn())
	parent := "organizations/" + owned.Slug

	mkConnector(t, ctx, client, parent, "a", "real-one")
	mkConnector(t, ctx, client, parent, "b", "real-two")

	// Classic tautology payload as an exact-match value — inert, matches nothing.
	resp, err := client.ListConnectors(ctx, &workflowsv1.ListConnectorsRequest{
		Parent: parent, Filter: `displayName = "x' OR '1'='1"`,
	})
	require.NoError(t, err, "an injection payload must be a harmless no-match, not an error")
	assert.Empty(t, resp.GetConnectors(), "payload matched no row — it was NOT executed as SQL")

	// Statement-terminator payload as a substring — likewise inert.
	resp, err = client.ListConnectors(ctx, &workflowsv1.ListConnectorsRequest{
		Parent: parent, Filter: `displayName : "'; DROP TABLE connectors;--"`,
	})
	require.NoError(t, err)
	assert.Empty(t, resp.GetConnectors())

	// The table is intact and the real rows still list.
	resp, err = client.ListConnectors(ctx, &workflowsv1.ListConnectorsRequest{Parent: parent})
	require.NoError(t, err)
	assert.Len(t, resp.GetConnectors(), 2, "injection attempts left the data intact")
}

// TestE2E_ListConnectors_ScopeIsolation pins that the base scope is
// non-negotiable: a filter cannot widen a list beyond its org, and one org's
// list never returns another org's connectors.
func TestE2E_ListConnectors_ScopeIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithConnectorsServer())
	// One owner owns both orgs (SeedOwnedOrg sets the caller; org B is then
	// created through the orgs client as that same caller).
	a := h.SeedOwnedOrg(t, "iso-list-a", "Iso List A", "connectors")
	ctx := context.Background()
	client := workflowsv1.NewConnectorsClient(h.Conn())

	op, err := apiv1.NewOrganizationsClient(h.Conn()).CreateOrganization(ctx,
		&apiv1.CreateOrganizationRequest{
			OrganizationId: "iso-list-b",
			Organization:   &apiv1.Organization{DisplayName: "Iso List B"},
		})
	require.NoError(t, err)
	require.True(t, op.GetDone())
	bParent := "organizations/iso-list-b"

	mkConnector(t, ctx, client, "organizations/"+a.Slug, "a-only", "A Only")
	mkConnector(t, ctx, client, bParent, "b-only", "B Only")

	// List A: only A's connector, even with a filter that would also match B's.
	resp, err := client.ListConnectors(ctx, &workflowsv1.ListConnectorsRequest{
		Parent: "organizations/" + a.Slug, Filter: `displayName : "Only"`,
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"A Only"}, listDisplayNames(resp.GetConnectors()), "filter can only narrow within the org scope")

	// List B: only B's connector.
	resp, err = client.ListConnectors(ctx, &workflowsv1.ListConnectorsRequest{Parent: bParent})
	require.NoError(t, err)
	assert.Equal(t, []string{"B Only"}, listDisplayNames(resp.GetConnectors()))
}
