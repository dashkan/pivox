//go:build dev

package organizations_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/dashkan/pivox/internal/iam"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	iampb "github.com/dashkan/pivox/internal/pkg/gen/pivox/iam/v1"
	"github.com/dashkan/pivox/internal/service/organizations"
	"github.com/dashkan/pivox/internal/testutil"
)

// noopTenantService stubs out Firebase Auth tenant operations for tests.
type noopTenantService struct{}

func (n noopTenantService) CreateTenant(_ context.Context, displayName string) (string, error) {
	return "tenant-" + displayName, nil
}

func (n noopTenantService) DeleteTenant(_ context.Context, _ string) error {
	return nil
}

func TestIntegration_Organizations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool, queries, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	iamHelper := iam.NewHelper(queries)

	conn := testutil.SetupGRPCServer(t, func(s *grpc.Server) {
		apiv1.RegisterOrganizationsServer(s, organizations.NewOrganizationsServer(pool, queries, iamHelper, noopTenantService{}))
	})

	client := apiv1.NewOrganizationsClient(conn)
	ctx := context.Background()

	var createdOrgName string

	t.Run("CreateOrganization", func(t *testing.T) {
		op, err := client.CreateOrganization(ctx, &apiv1.CreateOrganizationRequest{
			OrganizationId: "testorg",
			Organization: &apiv1.Organization{
				DisplayName: "Test Organization",
			},
		})
		require.NoError(t, err)
		assert.True(t, op.GetDone())

		var org apiv1.Organization
		require.NoError(t, op.GetResponse().UnmarshalTo(&org))
		assert.Equal(t, "organizations/testorg", org.GetName())
		assert.Equal(t, "Test Organization", org.GetDisplayName())
		createdOrgName = org.GetName()
	})

	t.Run("GetOrganization", func(t *testing.T) {
		resp, err := client.GetOrganization(ctx, &apiv1.GetOrganizationRequest{
			Name: createdOrgName,
		})
		require.NoError(t, err)
		assert.Equal(t, createdOrgName, resp.GetName())
		assert.Equal(t, "Test Organization", resp.GetDisplayName())
	})

	t.Run("ListOrganizations", func(t *testing.T) {
		resp, err := client.ListOrganizations(ctx, &apiv1.ListOrganizationsRequest{})
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(resp.GetOrganizations()), 1)

		found := false
		for _, o := range resp.GetOrganizations() {
			if o.GetName() == createdOrgName {
				found = true
			}
		}
		assert.True(t, found, "created org should appear in list")
	})

	t.Run("GetIamPolicy_Empty", func(t *testing.T) {
		resp, err := client.GetIamPolicy(ctx, &iampb.GetIamPolicyRequest{
			Name: createdOrgName,
		})
		require.NoError(t, err)
		// No bindings set yet.
		assert.Empty(t, resp.GetBindings())
	})

	t.Run("SetIamPolicy", func(t *testing.T) {
		resp, err := client.SetIamPolicy(ctx, &iampb.SetIamPolicyRequest{
			Resource: createdOrgName,
			Policy: &iampb.Policy{
				Bindings: []*iampb.Binding{
					{
						Role:    "roles/admin",
						Members: []string{"user:alice@example.com"},
					},
				},
			},
		})
		require.NoError(t, err)
		require.Len(t, resp.GetBindings(), 1)
		assert.Equal(t, "roles/admin", resp.GetBindings()[0].GetRole())
		assert.Contains(t, resp.GetBindings()[0].GetMembers(), "user:alice@example.com")
		assert.NotEmpty(t, resp.GetEtag())
	})

	t.Run("GetIamPolicy_WithBindings", func(t *testing.T) {
		resp, err := client.GetIamPolicy(ctx, &iampb.GetIamPolicyRequest{
			Name: createdOrgName,
		})
		require.NoError(t, err)
		require.Len(t, resp.GetBindings(), 1)
		assert.Equal(t, "roles/admin", resp.GetBindings()[0].GetRole())
	})
}
