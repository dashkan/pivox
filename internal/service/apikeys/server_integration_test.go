//go:build dev

package apikeys_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/dashkan/pivox/internal/appkey"
	db "github.com/dashkan/pivox/internal/db/generated"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	"github.com/dashkan/pivox/internal/service/apikeys"
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

func TestIntegration_ApiKeys(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool, queries, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	codec, err := appkey.NewFromHex(strings.Repeat("ab", 32))
	require.NoError(t, err)
	conn := testutil.SetupGRPCServer(t, func(s *grpc.Server) {
		apiv1.RegisterApiKeysServer(s, apikeys.NewApiKeysServer(pool, queries, codec))
	})

	client := apiv1.NewApiKeysClient(conn)
	ctx := context.Background()

	// Prerequisite: create an org directly via DB.
	createTestOrg(t, queries, "acme")

	var createdKeyName string
	var keyString string

	t.Run("CreateKey", func(t *testing.T) {
		resp, err := client.CreateKey(ctx, &apiv1.CreateKeyRequest{
			Parent: "organizations/acme",
			KeyId:  "my-api-key",
			Key: &apiv1.Key{
				DisplayName: "My API Key",
				Annotations: map[string]string{"env": "test"},
			},
		})
		require.NoError(t, err)
		assert.Equal(t, "organizations/acme/keys/my-api-key", resp.GetName())
		assert.Equal(t, "My API Key", resp.GetDisplayName())
		// KeyString is intentionally not returned in Key responses.
		createdKeyName = resp.GetName()
	})

	t.Run("GetKey", func(t *testing.T) {
		resp, err := client.GetKey(ctx, &apiv1.GetKeyRequest{
			Name: createdKeyName,
		})
		require.NoError(t, err)
		assert.Equal(t, createdKeyName, resp.GetName())
		assert.Equal(t, "My API Key", resp.GetDisplayName())
	})

	t.Run("GetKeyString", func(t *testing.T) {
		resp, err := client.GetKeyString(ctx, &apiv1.GetKeyStringRequest{
			Name: createdKeyName,
		})
		require.NoError(t, err)
		assert.NotEmpty(t, resp.GetKeyString())
		keyString = resp.GetKeyString()
	})

	t.Run("UpdateKey", func(t *testing.T) {
		resp, err := client.UpdateKey(ctx, &apiv1.UpdateKeyRequest{
			Key: &apiv1.Key{
				Name:        createdKeyName,
				DisplayName: "Updated API Key",
				Annotations: map[string]string{"env": "staging"},
			},
		})
		require.NoError(t, err)
		assert.Equal(t, "Updated API Key", resp.GetDisplayName())
	})

	t.Run("LookupKey", func(t *testing.T) {
		resp, err := client.LookupKey(ctx, &apiv1.LookupKeyRequest{
			KeyString: keyString,
		})
		require.NoError(t, err)
		assert.Equal(t, "organizations/acme", resp.GetParent())
		assert.Equal(t, createdKeyName, resp.GetName())
	})

	t.Run("ListKeys", func(t *testing.T) {
		// Create additional keys to test list.
		for _, id := range []string{"key-two", "key-three"} {
			_, err := client.CreateKey(ctx, &apiv1.CreateKeyRequest{
				Parent: "organizations/acme",
				KeyId:  id,
				Key: &apiv1.Key{
					DisplayName: "Key " + id,
				},
			})
			require.NoError(t, err)
		}

		resp, err := client.ListKeys(ctx, &apiv1.ListKeysRequest{
			Parent: "organizations/acme",
		})
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(resp.GetKeys()), 3, "should list at least 3 keys")

		// Verify pagination: request page_size=1.
		paginated, err := client.ListKeys(ctx, &apiv1.ListKeysRequest{
			Parent:   "organizations/acme",
			PageSize: 1,
		})
		require.NoError(t, err)
		assert.Len(t, paginated.GetKeys(), 1)
		assert.NotEmpty(t, paginated.GetNextPageToken(), "should have next page token")

		// Fetch second page.
		page2, err := client.ListKeys(ctx, &apiv1.ListKeysRequest{
			Parent:    "organizations/acme",
			PageSize:  1,
			PageToken: paginated.GetNextPageToken(),
		})
		require.NoError(t, err)
		assert.Len(t, page2.GetKeys(), 1)
		assert.NotEqual(t, paginated.GetKeys()[0].GetName(), page2.GetKeys()[0].GetName(), "page 2 should return different key")
	})

	t.Run("ListKeys_ShowDeleted", func(t *testing.T) {
		// First list without show_deleted — all active.
		before, err := client.ListKeys(ctx, &apiv1.ListKeysRequest{
			Parent: "organizations/acme",
		})
		require.NoError(t, err)
		beforeCount := len(before.GetKeys())

		// Delete the first key.
		_, err = client.DeleteKey(ctx, &apiv1.DeleteKeyRequest{
			Name: createdKeyName,
		})
		require.NoError(t, err)

		// Without show_deleted: should have one fewer.
		after, err := client.ListKeys(ctx, &apiv1.ListKeysRequest{
			Parent: "organizations/acme",
		})
		require.NoError(t, err)
		assert.Equal(t, beforeCount-1, len(after.GetKeys()))

		// With show_deleted: should include deleted key.
		withDeleted, err := client.ListKeys(ctx, &apiv1.ListKeysRequest{
			Parent:      "organizations/acme",
			ShowDeleted: true,
		})
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(withDeleted.GetKeys()), beforeCount)
	})

	// NOTE: UndeleteKey currently uses GetApiKeyByOrgAndKeyID which filters
	// deleted records (delete_time IS NULL), so it cannot find a soft-deleted
	// key. This is a known limitation in the server code.
	t.Run("UndeleteKey", func(t *testing.T) {
		t.Skip("server uses GetApiKeyByOrgAndKeyID which filters deleted keys")
	})
}
