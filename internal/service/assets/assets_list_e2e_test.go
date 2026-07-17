package assets_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	assetsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/assets/v1"
)

// mkAsset creates a placeholder asset with the given display name and returns
// its resource name.
func mkAsset(t *testing.T, ctx context.Context, client assetsv1.AssetsClient, parent, displayName string) string {
	t.Helper()
	op, err := client.CreateAsset(ctx, &assetsv1.CreateAssetRequest{
		Parent: parent,
		Asset:  &assetsv1.Asset{DisplayName: displayName},
	})
	require.NoError(t, err)
	require.True(t, op.GetDone())
	var a assetsv1.Asset
	require.NoError(t, op.GetResponse().UnmarshalTo(&a))
	return a.GetName()
}

// drainAssets follows page tokens to completion, returning every asset resource
// name across all pages, and fails if the page loop runs away (the
// pre-migration bug: a never-decoded token re-serves page 1 forever).
func drainAssets(t *testing.T, ctx context.Context, client assetsv1.AssetsClient, req *assetsv1.ListAssetsRequest) []string {
	t.Helper()
	var names []string
	token := ""
	for i := 0; i < 100; i++ {
		req.PageToken = token
		resp, err := client.ListAssets(ctx, req)
		require.NoError(t, err)
		for _, a := range resp.GetAssets() {
			names = append(names, a.GetName())
		}
		token = resp.GetNextPageToken()
		if token == "" {
			return names
		}
	}
	t.Fatal("pagination did not terminate within 100 pages")
	return nil
}

// drainAssetDisplayNames drains all pages and returns display names in page order.
func drainAssetDisplayNames(t *testing.T, ctx context.Context, client assetsv1.AssetsClient, req *assetsv1.ListAssetsRequest) []string {
	t.Helper()
	var out []string
	token := ""
	for i := 0; i < 100; i++ {
		req.PageToken = token
		resp, err := client.ListAssets(ctx, req)
		require.NoError(t, err)
		for _, a := range resp.GetAssets() {
			out = append(out, a.GetDisplayName())
		}
		token = resp.GetNextPageToken()
		if token == "" {
			return out
		}
	}
	t.Fatal("pagination did not terminate within 100 pages")
	return nil
}

// TestE2E_ListAssets_KeysetDrain_DefaultID pins the core fix under the default
// (id) order: draining 2*pageSize+1 rows via next_page_token returns each row
// exactly once AND terminates. The pre-migration handler re-served page 1
// forever (never-decoded token).
func TestE2E_ListAssets_KeysetDrain_DefaultID(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h, parent := newAssetsHarness(t, "askda", "spone")
	client := assetsv1.NewAssetsClient(h.Conn())
	ctx := context.Background()

	const pageSize = 3
	const total = 2*pageSize + 1 // 7
	for i := range total {
		mkAsset(t, ctx, client, parent, fmt.Sprintf("asset-%02d", i))
	}

	got := drainAssets(t, ctx, client, &assetsv1.ListAssetsRequest{Parent: parent, PageSize: pageSize})
	assert.Len(t, got, total, "every row returned exactly once across pages")
	uniq := map[string]struct{}{}
	for _, n := range got {
		uniq[n] = struct{}{}
	}
	assert.Len(t, uniq, total, "no duplicate rows across page boundaries (token advances)")
}

// TestE2E_ListAssets_KeysetDrain_OrderByDisplayName pins the compound-cursor
// path under a non-id text sort, created out of order.
func TestE2E_ListAssets_KeysetDrain_OrderByDisplayName(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h, parent := newAssetsHarness(t, "askdo", "spone")
	client := assetsv1.NewAssetsClient(h.Conn())
	ctx := context.Background()

	names := []string{"gg", "aa", "ee", "cc", "bb", "ff", "dd"}
	for _, n := range names {
		mkAsset(t, ctx, client, parent, n)
	}

	got := drainAssetDisplayNames(t, ctx, client, &assetsv1.ListAssetsRequest{
		Parent: parent, OrderBy: "displayName", PageSize: 3,
	})
	assert.Equal(t, []string{"aa", "bb", "cc", "dd", "ee", "ff", "gg"}, got,
		"compound (displayName,id) cursor covers every row once, in sorted order")
}

// TestE2E_ListAssets_KeysetDrain_OrderBySizeBytes pins the integer keyset
// boundary: size_bytes is a BIGINT sort column, so the compound cursor must
// resume on an int64 (not a text literal, which pgx rejects against a bigint).
// Distinct sizes are set directly since CreateAsset makes placeholders (size 0).
func TestE2E_ListAssets_KeysetDrain_OrderBySizeBytes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h, parent := newAssetsHarness(t, "askds", "spone")
	client := assetsv1.NewAssetsClient(h.Conn())
	ctx := context.Background()

	// display name → size_bytes, deliberately not monotonic in creation order.
	sizes := map[string]int64{"a": 500, "b": 10, "c": 900, "d": 250, "e": 10, "f": 1000, "g": 42}
	order := []string{"a", "b", "c", "d", "e", "f", "g"}
	for _, n := range order {
		name := mkAsset(t, ctx, client, parent, n)
		leaf := name[strings.LastIndex(name, "/")+1:]
		_, err := h.Pool.Exec(ctx, "UPDATE assets SET size_bytes = $1 WHERE name = $2", sizes[n], leaf)
		require.NoError(t, err)
	}

	got := drainAssetDisplayNames(t, ctx, client, &assetsv1.ListAssetsRequest{
		Parent: parent, OrderBy: "sizeBytes", PageSize: 2,
	})
	// Ascending by size; "b" and "e" share size 10 so the id tiebreaker orders
	// them, but both must appear exactly once — assert the set membership at each
	// size tier rather than a brittle tie order.
	require.Len(t, got, 7, "every row covered once under the bigint keyset")
	assert.Equal(t, "f", got[6], "largest (1000) sorts last")
	assert.Equal(t, "c", got[5], "second-largest (900)")
	assert.Equal(t, "a", got[4], "third (500)")
	assert.Equal(t, "d", got[3], "fourth (250)")
	assert.Equal(t, "g", got[2], "fifth (42)")
	assert.ElementsMatch(t, []string{"b", "e"}, got[0:2], "the two size-10 rows share the first tier")
}

// TestE2E_ListAssets_ShowDeletedDrain pins the soft-delete branch through the
// engine: without show_deleted the drain omits soft-deleted rows; with it, all
// rows are covered — across page boundaries.
func TestE2E_ListAssets_ShowDeletedDrain(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h, parent := newAssetsHarness(t, "assdl", "spone")
	client := assetsv1.NewAssetsClient(h.Conn())
	ctx := context.Background()

	const total = 7
	names := make([]string, 0, total)
	for i := range total {
		names = append(names, mkAsset(t, ctx, client, parent, fmt.Sprintf("a-%02d", i)))
	}
	// Soft-delete 2 of them.
	for _, n := range names[:2] {
		_, err := client.DeleteAsset(ctx, &assetsv1.DeleteAssetRequest{Name: n})
		require.NoError(t, err)
	}

	// Default: soft-deleted rows excluded.
	live := drainAssets(t, ctx, client, &assetsv1.ListAssetsRequest{Parent: parent, PageSize: 2})
	assert.Len(t, live, total-2, "default drain omits soft-deleted rows")

	// show_deleted: all rows, each once, across pages.
	all := drainAssets(t, ctx, client, &assetsv1.ListAssetsRequest{Parent: parent, PageSize: 2, ShowDeleted: true})
	assert.Len(t, all, total, "show_deleted drain covers every row")
	uniq := map[string]struct{}{}
	for _, n := range all {
		uniq[n] = struct{}{}
	}
	assert.Len(t, uniq, total, "no duplicates across page boundaries under show_deleted")
}

// TestE2E_ListAssets_Filter pins AIP-160 filtering + scope isolation basics.
func TestE2E_ListAssets_Filter(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h, parent := newAssetsHarness(t, "asflt", "spone")
	client := assetsv1.NewAssetsClient(h.Conn())
	ctx := context.Background()

	mkAsset(t, ctx, client, parent, "Hero Image")
	mkAsset(t, ctx, client, parent, "Hero Banner")
	mkAsset(t, ctx, client, parent, "Footer Logo")

	resp, err := client.ListAssets(ctx, &assetsv1.ListAssetsRequest{Parent: parent, Filter: `displayName : "Hero"`})
	require.NoError(t, err)
	assert.Len(t, resp.GetAssets(), 2)

	// Injection payload is an inert literal — matches nothing, errors nothing.
	resp, err = client.ListAssets(ctx, &assetsv1.ListAssetsRequest{Parent: parent, Filter: `displayName = "x' OR '1'='1"`})
	require.NoError(t, err)
	assert.Empty(t, resp.GetAssets())
}

// TestE2E_ListAssets_Rejections pins InvalidArgument on bad inputs.
func TestE2E_ListAssets_Rejections(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h, parent := newAssetsHarness(t, "asrej", "spone")
	client := assetsv1.NewAssetsClient(h.Conn())
	ctx := context.Background()
	mkAsset(t, ctx, client, parent, "a")

	_, err := client.ListAssets(ctx, &assetsv1.ListAssetsRequest{Parent: parent, Filter: `secret = "x"`})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))

	// expire_time is nullable → filterable-only, not sortable.
	_, err = client.ListAssets(ctx, &assetsv1.ListAssetsRequest{Parent: parent, OrderBy: "expireTime"})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))

	_, err = client.ListAssets(ctx, &assetsv1.ListAssetsRequest{Parent: parent, PageToken: "not-a-token"})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}
