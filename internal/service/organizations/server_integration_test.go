package organizations_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/permission"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
)

// orgNamesFromList extracts the resource names from a
// ListOrganizations response so tests can assert membership
// without verbose loops at every call site.
func orgNamesFromList(resp *apiv1.ListOrganizationsResponse) []string {
	out := make([]string, 0, len(resp.GetOrganizations()))
	for _, o := range resp.GetOrganizations() {
		out = append(out, o.GetName())
	}
	return out
}

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

// TestIntegration_CreateOrganization_ValidateOnly pins the AIP
// validate_only contract: a dry-run Create runs the full bootstrap tx
// (org insert + role seed) against real constraints but persists
// nothing, so the same slug is reusable afterward — and a dry-run that
// would fail live (duplicate slug) still returns AlreadyExists.
func TestIntegration_CreateOrganization_ValidateOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newLifecycleHarness(t)
	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "founder"})
	h.SetCaller(owner)

	client := apiv1.NewOrganizationsClient(h.Conn())
	ctx := context.Background()

	// A dry-run Create returns the would-be resource but writes nothing.
	op, err := client.CreateOrganization(ctx, &apiv1.CreateOrganizationRequest{
		OrganizationId: "vo-org",
		Organization:   &apiv1.Organization{DisplayName: "Dry Org"},
		ValidateOnly:   true,
	})
	require.NoError(t, err)
	require.True(t, op.GetDone())

	// Nothing persisted → a real Create can reuse the same slug.
	_, err = client.CreateOrganization(ctx, &apiv1.CreateOrganizationRequest{
		OrganizationId: "vo-org",
		Organization:   &apiv1.Organization{DisplayName: "Real Org"},
	})
	require.NoError(t, err, "validate_only must not have persisted the org")

	// A dry-run that WOULD fail live (duplicate slug now exists) fails.
	_, err = client.CreateOrganization(ctx, &apiv1.CreateOrganizationRequest{
		OrganizationId: "vo-org",
		Organization:   &apiv1.Organization{DisplayName: "Dup Org"},
		ValidateOnly:   true,
	})
	require.Error(t, err)
	assert.Equal(t, codes.AlreadyExists, status.Code(err),
		"validate_only must fail if the live request would")
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

// TestIntegration_CreateOrganization_SeedsFounderBinding pins the
// load-bearing invariant that CreateOrganization atomically seeds
// the founding owner's `org_members` row alongside the org row.
//
// Why it matters: every permission check on the org goes through
// `org_members`. If CreateOrganization commits the org row but
// fails to seed the binding (a regression in the tx flow would do
// this silently), the founder loses access to their own org and
// permissions break in confusing ways. The MockQuerier-era test
// covered this via call-shape assertions; the rewrite verifies
// the post-condition directly: the founder is queryable as a
// member with the owner role.
func TestIntegration_CreateOrganization_SeedsFounderBinding(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newLifecycleHarness(t)
	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "founder"})
	h.SetCaller(owner)

	client := apiv1.NewOrganizationsClient(h.Conn())
	ctx := context.Background()

	op, err := client.CreateOrganization(ctx, &apiv1.CreateOrganizationRequest{
		OrganizationId: "binding-org",
		Organization:   &apiv1.Organization{DisplayName: "Binding Test"},
	})
	require.NoError(t, err)
	require.True(t, op.GetDone())

	// Founder must be queryable as an org member with the owner role.
	// Going through the DB directly (rather than ListMembers RPC) keeps
	// this assertion independent of the members surface — a Members
	// RPC bug shouldn't mask a CreateOrganization atomicity bug.
	orgID := h.LookupOrgID(t, "binding-org")
	roleRow, err := h.Queries.GetSystemRole(ctx, db.GetSystemRoleParams{OrgID: orgID, Name: permission.RoleOwner})
	require.NoError(t, err, "owner role must exist on freshly-created org")

	// Verify the founder's owner-membership row exists. Big enough
	// limit to cover any seeded members; in this test there's just
	// the founder.
	members, err := h.Queries.ListOrgMembers(ctx, db.ListOrgMembersParams{
		OrgID:  orgID,
		Offset: 0,
		Limit:  100,
	})
	require.NoError(t, err)

	var founderHasOwnerRole bool
	for _, m := range members {
		if m.UserID.Valid && m.UserID.Bytes == owner.IdentityID && m.RoleID == roleRow.ID {
			founderHasOwnerRole = true
		}
	}
	require.True(t, founderHasOwnerRole,
		"founder identity must have an owner-role binding after CreateOrganization")
}

// TestIntegration_CreateOrganization_SeedsSystemRoles pins the
// invariant that all four system roles (owner / admin / editor /
// viewer) are seeded with the org row in a single tx. A regression
// breaks every subsequent CreateMember call with "role not found,"
// which is a confusing failure mode unless this test catches it.
func TestIntegration_CreateOrganization_SeedsSystemRoles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newLifecycleHarness(t)
	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "founder"})
	h.SetCaller(owner)

	client := apiv1.NewOrganizationsClient(h.Conn())
	ctx := context.Background()

	_, err := client.CreateOrganization(ctx, &apiv1.CreateOrganizationRequest{
		OrganizationId: "roles-org",
		Organization:   &apiv1.Organization{DisplayName: "Roles Test"},
	})
	require.NoError(t, err)

	orgID := h.LookupOrgID(t, "roles-org")
	for _, role := range []string{
		permission.RoleOwner,
		permission.RoleAdmin,
		permission.RoleEditor,
		permission.RoleViewer,
	} {
		_, err := h.Queries.GetSystemRole(ctx, db.GetSystemRoleParams{OrgID: orgID, Name: role})
		require.NoErrorf(t, err, "system role %q must exist on freshly-created org", role)
	}
}

// TestIntegration_GetOrganization_NotFound pins the NotFound path —
// the protovalidate interceptor catches malformed names; this test
// covers the well-formed-but-unknown-slug case which goes all the
// way to the handler's DB lookup.
func TestIntegration_GetOrganization_NotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newLifecycleHarness(t)
	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "founder"})
	h.SetCaller(owner)

	client := apiv1.NewOrganizationsClient(h.Conn())

	_, err := client.GetOrganization(context.Background(), &apiv1.GetOrganizationRequest{
		Name: "organizations/never-existed",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	// Caller has no membership on the unknown org → the
	// MembershipRequiredInterceptor kicks in before reaching the
	// handler, so the surface is PermissionDenied (or NotFound,
	// depending on whether the org row even exists). Pinning
	// "either of these is acceptable" keeps the test honest about
	// the layered enforcement without overspecifying the order.
	assert.Contains(t, []codes.Code{codes.NotFound, codes.PermissionDenied}, st.Code(),
		"unknown org should be NotFound or PermissionDenied (interceptor or handler)")
}

// TestIntegration_ListOrganizations_OnlyCallerOrgs pins access
// control on the list surface: a caller sees ONLY the orgs they're
// a member of, regardless of how many orgs exist globally. This is
// the access-control story for the cross-tenant list, and a
// regression here is a multi-tenant data leak.
func TestIntegration_ListOrganizations_OnlyCallerOrgs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newLifecycleHarness(t)
	client := apiv1.NewOrganizationsClient(h.Conn())
	ctx := context.Background()

	// Two independent founders, two independent orgs.
	alice := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "alice"})
	bob := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "bob"})

	h.SetCaller(alice)
	_, err := client.CreateOrganization(ctx, &apiv1.CreateOrganizationRequest{
		OrganizationId: "alice-org",
		Organization:   &apiv1.Organization{DisplayName: "Alice's Org"},
	})
	require.NoError(t, err)

	h.SetCaller(bob)
	_, err = client.CreateOrganization(ctx, &apiv1.CreateOrganizationRequest{
		OrganizationId: "bob-org",
		Organization:   &apiv1.Organization{DisplayName: "Bob's Org"},
	})
	require.NoError(t, err)

	// Alice lists — should see alice-org, NOT bob-org.
	h.SetCaller(alice)
	aliceList, err := client.ListOrganizations(ctx, &apiv1.ListOrganizationsRequest{})
	require.NoError(t, err)
	aliceNames := orgNamesFromList(aliceList)
	assert.Contains(t, aliceNames, "organizations/alice-org")
	assert.NotContains(t, aliceNames, "organizations/bob-org",
		"alice must not see bob-org — multi-tenant data leak")

	// Bob lists — symmetric.
	h.SetCaller(bob)
	bobList, err := client.ListOrganizations(ctx, &apiv1.ListOrganizationsRequest{})
	require.NoError(t, err)
	bobNames := orgNamesFromList(bobList)
	assert.Contains(t, bobNames, "organizations/bob-org")
	assert.NotContains(t, bobNames, "organizations/alice-org",
		"bob must not see alice-org — multi-tenant data leak")
}

// TestIntegration_ListOrganizations_EmptyForUnaffiliatedCaller —
// a caller with zero memberships gets an empty list (not an
// error). This is the canonical "fresh user with no orgs" state.
func TestIntegration_ListOrganizations_EmptyForUnaffiliatedCaller(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newLifecycleHarness(t)
	client := apiv1.NewOrganizationsClient(h.Conn())
	ctx := context.Background()

	// Founder creates an org so the global org count is non-zero.
	founder := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "founder"})
	h.SetCaller(founder)
	_, err := client.CreateOrganization(ctx, &apiv1.CreateOrganizationRequest{
		OrganizationId: "founder-org",
		Organization:   &apiv1.Organization{DisplayName: "Founder Org"},
	})
	require.NoError(t, err)

	// New identity with no memberships.
	stranger := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "stranger"})
	h.SetCaller(stranger)

	resp, err := client.ListOrganizations(ctx, &apiv1.ListOrganizationsRequest{})
	require.NoError(t, err, "unaffiliated caller gets empty list, not an error")
	assert.Empty(t, resp.GetOrganizations(),
		"caller with no memberships sees zero orgs even though founder-org exists")
}
