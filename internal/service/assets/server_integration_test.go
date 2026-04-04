//go:build dev

package assets_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	db "github.com/dashkan/pivox/internal/db/generated"
	assetsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/assets/v1"
	"github.com/dashkan/pivox/internal/service/assets"
	"github.com/dashkan/pivox/internal/testutil"
)

func createIntegrationOrg(t *testing.T, queries *db.Queries, name string) db.Organization {
	t.Helper()
	org, err := queries.CreateOrganization(context.Background(), db.CreateOrganizationParams{
		ID:          uuid.New(),
		Name:        name,
		DisplayName: "Test Org " + name,
		CreatedBy:   "test",
	})
	require.NoError(t, err)
	return org
}

func createIntegrationProject(t *testing.T, queries *db.Queries, orgID uuid.UUID, name string) db.Project {
	t.Helper()
	project, err := queries.CreateProject(context.Background(), db.CreateProjectParams{
		ID:          uuid.New(),
		OrgID:       orgID,
		Name:        name,
		DisplayName: "Test Project " + name,
		Labels:      json.RawMessage("{}"),
		CreatedBy:   "test",
	})
	require.NoError(t, err)
	return project
}

func TestIntegration_Assets_PlaceholderLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool, queries, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	conn := testutil.SetupGRPCServer(t, func(s *grpc.Server) {
		assetsv1.RegisterAssetsServer(s, assets.NewAssetsServer(pool, queries))
	})

	client := assetsv1.NewAssetsClient(conn)
	ctx := context.Background()

	// Prerequisite: create org and project.
	org := createIntegrationOrg(t, queries, "acme")
	createIntegrationProject(t, queries, org.ID, "proj1")

	parent := "organizations/acme/projects/proj1"
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

func TestIntegration_Assets_WithFile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool, queries, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	conn := testutil.SetupGRPCServer(t, func(s *grpc.Server) {
		assetsv1.RegisterAssetsServer(s, assets.NewAssetsServer(pool, queries))
	})

	client := assetsv1.NewAssetsClient(conn)
	ctx := context.Background()

	org := createIntegrationOrg(t, queries, "acme")
	createIntegrationProject(t, queries, org.ID, "proj1")

	parent := "organizations/acme/projects/proj1"

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
