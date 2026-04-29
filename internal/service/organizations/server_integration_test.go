//go:build dev

package organizations_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
)

func TestIntegration_CreateOrganization_DuplicateName(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newLifecycleHarness(t)
	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "founder"})
	h.SetCaller(owner)

	client := apiv1.NewOrganizationsClient(h.Conn())
	ctx := context.Background()

	// Create the first org.
	_, err := client.CreateOrganization(ctx, &apiv1.CreateOrganizationRequest{
		OrganizationId: "dupetest",
		Organization:   &apiv1.Organization{DisplayName: "First Org"},
	})
	require.NoError(t, err)

	// Create the second org with the same name.
	_, err = client.CreateOrganization(ctx, &apiv1.CreateOrganizationRequest{
		OrganizationId: "dupetest",
		Organization:   &apiv1.Organization{DisplayName: "Duplicate Org"},
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.AlreadyExists, st.Code())
}

func TestIntegration_Organizations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newLifecycleHarness(t)
	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "founder"})
	h.SetCaller(owner)

	client := apiv1.NewOrganizationsClient(h.Conn())
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
}
