//go:build dev

package server_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/permission"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	iampb "github.com/dashkan/pivox/internal/pkg/gen/pivox/iam/v1"
	"github.com/dashkan/pivox/internal/server"
	"github.com/dashkan/pivox/internal/service/iam"
	"github.com/dashkan/pivox/internal/service/organizations"
	"github.com/dashkan/pivox/internal/service/spaces"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
)

// Permission interceptor end-to-end tests. Pin the full
// authorization matrix through the production interceptor chain via
// grpcharness. Coverage:
//
//   - Direct user-binding allow + deny.
//   - Group-binding (caller ∈ group; group bound to org with role).
//   - Org → space inheritance (org-admin reaches space-scoped RPCs
//     without a direct space binding).
//   - Soft-delete gate: reads pass during DELETE_REQUESTED, mutations
//     blocked.
//
// All mutating-RPC scenarios use Spaces.CreateSpace (requires
// `spaces.create`, granted to owner+admin) since it's a well-wired
// admin-tier mutation. The Iam.CreateGroup RPC is currently
// Unimplemented, so groups for the binding tests are seeded via
// direct DB writes.

// TestE2E_Permission_OrgAdminCanCreateSpace pins the direct
// user-binding allow path. CreateSpace requires `spaces.create`
// which owner+admin have. A successor seeded as admin should be
// able to call CreateSpace.
func TestE2E_Permission_OrgAdminCanCreateSpace(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newPermissionHarness(t)
	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "owner"})
	h.SetCaller(owner)
	createOrgForPerm(t, apiv1.NewOrganizationsClient(h.Conn()), "perm-org", "Perm Org")
	orgID := h.LookupOrgID(t, "perm-org")

	admin := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "admin"})
	h.SeedMembership(t, orgID, admin, grpcharness.RoleAdmin)
	h.SetCaller(admin)

	spacesClient := apiv1.NewSpacesClient(h.Conn())
	op, err := spacesClient.CreateSpace(context.Background(), &apiv1.CreateSpaceRequest{
		Parent:  "organizations/perm-org",
		SpaceId: "engineering",
		Space:   &apiv1.Space{DisplayName: "Engineering"},
	})
	require.NoError(t, err)
	require.True(t, op.GetDone())
}

// TestE2E_Permission_OrgViewerCannotCreateSpace pins the direct
// user-binding deny path. Viewer lacks spaces.create — the
// permission interceptor returns PermissionDenied before the handler
// runs.
func TestE2E_Permission_OrgViewerCannotCreateSpace(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newPermissionHarness(t)
	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "owner"})
	h.SetCaller(owner)
	createOrgForPerm(t, apiv1.NewOrganizationsClient(h.Conn()), "perm-org", "Perm Org")
	orgID := h.LookupOrgID(t, "perm-org")

	viewer := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "viewer"})
	h.SeedMembership(t, orgID, viewer, grpcharness.RoleViewer)
	h.SetCaller(viewer)

	spacesClient := apiv1.NewSpacesClient(h.Conn())
	_, err := spacesClient.CreateSpace(context.Background(), &apiv1.CreateSpaceRequest{
		Parent:  "organizations/perm-org",
		SpaceId: "should-not-exist",
		Space:   &apiv1.Space{DisplayName: "Denied"},
	})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

// TestE2E_Permission_GroupBindingGrantsAccess pins permissions
// derived through GROUP membership (not direct user binding).
// Setup: caller has zero direct role bindings; they're a member of a
// group; the group is bound to the org with admin role. Calling
// CreateSpace should succeed because the resolver counts the
// caller's group-mediated admin role.
func TestE2E_Permission_GroupBindingGrantsAccess(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newPermissionHarness(t)
	ctx := context.Background()

	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "owner"})
	h.SetCaller(owner)
	createOrgForPerm(t, apiv1.NewOrganizationsClient(h.Conn()), "group-org", "Group Org")
	orgID := h.LookupOrgID(t, "group-org")

	// Caller has a per-org users row but NO direct role binding.
	caller := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "caller"})
	callerUserID := h.SeedUserMembershipOnly(t, orgID, caller)

	// Create a group + add caller to it + bind the group to admin.
	// All via direct DB writes since neither CreateGroup nor
	// AddGroupMembers RPCs are wired up yet.
	groupID := uuid.New()
	_, err := h.Pool.Exec(ctx,
		`INSERT INTO groups (id, org_id, display_name) VALUES ($1, $2, $3)`,
		groupID, orgID, "Admins")
	require.NoError(t, err)
	_, err = h.Pool.Exec(ctx,
		`INSERT INTO group_members (id, group_id, user_id) VALUES ($1, $2, $3)`,
		uuid.New(), groupID, callerUserID)
	require.NoError(t, err)
	adminRole, err := h.Queries.GetSystemRole(ctx, db.GetSystemRoleParams{
		OrgID: orgID, Name: grpcharness.RoleAdmin,
	})
	require.NoError(t, err)
	_, err = h.Queries.CreateOrgMember(ctx, db.CreateOrgMemberParams{
		ID:            uuid.New(),
		OrgID:         orgID,
		RoleID:        adminRole.ID,
		PrincipalKind: db.PrincipalKindGroup,
		PrincipalID:   groupID,
		CreatedBy:     owner.FirebaseIdentityID.String(),
	})
	require.NoError(t, err)

	// Switch to the caller — no direct binding, but in a group with
	// admin role. CreateSpace requires spaces.create; should pass.
	h.SetCaller(caller)
	spacesClient := apiv1.NewSpacesClient(h.Conn())
	_, err = spacesClient.CreateSpace(ctx, &apiv1.CreateSpaceRequest{
		Parent:  "organizations/group-org",
		SpaceId: "via-group",
		Space:   &apiv1.Space{DisplayName: "Via Group"},
	})
	require.NoError(t, err, "group-mediated admin role must allow CreateSpace")
}

// TestE2E_Permission_OrgRoleInheritsToSpaceReads pins org → space
// inheritance: a caller bound at the ORG level can read a space
// without a direct space binding. The resolver's space-scope path
// checks union(direct space binding, inherited org binding); the
// org-level role grants spaces.read.
func TestE2E_Permission_OrgRoleInheritsToSpaceReads(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newPermissionHarness(t)
	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "owner"})
	h.SetCaller(owner)
	createOrgForPerm(t, apiv1.NewOrganizationsClient(h.Conn()), "inherit-org", "Inherit Org")

	spacesClient := apiv1.NewSpacesClient(h.Conn())
	createSpaceOp, err := spacesClient.CreateSpace(context.Background(), &apiv1.CreateSpaceRequest{
		Parent:  "organizations/inherit-org",
		SpaceId: "engineering",
		Space:   &apiv1.Space{DisplayName: "Engineering"},
	})
	require.NoError(t, err)
	require.True(t, createSpaceOp.GetDone())

	// Owner (org-level binding) reads the space with no direct
	// space binding. The resolver inherits the org owner role to
	// the space scope.
	got, err := spacesClient.GetSpace(context.Background(), &apiv1.GetSpaceRequest{
		Name: "organizations/inherit-org/spaces/engineering",
	})
	require.NoError(t, err, "org owner must reach a space without a direct space binding")
	assert.Equal(t, "Engineering", got.GetDisplayName())
}

// TestE2E_Permission_SoftDeleteGateAllowsReads pins the soft-delete
// gate's read carve-out: with the org in DELETE_REQUESTED, reads
// (organizations.read, etc.) still succeed.
func TestE2E_Permission_SoftDeleteGateAllowsReads(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newPermissionHarness(t)
	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "owner"})
	h.SetCaller(owner)

	orgClient := apiv1.NewOrganizationsClient(h.Conn())
	createOrgForPerm(t, orgClient, "graceful-org", "Graceful Org")

	deleteOp, err := orgClient.DeleteOrganization(context.Background(), &apiv1.DeleteOrganizationRequest{
		Name: "organizations/graceful-org",
	})
	require.NoError(t, err)
	waitLifecycleOp(t, h, deleteOp, "DeleteOrganization")

	got, err := orgClient.GetOrganization(context.Background(), &apiv1.GetOrganizationRequest{
		Name: "organizations/graceful-org",
	})
	require.NoError(t, err)
	assert.Equal(t, apiv1.Organization_DELETE_REQUESTED, got.GetState())
}

// TestE2E_Permission_SoftDeleteGateBlocksMutations pins the gate's
// mutation rejection: even with the right permission, a mutating
// RPC against a DELETE_REQUESTED org is refused with
// FailedPrecondition. The soft-delete gate fires AFTER the
// permission check passes — the perm system OK'd the call, the
// gate said "not on a soft-deleted org."
func TestE2E_Permission_SoftDeleteGateBlocksMutations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newPermissionHarness(t)
	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "owner"})
	h.SetCaller(owner)

	orgClient := apiv1.NewOrganizationsClient(h.Conn())
	spacesClient := apiv1.NewSpacesClient(h.Conn())
	createOrgForPerm(t, orgClient, "shutting-down", "Shutting Down")

	deleteOp, err := orgClient.DeleteOrganization(context.Background(), &apiv1.DeleteOrganizationRequest{
		Name: "organizations/shutting-down",
	})
	require.NoError(t, err)
	waitLifecycleOp(t, h, deleteOp, "DeleteOrganization")

	// Owner has spaces.create; gate refuses anyway.
	_, err = spacesClient.CreateSpace(context.Background(), &apiv1.CreateSpaceRequest{
		Parent:  "organizations/shutting-down",
		SpaceId: "doomed",
		Space:   &apiv1.Space{DisplayName: "Doomed"},
	})
	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	assert.Contains(t, status.Convert(err).Message(), "DELETE_REQUESTED")
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func newPermissionHarness(t *testing.T) *grpcharness.Harness {
	return grpcharness.New(t, grpcharness.WithServices(func(h *grpcharness.Harness, s *grpc.Server) {
		callerIdentity := server.NewCallerIdentityResolver(h.Queries)
		permResolver := permission.NewResolver(h.Queries)
		apiv1.RegisterOrganizationsServer(s, organizations.NewOrganizationsServer(
			h.Pool, h.Queries, h.Auth, nil, server.AuthenticatedUID,
			permResolver, callerIdentity, h.LROManager, h.Encryptor,
		))
		apiv1.RegisterSpacesServer(s, spaces.NewSpacesServer(
			h.Pool, h.Pool, h.Queries, nil, permResolver, callerIdentity,
		))
		iampb.RegisterIamServer(s, iam.NewIamServer(
			h.Queries, h.Auth, callerIdentity, h.LROManager,
		))
	}))
}

func createOrgForPerm(t *testing.T, c apiv1.OrganizationsClient, slug, displayName string) {
	t.Helper()
	op, err := c.CreateOrganization(context.Background(), &apiv1.CreateOrganizationRequest{
		OrganizationId: slug,
		Organization:   &apiv1.Organization{DisplayName: displayName},
	})
	require.NoError(t, err)
	require.True(t, op.GetDone())
}

func waitLifecycleOp(t *testing.T, h *grpcharness.Harness, op interface{ GetName() string }, label string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	final, err := h.LROManager.WaitOperation(ctx, op.GetName())
	require.NoError(t, err, "WaitOperation(%s) failed", label)
	require.True(t, final.GetDone(), "%s should be done", label)
	if final.GetError() != nil {
		t.Fatalf("%s LRO failed: code=%d msg=%s",
			label, final.GetError().GetCode(), final.GetError().GetMessage())
	}
}
