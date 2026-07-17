package apikeys_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
)

// drainKeyNames follows next_page_token to completion, returning every key's
// resource name across all pages. Fails if the page loop runs away.
func drainKeyNames(t *testing.T, ctx context.Context, client apiv1.ApiKeysClient, req *apiv1.ListKeysRequest) []string {
	t.Helper()
	var names []string
	token := ""
	for range 100 {
		req.PageToken = token
		resp, err := client.ListKeys(ctx, req)
		require.NoError(t, err)
		for _, k := range resp.GetKeys() {
			names = append(names, k.GetName())
		}
		token = resp.GetNextPageToken()
		if token == "" {
			return names
		}
	}
	t.Fatal("pagination did not terminate within 100 pages")
	return nil
}

// drainKeyDisplayNames follows next_page_token to completion, returning every
// key's display name across all pages, in page order. Because each page is
// returned in sort order and pages are contiguous, the concatenation is the
// globally sorted sequence.
func drainKeyDisplayNames(t *testing.T, ctx context.Context, client apiv1.ApiKeysClient, req *apiv1.ListKeysRequest) []string {
	t.Helper()
	var names []string
	token := ""
	for range 100 {
		req.PageToken = token
		resp, err := client.ListKeys(ctx, req)
		require.NoError(t, err)
		for _, k := range resp.GetKeys() {
			names = append(names, k.GetDisplayName())
		}
		token = resp.GetNextPageToken()
		if token == "" {
			return names
		}
	}
	t.Fatal("pagination did not terminate within 100 pages")
	return nil
}

// TestE2E_ListKeys_OrderByDisplayName pins that a custom sort actually sorts:
// keys created out of alphabetical order come back ordered by display_name,
// ascending and descending. This is the surface the legacy id-only path could
// not keyset correctly (see the boundary test below).
func TestE2E_ListKeys_OrderByDisplayName(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithApiKeysServer())
	owned := h.SeedOwnedOrg(t, "keys-order", "Keys Order", "apikeys")
	ctx := context.Background()
	client := apiv1.NewApiKeysClient(h.Conn())
	parent := "organizations/" + owned.Slug

	// Create in an order that is NOT alphabetical, so sorting by displayName
	// differs from the id (creation) order.
	for i, dn := range []string{"charlie", "alpha", "bravo"} {
		_, err := client.CreateKey(ctx, &apiv1.CreateKeyRequest{
			Parent: parent,
			KeyId:  fmt.Sprintf("k%d", i),
			Key:    &apiv1.Key{DisplayName: dn},
		})
		require.NoError(t, err)
	}

	got := drainKeyDisplayNames(t, ctx, client, &apiv1.ListKeysRequest{
		Parent: parent, OrderBy: "displayName",
	})
	assert.Equal(t, []string{"alpha", "bravo", "charlie"}, got)

	got = drainKeyDisplayNames(t, ctx, client, &apiv1.ListKeysRequest{
		Parent: parent, OrderBy: "displayName desc",
	})
	assert.Equal(t, []string{"charlie", "bravo", "alpha"}, got)
}

// TestE2E_ListKeys_OrderByDisplayNameKeysetBoundary is THE test this migration
// exists for. It seeds pageSize+1 keys whose display_name order is the REVERSE
// of their id (creation) order, then paginates with order_by=displayName at a
// page size that forces boundary crossings, and asserts every key is returned
// exactly once, in sorted order.
//
// Why it fails on the legacy id-only-cursor path (filter.Query): that path
// emits `ORDER BY display_name` but resumes with `id > $cursor`, encoding only
// the last row's id. When display_name order disagrees with id order, page 2's
// `id > lastId` filter re-includes rows already shown and skips rows not yet
// shown — rows drop and duplicate across the boundary. The compound
// (display_name, id) cursor via PlanOrderBy/EncodeCursor/DecodeCursor keeps the
// resume predicate aligned with the ORDER BY, so no row drops or repeats.
func TestE2E_ListKeys_OrderByDisplayNameKeysetBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithApiKeysServer())
	owned := h.SeedOwnedOrg(t, "keys-obkb", "Keys OBKB", "apikeys")
	ctx := context.Background()
	client := apiv1.NewApiKeysClient(h.Conn())
	parent := "organizations/" + owned.Slug

	const pageSize = 3
	// display_name descending as created → ascending is the reverse of id order.
	displayNames := []string{"gg", "ff", "ee", "dd", "cc", "bb", "aa"}
	require.Greater(t, len(displayNames), pageSize, "must cross at least one page boundary")
	for i, dn := range displayNames {
		_, err := client.CreateKey(ctx, &apiv1.CreateKeyRequest{
			Parent: parent,
			KeyId:  fmt.Sprintf("k%d", i),
			Key:    &apiv1.Key{DisplayName: dn},
		})
		require.NoError(t, err)
	}

	got := drainKeyDisplayNames(t, ctx, client, &apiv1.ListKeysRequest{
		Parent: parent, OrderBy: "displayName", PageSize: pageSize,
	})
	assert.Equal(t, []string{"aa", "bb", "cc", "dd", "ee", "ff", "gg"}, got,
		"compound cursor returns every key once, in display_name order, across boundaries")

	uniq := map[string]struct{}{}
	for _, n := range got {
		uniq[n] = struct{}{}
	}
	assert.Len(t, uniq, len(displayNames), "no key dropped or duplicated across the order_by boundary")
}

// TestE2E_ListKeys_OrderByDuplicateKeysKeysetBoundary stresses the id
// tiebreaker: many keys share the SAME display name, so the compound cursor
// must fall through to id to avoid dropping or repeating rows at a boundary.
func TestE2E_ListKeys_OrderByDuplicateKeysKeysetBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithApiKeysServer())
	owned := h.SeedOwnedOrg(t, "keys-dupkey", "Keys DupKey", "apikeys")
	ctx := context.Background()
	client := apiv1.NewApiKeysClient(h.Conn())
	parent := "organizations/" + owned.Slug

	const n = 8
	for i := range n {
		_, err := client.CreateKey(ctx, &apiv1.CreateKeyRequest{
			Parent: parent,
			KeyId:  fmt.Sprintf("k%d", i),
			Key:    &apiv1.Key{DisplayName: "same-name"},
		})
		require.NoError(t, err)
	}

	got := drainKeyNames(t, ctx, client, &apiv1.ListKeysRequest{
		Parent: parent, OrderBy: "displayName", PageSize: 3,
	})
	assert.Len(t, got, n, "all rows with identical sort keys are covered")
	uniq := map[string]struct{}{}
	for _, name := range got {
		uniq[name] = struct{}{}
	}
	assert.Len(t, uniq, n, "id tiebreaker prevents dupes across boundaries")
}

// TestE2E_ListKeys_KeysetBoundary pins the keyset off-by-one: with exactly
// pageSize+1 keys and a page size that forces one boundary crossing, every key
// must be returned exactly once — no row dropped at the boundary, none
// duplicated. This fails against the old rows[pageSize] cursor (which encodes
// the first UN-returned row and then resumes strictly past it, skipping it) and
// passes once the cursor is the last RETURNED row via filter.Paginate. No
// order_by is passed, so the list uses the default id keyset (CursorColumn=id),
// isolating the off-by-one from the separate order_by/cursor-column mismatch.
func TestE2E_ListKeys_KeysetBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithApiKeysServer())
	owned := h.SeedOwnedOrg(t, "keys-page", "Keys Page", "apikeys")
	ctx := context.Background()
	client := apiv1.NewApiKeysClient(h.Conn())
	parent := "organizations/" + owned.Slug

	const pageSize = 3
	const total = pageSize + 1 // exactly one boundary crossing
	for i := range total {
		_, err := client.CreateKey(ctx, &apiv1.CreateKeyRequest{
			Parent: parent,
			KeyId:  fmt.Sprintf("k%d", i),
			Key:    &apiv1.Key{DisplayName: fmt.Sprintf("Key %d", i)},
		})
		require.NoError(t, err)
	}

	got := drainKeyNames(t, ctx, client, &apiv1.ListKeysRequest{
		Parent:   parent,
		PageSize: pageSize,
	})
	assert.Len(t, got, total, "every key returned exactly once across the page boundary (no drop)")
	uniq := map[string]struct{}{}
	for _, n := range got {
		uniq[n] = struct{}{}
	}
	assert.Len(t, uniq, total, "no duplicate keys across the page boundary")
}
