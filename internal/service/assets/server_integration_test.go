package assets_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	assetsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/assets/v1"
	"github.com/dashkan/pivox/internal/service/assets"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
)

// newAssetsHarness wires Organizations + Spaces + Assets and seeds
// an owned org+space. Each top-level test gets its own harness for
// isolation — same shape used by requests_integration_test.
func newAssetsHarness(t *testing.T, orgSlug, spaceSlug string) (*grpcharness.Harness, string) {
	t.Helper()
	h := grpcharness.New(t,
		grpcharness.WithOrganizationsServer(),
		grpcharness.WithSpacesServer(),
		grpcharness.WithServices(func(h *grpcharness.Harness, s *grpc.Server) {
			assetsv1.RegisterAssetsServer(s, assets.NewAssetsServer(assets.Config{
				Pool: h.Pool, Queries: h.Queries,
			}))
		}))
	h.SeedOwnedOrg(t, orgSlug, "Acme", "assets")
	h.SeedOwnedSpace(t, orgSlug, spaceSlug, "Project")
	return h, "organizations/" + orgSlug + "/spaces/" + spaceSlug
}

func TestIntegration_Assets_PlaceholderLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h, parent := newAssetsHarness(t, "acme", "proj1")
	client := assetsv1.NewAssetsClient(h.Conn())
	ctx := context.Background()

	var assetName string

	t.Run("CreatePlaceholder", func(t *testing.T) {
		op, err := client.CreateAsset(ctx, &assetsv1.CreateAssetRequest{
			Parent: parent,
			Asset: &assetsv1.Asset{
				DisplayName: "Logo Placeholder",
			},
		})
		require.NoError(t, err)
		assert.True(t, op.GetDone())

		var asset assetsv1.Asset
		require.NoError(t, op.GetResponse().UnmarshalTo(&asset))
		assert.Equal(t, assetsv1.Asset_PLACEHOLDER, asset.GetState())
		assert.NotEmpty(t, asset.GetName())
		assetName = asset.GetName()
	})

	t.Run("GetAsset", func(t *testing.T) {
		resp, err := client.GetAsset(ctx, &assetsv1.GetAssetRequest{
			Name: assetName,
		})
		require.NoError(t, err)
		assert.Equal(t, assetName, resp.GetName())
		assert.Equal(t, "Logo Placeholder", resp.GetDisplayName())
		assert.Equal(t, assetsv1.Asset_PLACEHOLDER, resp.GetState())
	})

	t.Run("UpdateAsset", func(t *testing.T) {
		op, err := client.UpdateAsset(ctx, &assetsv1.UpdateAssetRequest{
			Asset: &assetsv1.Asset{
				Name:        assetName,
				DisplayName: "Updated Logo",
			},
		})
		require.NoError(t, err)
		assert.True(t, op.GetDone())

		var asset assetsv1.Asset
		require.NoError(t, op.GetResponse().UnmarshalTo(&asset))
		assert.Equal(t, "Updated Logo", asset.GetDisplayName())
	})

	t.Run("DeleteAsset", func(t *testing.T) {
		op, err := client.DeleteAsset(ctx, &assetsv1.DeleteAssetRequest{
			Name: assetName,
		})
		require.NoError(t, err)
		assert.True(t, op.GetDone())

		var asset assetsv1.Asset
		require.NoError(t, op.GetResponse().UnmarshalTo(&asset))
		assert.Equal(t, assetsv1.Asset_DELETE_REQUESTED, asset.GetState())
	})

	t.Run("UndeleteAsset", func(t *testing.T) {
		op, err := client.UndeleteAsset(ctx, &assetsv1.UndeleteAssetRequest{
			Name: assetName,
		})
		require.NoError(t, err)
		assert.True(t, op.GetDone())

		var asset assetsv1.Asset
		require.NoError(t, op.GetResponse().UnmarshalTo(&asset))
		// After undelete, state should revert to non-deleted.
		assert.NotEqual(t, assetsv1.Asset_DELETE_REQUESTED, asset.GetState())
	})
}

func TestIntegration_Assets_ListAssets(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h, parent := newAssetsHarness(t, "acme", "proj1")
	client := assetsv1.NewAssetsClient(h.Conn())
	ctx := context.Background()

	// Create multiple assets.
	var assetNames []string
	for i := range 3 {
		op, err := client.CreateAsset(ctx, &assetsv1.CreateAssetRequest{
			Parent: parent,
			Asset: &assetsv1.Asset{
				DisplayName: fmt.Sprintf("Asset %d", i),
			},
		})
		require.NoError(t, err)
		var a assetsv1.Asset
		require.NoError(t, op.GetResponse().UnmarshalTo(&a))
		assetNames = append(assetNames, a.GetName())
	}

	t.Run("list_all", func(t *testing.T) {
		resp, err := client.ListAssets(ctx, &assetsv1.ListAssetsRequest{
			Parent: parent,
		})
		require.NoError(t, err)
		assert.Len(t, resp.GetAssets(), 3)
	})

	t.Run("list_with_show_deleted", func(t *testing.T) {
		// Delete one asset.
		_, err := client.DeleteAsset(ctx, &assetsv1.DeleteAssetRequest{
			Name: assetNames[0],
		})
		require.NoError(t, err)

		// Without show_deleted: should have 2.
		resp, err := client.ListAssets(ctx, &assetsv1.ListAssetsRequest{
			Parent: parent,
		})
		require.NoError(t, err)
		assert.Len(t, resp.GetAssets(), 2)

		// With show_deleted: should have 3.
		withDeleted, err := client.ListAssets(ctx, &assetsv1.ListAssetsRequest{
			Parent:      parent,
			ShowDeleted: true,
		})
		require.NoError(t, err)
		assert.Len(t, withDeleted.GetAssets(), 3)
	})

	t.Run("list_pagination", func(t *testing.T) {
		resp, err := client.ListAssets(ctx, &assetsv1.ListAssetsRequest{
			Parent:   parent,
			PageSize: 1,
		})
		require.NoError(t, err)
		assert.Len(t, resp.GetAssets(), 1)
		assert.NotEmpty(t, resp.GetNextPageToken())
	})
}

// TestIntegration_Assets_ValidateOnly pins the AIP validate_only contract
// for CreateAsset: a dry-run returns the would-be resource (with a name)
// but persists nothing, so fetching that name afterward is NotFound.
func TestIntegration_Assets_ValidateOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h, parent := newAssetsHarness(t, "acme-vo", "proj-vo")
	client := assetsv1.NewAssetsClient(h.Conn())
	ctx := context.Background()

	op, err := client.CreateAsset(ctx, &assetsv1.CreateAssetRequest{
		Parent:       parent,
		Asset:        &assetsv1.Asset{DisplayName: "Dry"},
		ValidateOnly: true,
	})
	require.NoError(t, err)
	require.True(t, op.GetDone())

	var asset assetsv1.Asset
	require.NoError(t, op.GetResponse().UnmarshalTo(&asset))
	require.NotEmpty(t, asset.GetName())

	// Nothing persisted → fetching the would-be asset is NotFound.
	_, err = client.GetAsset(ctx, &assetsv1.GetAssetRequest{Name: asset.GetName()})
	require.Error(t, err, "validate_only must not have persisted the asset")
	assert.Equal(t, codes.NotFound, status.Code(err))

	// And ListAssets shows nothing was created.
	list, err := client.ListAssets(ctx, &assetsv1.ListAssetsRequest{Parent: parent})
	require.NoError(t, err)
	assert.Empty(t, list.GetAssets(), "validate_only create must persist nothing")
}

func TestIntegration_Assets_WithFile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h, parent := newAssetsHarness(t, "acme", "proj1")
	client := assetsv1.NewAssetsClient(h.Conn())
	ctx := context.Background()

	t.Run("CreateWithFile_ProcessingToActive", func(t *testing.T) {
		op, err := client.CreateAsset(ctx, &assetsv1.CreateAssetRequest{
			Parent: parent,
			Asset: &assetsv1.Asset{
				DisplayName: "Hero Image",
			},
			Filename: "hero.png",
		})
		require.NoError(t, err)
		assert.True(t, op.GetDone())

		var asset assetsv1.Asset
		require.NoError(t, op.GetResponse().UnmarshalTo(&asset))
		// The server marks it ACTIVE after the synchronous "pipeline".
		assert.Equal(t, assetsv1.Asset_ACTIVE, asset.GetState())
	})
}
