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
// Returns the user UUID (the principal_id used in org_members and
// the {user} segment of resource paths). Post-Phase-7 unification
// this is `firebase_identities.id` — the global Pivox user uuid,
// not a per-org join row.
func (h *Harness) SeedMembership(t *testing.T, orgID uuid.UUID, identity *Caller, role string) uuid.UUID {
	t.Helper()
	ctx := context.Background()

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
		PrincipalID:   identity.FirebaseIdentityID,
		CreatedBy:     identity.FirebaseIdentityID.String(),
	})
	require.NoError(t, err)

	return identity.FirebaseIdentityID
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
	return identity.FirebaseIdentityID
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

// LookupOrgUserID returns the firebase_identity_id directly —
// post-Phase-7 unification the per-org user uuid IS the
// firebase_identity_id, and there's no per-org users row to look
// up. The orgID parameter is preserved on the signature for
// caller compatibility but is no longer relevant: a
// firebase_identity has the same uuid in every org.
func (h *Harness) LookupOrgUserID(t *testing.T, orgID, firebaseIdentityID uuid.UUID) uuid.UUID {
	t.Helper()
	_ = orgID
	return firebaseIdentityID
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
