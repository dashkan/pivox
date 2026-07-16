package connectors_test

import (
	"context"
	"encoding/json"
	"sort"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	db "github.com/dashkan/pivox/internal/db/generated"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	workflowsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/workflows/v1"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
)

// rollupHarness wires the org, space, and connector services so a single test
// can create an org-direct connector plus space-scoped connectors and exercise
// the org-level rollup.
func rollupHarness(t *testing.T) *grpcharness.Harness {
	t.Helper()
	return grpcharness.New(t,
		grpcharness.WithOrganizationsServer(),
		grpcharness.WithSpacesServer(),
		grpcharness.WithConnectorsServer())
}

// TestE2E_ListConnectors_OrgLevelRollup pins the rollup: an org-level list
// returns org-direct connectors AND every space's connectors, each rendered
// with its actual (org-direct or space-scoped) resource name.
func TestE2E_ListConnectors_OrgLevelRollup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := rollupHarness(t)
	owned := h.SeedOwnedOrg(t, "rollup", "Rollup Co", "connectors")
	spaceA := h.SeedOwnedSpace(t, owned.Slug, "team-a", "Team A")
	spaceB := h.SeedOwnedSpace(t, owned.Slug, "team-b", "Team B")
	ctx := context.Background()
	client := workflowsv1.NewConnectorsClient(h.Conn())

	orgParent := "organizations/" + owned.Slug
	spaceAParent := orgParent + "/spaces/" + spaceA.Slug
	spaceBParent := orgParent + "/spaces/" + spaceB.Slug

	mkConnector(t, ctx, client, orgParent, "org-conn", "Org Direct")
	mkConnector(t, ctx, client, spaceAParent, "a-conn", "Space A Conn")
	mkConnector(t, ctx, client, spaceBParent, "b-conn", "Space B Conn")

	// Org-level list rolls up all three, each with its actual name.
	got := drainAll(t, ctx, client, &workflowsv1.ListConnectorsRequest{Parent: orgParent})
	sort.Strings(got)
	want := []string{
		orgParent + "/connectors/org-conn",
		spaceAParent + "/connectors/a-conn",
		spaceBParent + "/connectors/b-conn",
	}
	sort.Strings(want)
	assert.Equal(t, want, got, "org-level rollup returns org-direct + all space rows with correct names")

	// The org-direct row carries no /spaces/ segment; the space rows do.
	assert.Contains(t, got, orgParent+"/connectors/org-conn")
	for _, n := range got {
		if n == orgParent+"/connectors/org-conn" {
			assert.NotContains(t, n, "/spaces/", "org-direct row is named without a space segment")
		}
	}
}

// TestE2E_ListConnectors_SpaceLevelScoped pins that a space-level list is still
// scoped to that one space — it does NOT roll up org-direct or sibling-space
// connectors.
func TestE2E_ListConnectors_SpaceLevelScoped(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := rollupHarness(t)
	owned := h.SeedOwnedOrg(t, "spscope", "SpScope Co", "connectors")
	spaceA := h.SeedOwnedSpace(t, owned.Slug, "team-a", "Team A")
	spaceB := h.SeedOwnedSpace(t, owned.Slug, "team-b", "Team B")
	ctx := context.Background()
	client := workflowsv1.NewConnectorsClient(h.Conn())

	orgParent := "organizations/" + owned.Slug
	spaceAParent := orgParent + "/spaces/" + spaceA.Slug
	spaceBParent := orgParent + "/spaces/" + spaceB.Slug

	mkConnector(t, ctx, client, orgParent, "org-conn", "Org Direct")
	mkConnector(t, ctx, client, spaceAParent, "a-conn", "Space A Conn")
	mkConnector(t, ctx, client, spaceBParent, "b-conn", "Space B Conn")

	// Space A's list: only Space A's connector.
	resp, err := client.ListConnectors(ctx, &workflowsv1.ListConnectorsRequest{Parent: spaceAParent})
	require.NoError(t, err)
	assert.Equal(t, []string{spaceAParent + "/connectors/a-conn"}, namesOf(resp.GetConnectors()),
		"a space-level list returns only that space's connector")
}

// TestE2E_ListConnectors_RollupSortFilterKeyset pins that sort, a page
// boundary, and a filter all hold across the mixed org+space rollup.
func TestE2E_ListConnectors_RollupSortFilterKeyset(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := rollupHarness(t)
	owned := h.SeedOwnedOrg(t, "rmix", "RMix Co", "connectors")
	spaceA := h.SeedOwnedSpace(t, owned.Slug, "team-a", "Team A")
	spaceB := h.SeedOwnedSpace(t, owned.Slug, "team-b", "Team B")
	ctx := context.Background()
	client := workflowsv1.NewConnectorsClient(h.Conn())

	orgParent := "organizations/" + owned.Slug
	spaceAParent := orgParent + "/spaces/" + spaceA.Slug
	spaceBParent := orgParent + "/spaces/" + spaceB.Slug

	// Display names chosen so displayName order interleaves org and space rows:
	// asc → aaa(spaceA), mmm(org), sss(spaceB), zzz(spaceA).
	orgConn := mkConnector(t, ctx, client, orgParent, "org-conn", "mmm")
	aConn1 := mkConnector(t, ctx, client, spaceAParent, "a-1", "aaa")
	bConn := mkConnector(t, ctx, client, spaceBParent, "b-1", "sss")
	aConn2 := mkConnector(t, ctx, client, spaceAParent, "a-2", "zzz")

	// Sort across the rollup: displayName asc, interleaving org and space rows.
	resp, err := client.ListConnectors(ctx, &workflowsv1.ListConnectorsRequest{
		Parent: orgParent, OrderBy: "displayName",
	})
	require.NoError(t, err)
	assert.Equal(t,
		[]string{
			aConn1.GetName(),  // aaa (space A)
			orgConn.GetName(), // mmm (org-direct)
			bConn.GetName(),   // sss (space B)
			aConn2.GetName(),  // zzz (space A)
		},
		namesOf(resp.GetConnectors()),
		"displayName sort orders the mixed org+space rollup correctly")

	// Keyset across the rollup: a page boundary smaller than the set must return
	// a working next_page_token that continues correctly across the org/space mix.
	paged := drainAll(t, ctx, client, &workflowsv1.ListConnectorsRequest{
		Parent: orgParent, OrderBy: "displayName", PageSize: 2,
	})
	assert.Equal(t,
		[]string{aConn1.GetName(), orgConn.GetName(), bConn.GetName(), aConn2.GetName()},
		paged,
		"keyset pagination covers the mixed rollup in sorted order, no dupes/skips")

	// Filter across the rollup: an exact-match narrows to the one org-direct row.
	resp, err = client.ListConnectors(ctx, &workflowsv1.ListConnectorsRequest{
		Parent: orgParent, Filter: `displayName = "mmm"`,
	})
	require.NoError(t, err)
	assert.Equal(t, []string{orgConn.GetName()}, namesOf(resp.GetConnectors()),
		"filter narrows across the rollup")
}

// TestE2E_ListConnectors_RollupNameRoundTrip pins that a name minted by the
// org-level rollup for a space-scoped row resolves the same row via
// GetConnector — the space-scoped name is well-formed and addressable.
func TestE2E_ListConnectors_RollupNameRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := rollupHarness(t)
	owned := h.SeedOwnedOrg(t, "rtrip", "RTrip Co", "connectors")
	spaceA := h.SeedOwnedSpace(t, owned.Slug, "team-a", "Team A")
	ctx := context.Background()
	client := workflowsv1.NewConnectorsClient(h.Conn())

	orgParent := "organizations/" + owned.Slug
	spaceAParent := orgParent + "/spaces/" + spaceA.Slug
	mkConnector(t, ctx, client, orgParent, "org-conn", "Org Direct")
	created := mkConnector(t, ctx, client, spaceAParent, "a-conn", "Space A Conn")

	// Find the space row's name as the org-level rollup renders it.
	resp, err := client.ListConnectors(ctx, &workflowsv1.ListConnectorsRequest{Parent: orgParent})
	require.NoError(t, err)
	var rolledName string
	for _, c := range resp.GetConnectors() {
		if c.GetDisplayName() == "Space A Conn" {
			rolledName = c.GetName()
		}
	}
	require.Equal(t, spaceAParent+"/connectors/a-conn", rolledName,
		"the rollup names the space row with its space-scoped path")

	// GetConnector by that rolled-up name resolves the same row.
	got, err := client.GetConnector(ctx, &workflowsv1.GetConnectorRequest{Name: rolledName})
	require.NoError(t, err)
	assert.Equal(t, created.GetName(), got.GetName())
	assert.Equal(t, "Space A Conn", got.GetDisplayName())
}

// TestE2E_ListConnectors_RollupOmitsCrossOrgSpaceRow pins the fail-safe for the
// cross-org anomaly: a connector row in org A whose space_id points at a space
// owned by org B (no same-org FK forbids it) has no resolvable slug in org A's
// scope, so the org-level rollup OMITS it rather than emitting a mis-addressable
// org-direct name. The anomaly is unreachable via the API, so it is induced by a
// direct DB insert that bypasses the handler's scope resolution.
func TestE2E_ListConnectors_RollupOmitsCrossOrgSpaceRow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := rollupHarness(t)
	orgA := h.SeedOwnedOrg(t, "xorg-a", "XOrg A", "connectors")
	ctx := context.Background()
	client := workflowsv1.NewConnectorsClient(h.Conn())
	orgAParent := "organizations/" + orgA.Slug

	// The same owner bootstraps org B and a space inside it.
	op, err := apiv1.NewOrganizationsClient(h.Conn()).CreateOrganization(ctx, &apiv1.CreateOrganizationRequest{
		OrganizationId: "xorg-b",
		Organization:   &apiv1.Organization{DisplayName: "XOrg B"},
	})
	require.NoError(t, err)
	require.True(t, op.GetDone())
	spaceB := h.SeedOwnedSpace(t, "xorg-b", "team-b", "Team B")

	// A well-formed org-direct connector in org A — the control that MUST list.
	good := mkConnector(t, ctx, client, orgAParent, "good", "Good Conn")

	// The anomaly: a connector row with org_id = org A but space_id = a space
	// owned by org B. Inserted directly (the API's scope resolution would reject
	// a cross-org space), which is the only way this state can arise.
	anomalyID, err := uuid.NewV7()
	require.NoError(t, err)
	_, err = h.Queries.CreateConnector(ctx, db.CreateConnectorParams{
		ID:          anomalyID,
		OrgID:       orgA.Row.ID,
		SpaceID:     pgtype.UUID{Bytes: spaceB.Row.ID, Valid: true},
		Slug:        "orphan",
		DisplayName: "Cross-Org Orphan",
		Config:      json.RawMessage("{}"),
		Annotations: json.RawMessage("{}"),
		CreatedBy:   pgtype.UUID{Bytes: orgA.Owner.IdentityID, Valid: true},
	})
	require.NoError(t, err)

	// Org A's rollup returns the good row and OMITS the cross-org anomaly — it is
	// absent, NOT present under a bare org-direct name.
	got := drainAll(t, ctx, client, &workflowsv1.ListConnectorsRequest{Parent: orgAParent})
	assert.Equal(t, []string{good.GetName()}, got,
		"the rollup omits the cross-org space row rather than mis-naming it as org-direct")
	assert.NotContains(t, got, orgAParent+"/connectors/orphan",
		"the anomalous row must NOT appear with a mis-addressable org-direct name")
}

// namesOf returns the resource names of a connector slice, in order.
func namesOf(cs []*workflowsv1.Connector) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.GetName())
	}
	return out
}
