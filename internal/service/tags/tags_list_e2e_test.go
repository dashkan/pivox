package tags_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
)

// These tests pin the keyset off-by-one across the tags List surfaces. With
// exactly pageSize+1 rows under one parent and a page size that forces a single
// boundary crossing, every row must be returned exactly once — none dropped at
// the boundary, none duplicated. They fail against the old rows[pageSize] cursor
// (which encodes the first UN-returned row and then resumes strictly past it,
// skipping it) and pass once the cursor is the last RETURNED row via
// filter.Paginate.
//
// order_by is deliberately NOT exercised: these lock the default id-ordered
// keyset. (The tags filters accept order_by on non-id columns while resuming on
// an id-only token — a distinct, deeper keyset bug that is out of scope here.)
//
// ListTagValues has no dedicated boundary test here; its create+list
// reachability is pinned in tags_reachability_e2e_test.go. These keyset-boundary
// tests seed rows directly through the DB (not the Create* RPCs) so the
// off-by-one assertion is isolated from create-path validation and needs only a
// single parent's worth of rows.

const (
	pageSize   = 3
	totalPages = pageSize + 1 // exactly one boundary crossing
)

// createTagKeyName creates a tag key under the org parent and returns its
// resource name ("tagKeys/{uuid}").
func createTagKeyName(t *testing.T, ctx context.Context, client apiv1.TagKeysClient, parent, id string) string {
	t.Helper()
	op, err := client.CreateTagKey(ctx, &apiv1.CreateTagKeyRequest{
		Parent:   parent,
		TagKeyId: id,
		TagKey:   &apiv1.TagKey{Description: "k"},
	})
	require.NoError(t, err)
	var tk apiv1.TagKey
	require.NoError(t, op.GetResponse().UnmarshalTo(&tk))
	return tk.GetName()
}

// TestE2E_ListTagKeys_KeysetBoundary pins the off-by-one on the TagKeys list,
// exercised end-to-end (create + list) through the real interceptor chain.
func TestE2E_ListTagKeys_KeysetBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h, owned := newTagsHarness(t, "tags-keys-page")
	ctx := context.Background()
	client := apiv1.NewTagKeysClient(h.Conn())
	parent := "organizations/" + owned.Slug

	for i := range totalPages {
		createTagKeyName(t, ctx, client, parent, fmt.Sprintf("k%d", i))
	}

	var names []string
	token := ""
	for range 100 {
		resp, err := client.ListTagKeys(ctx, &apiv1.ListTagKeysRequest{
			Parent:    parent,
			PageSize:  pageSize,
			PageToken: token,
		})
		require.NoError(t, err)
		for _, k := range resp.GetTagKeys() {
			names = append(names, k.GetName())
		}
		if token = resp.GetNextPageToken(); token == "" {
			break
		}
	}

	assert.Len(t, names, totalPages, "every tag key returned exactly once across the page boundary (no drop)")
	uniq := map[string]struct{}{}
	for _, n := range names {
		uniq[n] = struct{}{}
	}
	assert.Len(t, uniq, totalPages, "no duplicate tag keys across the page boundary")
}

// TestE2E_ListTagBindings_KeysetBoundary pins the off-by-one on the TagBindings
// list. All bindings share one parent_resource (the list filter key), each
// binding a distinct tag value (UNIQUE(parent_resource, tag_value_id) forbids
// re-binding the same value). Rows are seeded through the DB because the
// CreateTagValue/CreateTagBinding RPCs are unreachable (see file header); the
// ListTagBindings RPC under test still runs through the full interceptor chain.
func TestE2E_ListTagBindings_KeysetBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h, owned := newTagsHarness(t, "tags-bindings-page")
	ctx := context.Background()
	createdBy := convert.PgUUID(owned.Owner.IdentityID)

	// An org-scoped parent_resource: ScopeFromPath resolves it to OrgScope (the
	// owner has TagsRead), and the handler filters tag_bindings by this exact
	// string.
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

	for i := range totalPages {
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
			ID:             uuid.New(),
			ParentResource: parentResource,
			TagValueID:     tagValue.ID,
			CreatedBy:      createdBy,
		})
		require.NoError(t, err)
	}

	bindingsClient := apiv1.NewTagBindingsClient(h.Conn())
	var names []string
	token := ""
	for range 100 {
		resp, err := bindingsClient.ListTagBindings(ctx, &apiv1.ListTagBindingsRequest{
			Parent:    parentResource,
			PageSize:  pageSize,
			PageToken: token,
		})
		require.NoError(t, err)
		for _, b := range resp.GetTagBindings() {
			names = append(names, b.GetName())
		}
		if token = resp.GetNextPageToken(); token == "" {
			break
		}
	}

	assert.Len(t, names, totalPages, "every tag binding returned exactly once across the page boundary (no drop)")
	uniq := map[string]struct{}{}
	for _, n := range names {
		uniq[n] = struct{}{}
	}
	assert.Len(t, uniq, totalPages, "no duplicate tag bindings across the page boundary")
}
