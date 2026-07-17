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
