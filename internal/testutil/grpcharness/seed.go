//go:build dev

package grpcharness

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/permission"
)

// SeedMembership adds an identity as a member of an org with the
// given system role (one of permission.RoleOwner / RoleAdmin /
// RoleEditor / RoleViewer). Bypasses the gRPC client because
// CreateMember requires the caller to already be an org member with
// `members.create`, which creates a chicken-and-egg for tests that
// need to set up a multi-member org from scratch.
//
// The role must already exist on the org (i.e., the org was created
// via CreateOrganization, which seeds the 4 system roles in the
// same transaction).
//
// Returns the per-org user uuid (the principal_id used in
// org_members and as the {user} segment of the resource path).
func (h *Harness) SeedMembership(t *testing.T, orgID uuid.UUID, identity *Caller, role string) uuid.UUID {
	t.Helper()
	ctx := context.Background()

	user, err := h.Queries.CreateUserMembership(ctx, db.CreateUserMembershipParams{
		ID:                 uuid.New(),
		OrgID:              orgID,
		FirebaseIdentityID: identity.FirebaseIdentityID,
	})
	require.NoError(t, err)

	roleRow, err := h.Queries.GetSystemRole(ctx, db.GetSystemRoleParams{
		OrgID: orgID,
		Name:  role,
	})
	require.NoError(t, err, "system role %q not found in org %s — was the org created via CreateOrganization?", role, orgID)

	_, err = h.Queries.CreateOrgMember(ctx, db.CreateOrgMemberParams{
		ID:            uuid.New(),
		OrgID:         orgID,
		RoleID:        roleRow.ID,
		PrincipalKind: db.PrincipalKindUser,
		PrincipalID:   user.ID,
		CreatedBy:     identity.FirebaseIdentityID.String(),
	})
	require.NoError(t, err)

	return user.ID
}

// LookupOrgID resolves a slug to its uuid. Convenience for tests
// that create an org via CreateOrganization (which returns the proto
// Organization without exposing the uuid) and then need the uuid for
// SeedMembership.
func (h *Harness) LookupOrgID(t *testing.T, slug string) uuid.UUID {
	t.Helper()
	org, err := h.Queries.GetOrganizationByName(context.Background(), slug)
	require.NoError(t, err)
	return org.ID
}

// LookupOrgUserID resolves a firebase_identity's per-org users.id —
// the principal_id used in org_members and the {user} segment of
// resource paths. Tests need this when CreateOrganization (which
// creates the founder's user row internally) didn't surface the id
// and the test now needs to address that user via the gRPC API.
func (h *Harness) LookupOrgUserID(t *testing.T, orgID, firebaseIdentityID uuid.UUID) uuid.UUID {
	t.Helper()
	users, err := h.Queries.ListUsersByFirebaseIdentity(context.Background(), firebaseIdentityID)
	require.NoError(t, err)
	for _, u := range users {
		if u.OrgID == orgID {
			return u.ID
		}
	}
	t.Fatalf("no per-org user row for firebase_identity=%s in org=%s", firebaseIdentityID, orgID)
	return uuid.Nil
}

// permission.RoleOwner / RoleAdmin / RoleEditor / RoleViewer are the
// canonical strings; re-export here so tests don't have to import
// internal/permission for a single constant.
const (
	RoleOwner  = permission.RoleOwner
	RoleAdmin  = permission.RoleAdmin
	RoleEditor = permission.RoleEditor
	RoleViewer = permission.RoleViewer
)
