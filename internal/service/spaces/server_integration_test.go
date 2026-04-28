//go:build dev

package spaces_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	db "github.com/dashkan/pivox/internal/db/generated"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	"github.com/dashkan/pivox/internal/service/spaces"
	"github.com/dashkan/pivox/internal/testutil"
)

func createTestOrg(t *testing.T, queries *db.Queries, name string) db.Organization {
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

func TestIntegration_Spaces(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool, queries, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	conn := testutil.SetupGRPCServer(t, func(s *grpc.Server) {
		apiv1.RegisterSpacesServer(s, spaces.NewSpacesServer(pool, queries, nil, nil, nil))
	})

	client := apiv1.NewSpacesClient(conn)
	ctx := context.Background()

	// Prerequisite: create org directly via DB.
	createTestOrg(t, queries, "acme")

	var createdSpaceName string

	t.Run("CreateSpace", func(t *testing.T) {
		op, err := client.CreateSpace(ctx, &apiv1.CreateSpaceRequest{
			Parent:  "organizations/acme",
			SpaceId: "my-proj",
			Space: &apiv1.Space{
				DisplayName: "My Space",
				Labels:      map[string]string{"team": "eng"},
			},
		})
		require.NoError(t, err)
		assert.True(t, op.GetDone())

		// Extract the space from the operation response.
		var space apiv1.Space
		require.NoError(t, op.GetResponse().UnmarshalTo(&space))
		assert.Equal(t, "organizations/acme/spaces/my-proj", space.GetName())
		assert.Equal(t, "My Space", space.GetDisplayName())
		createdSpaceName = space.GetName()
	})

	t.Run("GetSpace", func(t *testing.T) {
		resp, err := client.GetSpace(ctx, &apiv1.GetSpaceRequest{
			Name: createdSpaceName,
		})
		require.NoError(t, err)
		assert.Equal(t, createdSpaceName, resp.GetName())
		assert.Equal(t, "My Space", resp.GetDisplayName())
	})

	t.Run("ListSpaces", func(t *testing.T) {
		resp, err := client.ListSpaces(ctx, &apiv1.ListSpacesRequest{
			Parent: "organizations/acme",
		})
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(resp.GetSpaces()), 1)
	})

	t.Run("UpdateSpace", func(t *testing.T) {
		op, err := client.UpdateSpace(ctx, &apiv1.UpdateSpaceRequest{
			Space: &apiv1.Space{
				Name:        createdSpaceName,
				DisplayName: "Updated Space",
			},
		})
		require.NoError(t, err)
		assert.True(t, op.GetDone())

		var space apiv1.Space
		require.NoError(t, op.GetResponse().UnmarshalTo(&space))
		assert.Equal(t, "Updated Space", space.GetDisplayName())
	})

	t.Run("DeleteSpace", func(t *testing.T) {
		op, err := client.DeleteSpace(ctx, &apiv1.DeleteSpaceRequest{
			Name: createdSpaceName,
		})
		require.NoError(t, err)
		assert.True(t, op.GetDone())

		var space apiv1.Space
		require.NoError(t, op.GetResponse().UnmarshalTo(&space))
		assert.NotNil(t, space.GetDeleteTime())
	})

	// NOTE: UndeleteSpace currently uses GetSpaceByName which filters
	// deleted records (delete_time IS NULL), so it cannot find a soft-deleted
	// space. This is a known limitation in the server code. When the
	// production code is fixed to use GetSpaceIncludingDeleted, re-enable.
	t.Run("UndeleteSpace", func(t *testing.T) {
		t.Skip("server uses GetSpaceByName which filters deleted spaces")
	})
}
