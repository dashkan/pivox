package spaces_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
)

// drainSpaceNames follows next_page_token to completion, returning every
// space's resource name across all pages. Fails if the page loop runs away.
func drainSpaceNames(t *testing.T, ctx context.Context, client apiv1.SpacesClient, req *apiv1.ListSpacesRequest) []string {
	t.Helper()
	var names []string
	token := ""
	for range 100 {
		req.PageToken = token
		resp, err := client.ListSpaces(ctx, req)
		require.NoError(t, err)
		for _, s := range resp.GetSpaces() {
			names = append(names, s.GetName())
		}
		token = resp.GetNextPageToken()
		if token == "" {
			return names
		}
	}
	t.Fatal("pagination did not terminate within 100 pages")
	return nil
}

// drainSpaceDisplayNames follows next_page_token to completion, returning every
// space's display name across all pages, in page order. Because each page is
// returned in sort order and pages are contiguous, the concatenation is the
// globally sorted sequence.
func drainSpaceDisplayNames(t *testing.T, ctx context.Context, client apiv1.SpacesClient, req *apiv1.ListSpacesRequest) []string {
	t.Helper()
	var names []string
	token := ""
	for range 100 {
		req.PageToken = token
		resp, err := client.ListSpaces(ctx, req)
		require.NoError(t, err)
		for _, s := range resp.GetSpaces() {
			names = append(names, s.GetDisplayName())
		}
		token = resp.GetNextPageToken()
		if token == "" {
			return names
		}
	}
	t.Fatal("pagination did not terminate within 100 pages")
	return nil
}

// TestE2E_ListSpaces_OrderByDisplayName pins that a custom sort actually sorts:
// spaces created out of alphabetical order come back ordered by display_name,
// ascending and descending. This is the surface the legacy id-only path could
// not keyset correctly (see the boundary test below).
func TestE2E_ListSpaces_OrderByDisplayName(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithSpacesServer())
	owned := h.SeedOwnedOrg(t, "sp-order", "Sp Order", "spaces")
	ctx := context.Background()
	client := apiv1.NewSpacesClient(h.Conn())
	parent := "organizations/" + owned.Slug

	// Create in an order that is NOT alphabetical, so sorting by displayName
	// differs from the id (creation) order.
	h.SeedOwnedSpace(t, owned.Slug, "s1", "charlie")
	h.SeedOwnedSpace(t, owned.Slug, "s2", "alpha")
	h.SeedOwnedSpace(t, owned.Slug, "s3", "bravo")

	got := drainSpaceDisplayNames(t, ctx, client, &apiv1.ListSpacesRequest{
		Parent: parent, OrderBy: "displayName",
	})
	assert.Equal(t, []string{"alpha", "bravo", "charlie"}, got)

	got = drainSpaceDisplayNames(t, ctx, client, &apiv1.ListSpacesRequest{
		Parent: parent, OrderBy: "displayName desc",
	})
	assert.Equal(t, []string{"charlie", "bravo", "alpha"}, got)
}

// TestE2E_ListSpaces_OrderByDisplayNameKeysetBoundary is THE test this migration
// exists for. It seeds pageSize+1 spaces whose display_name order is the REVERSE
// of their id (creation) order, then paginates with order_by=displayName at a
// page size that forces boundary crossings, and asserts every space is returned
// exactly once, in sorted order.
//
// Why it fails on the legacy id-only-cursor path: that path emits
// `ORDER BY display_name` but resumes with `id > $cursor`, encoding only the
// last row's id. When display_name order disagrees with id order, page 2's
// `id > lastId` filter re-includes rows already shown (whose id > lastId but
// display_name < page 1's max) and skips rows not yet shown (whose id < lastId
// but display_name > page 1's max). Concretely, with display names g..a created
// in that order (ids ascending) and pageSize=3: page 1 returns a,b,c (highest
// ids); page 2 resumes id > id(c), which yields only a,b again — d,e,f,g are
// dropped and a,b duplicated. The compound (display_name, id) cursor keeps the
// resume predicate aligned with the ORDER BY, so no row drops or repeats.
func TestE2E_ListSpaces_OrderByDisplayNameKeysetBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithSpacesServer())
	owned := h.SeedOwnedOrg(t, "sp-obkb", "Sp OBKB", "spaces")
	ctx := context.Background()
	client := apiv1.NewSpacesClient(h.Conn())
	parent := "organizations/" + owned.Slug

	const pageSize = 3
	// display_name descending as created → ascending is the reverse of id order.
	displayNames := []string{"gg", "ff", "ee", "dd", "cc", "bb", "aa"}
	require.Greater(t, len(displayNames), pageSize, "must cross at least one page boundary")
	for i, dn := range displayNames {
		h.SeedOwnedSpace(t, owned.Slug, fmt.Sprintf("sp%d", i), dn)
	}

	got := drainSpaceDisplayNames(t, ctx, client, &apiv1.ListSpacesRequest{
		Parent: parent, OrderBy: "displayName", PageSize: pageSize,
	})
	assert.Equal(t, []string{"aa", "bb", "cc", "dd", "ee", "ff", "gg"}, got,
		"compound cursor returns every space once, in display_name order, across boundaries")

	uniq := map[string]struct{}{}
	for _, n := range got {
		uniq[n] = struct{}{}
	}
	assert.Len(t, uniq, len(displayNames), "no space dropped or duplicated across the order_by boundary")
}

// TestE2E_ListSpaces_OrderByDuplicateKeysKeysetBoundary stresses the id
// tiebreaker: many spaces share the SAME display name, so the compound cursor
// must fall through to id to avoid dropping or repeating rows at a boundary.
func TestE2E_ListSpaces_OrderByDuplicateKeysKeysetBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithSpacesServer())
	owned := h.SeedOwnedOrg(t, "sp-dupkey", "Sp DupKey", "spaces")
	ctx := context.Background()
	client := apiv1.NewSpacesClient(h.Conn())
	parent := "organizations/" + owned.Slug

	const n = 8
	for i := range n {
		h.SeedOwnedSpace(t, owned.Slug, fmt.Sprintf("sp%d", i), "same-name")
	}

	got := drainSpaceNames(t, ctx, client, &apiv1.ListSpacesRequest{
		Parent: parent, OrderBy: "displayName", PageSize: 3,
	})
	assert.Len(t, got, n, "all rows with identical sort keys are covered")
	uniq := map[string]struct{}{}
	for _, name := range got {
		uniq[name] = struct{}{}
	}
	assert.Len(t, uniq, n, "id tiebreaker prevents dupes across boundaries")
}

// TestE2E_ListSpaces_KeysetBoundary pins the keyset off-by-one: with exactly
// pageSize+1 spaces and a page size that forces one boundary crossing, every
// space must be returned exactly once — no row dropped at the boundary, none
// duplicated. This fails against the old rows[pageSize] cursor (which encodes
// the first UN-returned row and then resumes strictly past it, skipping it) and
// passes once the cursor is the last RETURNED row via filter.Paginate. No
// order_by is passed, so the list uses the default id keyset (CursorColumn=id),
// isolating the off-by-one from the separate order_by/cursor-column mismatch.
func TestE2E_ListSpaces_KeysetBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithSpacesServer())
	owned := h.SeedOwnedOrg(t, "spaces-page", "Spaces Page", "spaces")
	ctx := context.Background()
	client := apiv1.NewSpacesClient(h.Conn())
	parent := "organizations/" + owned.Slug

	const pageSize = 3
	const total = pageSize + 1 // exactly one boundary crossing
	for i := range total {
		h.SeedOwnedSpace(t, owned.Slug, fmt.Sprintf("sp%d", i), fmt.Sprintf("Space %d", i))
	}

	got := drainSpaceNames(t, ctx, client, &apiv1.ListSpacesRequest{
		Parent:   parent,
		PageSize: pageSize,
	})
	assert.Len(t, got, total, "every space returned exactly once across the page boundary (no drop)")
	uniq := map[string]struct{}{}
	for _, n := range got {
		uniq[n] = struct{}{}
	}
	assert.Len(t, uniq, total, "no duplicate spaces across the page boundary")
}
