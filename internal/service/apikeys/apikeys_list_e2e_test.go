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
