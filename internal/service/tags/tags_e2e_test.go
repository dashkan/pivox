package tags_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/dashkan/pivox/internal/appkey"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	"github.com/dashkan/pivox/internal/service/tags"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
)

// newTagsHarness wires Organizations + TagKeys/TagValues/TagBindings and
// seeds an owned org so the caller passes the membership + permission
// interceptors.
func newTagsHarness(t *testing.T, orgSlug string) (*grpcharness.Harness, grpcharness.OwnedOrg) {
	t.Helper()
	h := grpcharness.New(t,
		grpcharness.WithOrganizationsServer(),
		grpcharness.WithServices(func(h *grpcharness.Harness, s *grpc.Server) {
			codec, err := appkey.NewFromHex(strings.Repeat("ab", 32))
			require.NoError(t, err)
			apiv1.RegisterTagKeysServer(s, tags.NewTagKeysServer(tags.TagKeysConfig{
				Pool: h.Pool, Queries: h.Queries, Codec: codec,
			}))
			apiv1.RegisterTagValuesServer(s, tags.NewTagValuesServer(tags.TagValuesConfig{
				Pool: h.Pool, Queries: h.Queries, Codec: codec,
			}))
			apiv1.RegisterTagBindingsServer(s, tags.NewTagBindingsServer(tags.TagBindingsConfig{
				Pool: h.Pool, Queries: h.Queries, Codec: codec,
			}))
		}))
	owned := h.SeedOwnedOrg(t, orgSlug, "Tags Co", "tags")
	return h, owned
}

// TestE2E_TagKey_ValidateOnly pins the AIP validate_only contract for the
// TagKeys create path: a dry-run runs the same validation a live request
// would (so a would-fail request still fails) but persists nothing.
func TestE2E_TagKey_ValidateOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h, owned := newTagsHarness(t, "tags-vo")
	ctx := context.Background()
	client := apiv1.NewTagKeysClient(h.Conn())
	parent := "organizations/" + owned.Slug

	// A dry-run Create returns the would-be resource but writes nothing.
	dry, err := client.CreateTagKey(ctx, &apiv1.CreateTagKeyRequest{
		Parent:       parent,
		TagKeyId:     "env",
		TagKey:       &apiv1.TagKey{Description: "Dry"},
		ValidateOnly: true,
	})
	require.NoError(t, err)
	require.True(t, dry.GetDone())

	// Nothing persisted → a real Create can reuse the same short name.
	_, err = client.CreateTagKey(ctx, &apiv1.CreateTagKeyRequest{
		Parent:   parent,
		TagKeyId: "env",
		TagKey:   &apiv1.TagKey{Description: "Real"},
	})
	require.NoError(t, err, "validate_only must not have persisted the tag key")

	// A dry-run that WOULD fail live (duplicate short name now exists) fails.
	_, err = client.CreateTagKey(ctx, &apiv1.CreateTagKeyRequest{
		Parent:       parent,
		TagKeyId:     "env",
		TagKey:       &apiv1.TagKey{Description: "Dup"},
		ValidateOnly: true,
	})
	require.Error(t, err)
	assert.Equal(t, codes.AlreadyExists, status.Code(err),
		"validate_only must fail if the live request would")
}
