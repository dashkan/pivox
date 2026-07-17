package tags_test

import (
	"context"
	"fmt"
	"sort"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
)

// These tests pin the compound-cursor keyset migration for the tags List
// surfaces: an order_by on a NON-id sortable column must keyset correctly across
// a page boundary. The legacy filter.Query path emitted `ORDER BY <col>` but
// resumed on an id-only token (`id > $cursor`), so when the sort order differed
// from id order rows dropped and duplicated across the boundary. The compound
// (sortCol, id) cursor keeps the resume predicate aligned with the ORDER BY.
//
// The tag protos expose only the id-based resource Name, not short_name, so the
// tests control the id↔short_name mapping (ascending short_name ↔ DESCENDING id)
// and assert on the ordered resource names: sorting by short_name ascending must
// yield the ids in descending order, which is exactly the id/sort mismatch that
// broke the legacy keyset.
//
// Reachability note (documented once here for all three handlers):
//   - ListTagKeys is org-scoped and fully reachable through the interceptor
//     chain, so its order_by boundary is driven end-to-end through the RPC.
//   - ListTagValues is org-scoped too (parent
//     `organizations/{org}/tagKeys/{key}`) and fully reachable, so its order_by
//     boundary is likewise driven end-to-end through the RPC. Rows are seeded
//     through the DB to control the id↔short_name mapping the keyset must sort by.
//   - ListTagBindings is reached with an org-scoped parent (the handler treats it
//     as an opaque parent_resource filter). Its only sortable, parentResource, is
//     constant within any single reachable list scope, so its order_by boundary
//     exercises the id tiebreaker under identical sort keys (rows seeded through
//     the DB with controlled ids).

// reverseIDNames builds n uuids and returns them in DESCENDING order — the order
// the ascending-short_name sort must produce, given each row's short_name is
// assigned in ascending order to a descending id.
func reverseIDNames(n int) []uuid.UUID {
	ids := make([]uuid.UUID, n)
	for i := range ids {
		ids[i] = uuid.New()
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() > ids[j].String() })
	return ids
}

// seedTagKeysReverseID inserts n tag keys under org whose short_name ASC order is
// the REVERSE of their id ASC order: short_name "sk0" gets the largest id, "sk{n-1}"
// the smallest. That mismatch is exactly what breaks the legacy id-only keyset
// (page 2 resumes `id > lastId`, re-including higher-id/lower-short_name rows and
// dropping the rest), and what the compound cursor fixes. Returns the expected
// resource names in short_name-ascending (= id-descending) order.
func seedTagKeysReverseID(t *testing.T, ctx context.Context, q *db.Queries, orgID, createdBy uuid.UUID, n int) []string {
	t.Helper()
	idsDesc := reverseIDNames(n)
	want := make([]string, n)
	for i := range n {
		sn := fmt.Sprintf("sk%d", i)
		id := idsDesc[i] // ascending short_name i ↔ descending id
		_, err := q.CreateTagKey(ctx, db.CreateTagKeyParams{
			ID:             id,
			OrgID:          orgID,
			ShortName:      sn,
			NamespacedName: orgID.String() + "/" + sn,
			Description:    "k",
			CreatedBy:      convert.PgUUID(createdBy),
		})
		require.NoError(t, err)
		want[i] = "tagKeys/" + id.String()
	}
	return want
}

// TestE2E_ListTagKeys_OrderByShortNameKeysetBoundary is THE test the TagKeys
// migration exists for. It seeds pageSize+1 keys whose short_name order is the
// reverse of their id order, paginates with order_by=shortName at a page size
// that forces a boundary crossing, and asserts every key is returned exactly
// once, in short_name order. It fails on the legacy id-only-cursor path (page 2
// re-includes page 1's high-id keys and drops the tail) and passes on the
// compound (short_name, id) cursor.
func TestE2E_ListTagKeys_OrderByShortNameKeysetBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h, owned := newTagsHarness(t, "tags-keys-ob")
	ctx := context.Background()
	client := apiv1.NewTagKeysClient(h.Conn())
	parent := "organizations/" + owned.Slug

	const n = 7
	want := seedTagKeysReverseID(t, ctx, h.Queries, owned.Row.ID, owned.Owner.IdentityID, n)

	var got []string
	token := ""
	for range 100 {
		resp, err := client.ListTagKeys(ctx, &apiv1.ListTagKeysRequest{
			Parent:    parent,
			OrderBy:   "shortName",
			PageSize:  3,
			PageToken: token,
		})
		require.NoError(t, err)
		for _, k := range resp.GetTagKeys() {
			got = append(got, k.GetName())
		}
		if token = resp.GetNextPageToken(); token == "" {
			break
		}
	}

	assert.Equal(t, want, got,
		"compound cursor returns every tag key once, in short_name order, across boundaries")
	uniq := map[string]struct{}{}
	for _, name := range got {
		uniq[name] = struct{}{}
	}
	assert.Len(t, uniq, n, "no tag key dropped or duplicated across the order_by boundary")
}

// seedTagValuesReverseID mirrors seedTagKeysReverseID for tag values under one
// tag key. orgSlug scopes the expected resource names. Returns the expected
// names in short_name-ascending (= id-descending) order.
func seedTagValuesReverseID(t *testing.T, ctx context.Context, q *db.Queries, tagKey db.TagKey, orgSlug string, createdBy uuid.UUID, n int) []string {
	t.Helper()
	idsDesc := reverseIDNames(n)
	want := make([]string, n)
	for i := range n {
		sn := fmt.Sprintf("sv%d", i)
		id := idsDesc[i]
		_, err := q.CreateTagValue(ctx, db.CreateTagValueParams{
			ID:             id,
			TagKeyID:       tagKey.ID,
			ShortName:      sn,
			NamespacedName: tagKey.NamespacedName + "/" + sn,
			Description:    "v",
			CreatedBy:      convert.PgUUID(createdBy),
		})
		require.NoError(t, err)
		want[i] = "organizations/" + orgSlug + "/tagKeys/" + tagKey.ID.String() + "/tagValues/" + id.String()
	}
	return want
}

// drainTagValueNames drains ListTagValues through the RPC to completion.
func drainTagValueNames(t *testing.T, ctx context.Context, client apiv1.TagValuesClient, req *apiv1.ListTagValuesRequest) []string {
	t.Helper()
	var got []string
	token := ""
	for range 100 {
		req.PageToken = token
		resp, err := client.ListTagValues(ctx, req)
		require.NoError(t, err)
		for _, v := range resp.GetTagValues() {
			got = append(got, v.GetName())
		}
		if token = resp.GetNextPageToken(); token == "" {
			return got
		}
	}
	t.Fatal("pagination did not terminate within 100 pages")
	return nil
}

// TestE2E_ListTagValues_OrderByShortNameKeysetBoundary pins the TagValues
// migration, driven end-to-end through the RPC (org-scoped parent).
func TestE2E_ListTagValues_OrderByShortNameKeysetBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h, owned := newTagsHarness(t, "tags-values-ob")
	ctx := context.Background()
	client := apiv1.NewTagValuesClient(h.Conn())

	tagKey, err := h.Queries.CreateTagKey(ctx, db.CreateTagKeyParams{
		ID:             uuid.New(),
		OrgID:          owned.Row.ID,
		ShortName:      "env",
		NamespacedName: owned.Row.ID.String() + "/env",
		Description:    "k",
		CreatedBy:      convert.PgUUID(owned.Owner.IdentityID),
	})
	require.NoError(t, err)

	const n = 7
	want := seedTagValuesReverseID(t, ctx, h.Queries, tagKey, owned.Slug, owned.Owner.IdentityID, n)

	got := drainTagValueNames(t, ctx, client, &apiv1.ListTagValuesRequest{
		Parent:   "organizations/" + owned.Slug + "/tagKeys/" + tagKey.ID.String(),
		OrderBy:  "shortName",
		PageSize: 3,
	})

	assert.Equal(t, want, got,
		"compound cursor returns every tag value once, in short_name order, across boundaries")
	uniq := map[string]struct{}{}
	for _, name := range got {
		uniq[name] = struct{}{}
	}
	assert.Len(t, uniq, n, "no tag value dropped or duplicated across the order_by boundary")
}

// TestE2E_ListTagValues_KeysetBoundary pins the default-id keyset off-by-one for
// TagValues (no order_by), driven end-to-end through the RPC.
func TestE2E_ListTagValues_KeysetBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h, owned := newTagsHarness(t, "tags-values-page")
	ctx := context.Background()
	client := apiv1.NewTagValuesClient(h.Conn())

	tagKey, err := h.Queries.CreateTagKey(ctx, db.CreateTagKeyParams{
		ID:             uuid.New(),
		OrgID:          owned.Row.ID,
		ShortName:      "env",
		NamespacedName: owned.Row.ID.String() + "/env",
		Description:    "k",
		CreatedBy:      convert.PgUUID(owned.Owner.IdentityID),
	})
	require.NoError(t, err)

	const total = pageSize + 1 // exactly one boundary crossing
	for i := range total {
		_, err := h.Queries.CreateTagValue(ctx, db.CreateTagValueParams{
			ID:             uuid.New(),
			TagKeyID:       tagKey.ID,
			ShortName:      fmt.Sprintf("v%d", i),
			NamespacedName: fmt.Sprintf("%s/env/v%d", owned.Row.ID, i),
			Description:    "v",
			CreatedBy:      convert.PgUUID(owned.Owner.IdentityID),
		})
		require.NoError(t, err)
	}

	got := drainTagValueNames(t, ctx, client, &apiv1.ListTagValuesRequest{
		Parent:   "organizations/" + owned.Slug + "/tagKeys/" + tagKey.ID.String(),
		PageSize: pageSize,
	})
	assert.Len(t, got, total, "every tag value returned exactly once across the page boundary (no drop)")
	uniq := map[string]struct{}{}
	for _, name := range got {
		uniq[name] = struct{}{}
	}
	assert.Len(t, uniq, total, "no duplicate tag values across the page boundary")
}

// TestE2E_ListTagBindings_OrderByParentResourceKeysetBoundary exercises the
// TagBindings migration under an order_by on its sole sortable, parentResource.
// Because every binding in a single reachable list shares the same
// parent_resource (the list filter key), the sort key is constant across the
// page and the compound cursor must fall through to the id tiebreaker. Rows are
// seeded with ids in descending insertion order so the legacy path — which
// emitted `ORDER BY parent_resource` with NO id tiebreaker and resumed on an
// id-only token — drops and duplicates rows across the boundary; the compound
// (parent_resource, id) cursor covers every row exactly once.
func TestE2E_ListTagBindings_OrderByParentResourceKeysetBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h, owned := newTagsHarness(t, "tags-bindings-ob")
	ctx := context.Background()
	createdBy := convert.PgUUID(owned.Owner.IdentityID)
	parentResource := "organizations/" + owned.Slug

	tagKey, err := h.Queries.CreateTagKey(ctx, db.CreateTagKeyParams{
		ID:             uuid.New(),
		OrgID:          owned.Row.ID,
		ShortName:      "env",
		NamespacedName: owned.Row.ID.String() + "/env",
		Description:    "k",
		CreatedBy:      createdBy,
	})
	require.NoError(t, err)

	const n = 7
	// Descending ids in insertion order: insertion order (the physical scan order
	// the legacy tie-free ORDER BY relies on) disagrees with id order, so the
	// id-only cursor mis-resumes.
	ids := reverseIDNames(n)

	for i := range n {
		tagValue, err := h.Queries.CreateTagValue(ctx, db.CreateTagValueParams{
			ID:             uuid.New(),
			TagKeyID:       tagKey.ID,
			ShortName:      fmt.Sprintf("v%d", i),
			NamespacedName: fmt.Sprintf("%s/env/v%d", owned.Row.ID, i),
			Description:    "v",
			CreatedBy:      createdBy,
		})
		require.NoError(t, err)
		_, err = h.Queries.CreateTagBinding(ctx, db.CreateTagBindingParams{
			ID:             ids[i],
			ParentResource: parentResource,
			TagValueID:     tagValue.ID,
			CreatedBy:      createdBy,
		})
		require.NoError(t, err)
	}

	bindingsClient := apiv1.NewTagBindingsClient(h.Conn())
	var got []string
	token := ""
	for range 100 {
		resp, err := bindingsClient.ListTagBindings(ctx, &apiv1.ListTagBindingsRequest{
			Parent:    parentResource,
			OrderBy:   "parentResource",
			PageSize:  3,
			PageToken: token,
		})
		require.NoError(t, err)
		for _, b := range resp.GetTagBindings() {
			got = append(got, b.GetName())
		}
		if token = resp.GetNextPageToken(); token == "" {
			break
		}
	}

	assert.Len(t, got, n, "every tag binding returned exactly once across the order_by boundary (no drop)")
	uniq := map[string]struct{}{}
	for _, name := range got {
		uniq[name] = struct{}{}
	}
	assert.Len(t, uniq, n, "id tiebreaker prevents dupes across the constant-sort-key boundary")
}
