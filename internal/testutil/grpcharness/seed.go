package grpcharness

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/dashkan/pivox/internal/convert"
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
// Returns the user UUID (the principal_id used in org_members and
// the {user} segment of resource paths). Post-Phase-7 unification
// this is `identities.id` — the global Pivox user uuid,
// not a per-org join row.
func (h *Harness) SeedMembership(t *testing.T, orgID uuid.UUID, identity *Caller, role string) uuid.UUID {
	t.Helper()
	ctx := context.Background()

	roleRow, err := h.Queries.GetSystemRole(ctx, db.GetSystemRoleParams{
		OrgID: orgID,
		Name:  role,
	})
	require.NoError(t, err, "system role %q not found in org %s — was the org created via CreateOrganization?", role, orgID)

	// Post-principal-id-split: members carry typed user_id /
	// group_id columns instead of (principal_kind, principal_id).
	// This helper only seeds user bindings; group memberships go via
	// a separate path.
	_, err = h.Queries.CreateOrgUserMember(ctx, db.CreateOrgUserMemberParams{
		ID:        uuid.New(),
		OrgID:     orgID,
		RoleID:    roleRow.ID,
		UserID:    convert.PgUUID(identity.IdentityID),
		CreatedBy: convert.PgUUID(identity.IdentityID),
	})
	require.NoError(t, err)

	return identity.IdentityID
}

// SeedSpaceMembership binds an identity to a space with the given system role
// via a direct `space_members` user binding (no org_members row of its own).
// Roles are org-scoped and shared by the org's spaces, so `role` resolves
// against the parent org's system roles. Returns the user UUID.
//
// Note: the MembershipRequiredInterceptor gates every non-bootstrap RPC on org
// membership (`org_members`), which a space binding does NOT satisfy — a caller
// with ONLY a space binding is treated as memberless. To exercise space-scoped
// authorization, also give the identity an org binding (e.g. SeedMembership with
// a low-privilege role like viewer) so it clears the membership gate.
func (h *Harness) SeedSpaceMembership(t *testing.T, orgID, spaceID uuid.UUID, identity *Caller, role string) uuid.UUID {
	t.Helper()
	ctx := context.Background()

	roleRow, err := h.Queries.GetSystemRole(ctx, db.GetSystemRoleParams{
		OrgID: orgID,
		Name:  role,
	})
	require.NoError(t, err, "system role %q not found in org %s — was the org created via CreateOrganization?", role, orgID)

	_, err = h.Queries.CreateSpaceUserMember(ctx, db.CreateSpaceUserMemberParams{
		ID:        uuid.New(),
		SpaceID:   spaceID,
		RoleID:    roleRow.ID,
		UserID:    convert.PgUUID(identity.IdentityID),
		CreatedBy: convert.PgUUID(identity.IdentityID),
	})
	require.NoError(t, err)

	return identity.IdentityID
}

// SeedGroupMembership binds an identity to `role` at org scope purely
// through a group: it creates a group in the org, adds the identity as
// a group member, and binds the group to the role via org_members
// (group_id) with NO direct user binding. Exercises the group-derived
// resolution branch (group_id IN (SELECT … group_members …)) the
// permission queries resolve. Returns the group id.
func (h *Harness) SeedGroupMembership(t *testing.T, orgID uuid.UUID, identity *Caller, role string) uuid.UUID {
	t.Helper()
	ctx := context.Background()

	roleRow, err := h.Queries.GetSystemRole(ctx, db.GetSystemRoleParams{OrgID: orgID, Name: role})
	require.NoError(t, err, "system role %q not found in org %s", role, orgID)

	groupID := uuid.New()
	_, err = h.Pool.Exec(ctx,
		`INSERT INTO groups (id, org_id, display_name, created_by) VALUES ($1, $2, $3, $4)`,
		groupID, orgID, "test-group", identity.IdentityID)
	require.NoError(t, err)

	_, err = h.Pool.Exec(ctx,
		`INSERT INTO group_members (id, group_id, user_id, created_by) VALUES ($1, $2, $3, $4)`,
		uuid.New(), groupID, identity.IdentityID, identity.IdentityID)
	require.NoError(t, err)

	_, err = h.Queries.CreateOrgGroupMember(ctx, db.CreateOrgGroupMemberParams{
		ID:        uuid.New(),
		OrgID:     orgID,
		RoleID:    roleRow.ID,
		GroupID:   convert.PgUUID(groupID),
		CreatedBy: convert.PgUUID(identity.IdentityID),
	})
	require.NoError(t, err)

	return groupID
}

// SeedUserMembershipOnly is retained for compatibility with E2E
// tests that previously created a `users` row without an
// org_members binding (to verify group-mediated access reached the
// handler). Post-Phase-7 there's no `users` table — "membership"
// is just the existence of a binding. So this helper is now a
// no-op that returns the caller's user UUID directly. Tests that
// relied on the "user-row-but-no-binding" middle state should
// either drop that scenario or seed a group_members row instead.
func (h *Harness) SeedUserMembershipOnly(t *testing.T, orgID uuid.UUID, identity *Caller) uuid.UUID {
	t.Helper()
	_ = orgID
	return identity.IdentityID
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

// LookupOrgUserID returns the identity_id directly —
// post-Phase-7 unification the per-org user uuid IS the
// identity_id, and there's no per-org users row to look
// up. The orgID parameter is preserved on the signature for
// caller compatibility but is no longer relevant: an
// identity has the same uuid in every org.
func (h *Harness) LookupOrgUserID(t *testing.T, orgID, identityID uuid.UUID) uuid.UUID {
	t.Helper()
	_ = orgID
	return identityID
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
