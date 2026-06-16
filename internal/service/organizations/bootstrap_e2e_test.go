package organizations_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/permission"
)

// TestE2E_Bootstrap_PopulatesRolePermissions verifies that creating an
// org through the real CreateOrganization handler materializes the
// static system-role grant matrix (permission.RoleGrants) into the
// role_permissions table for all 4 system roles. This is the
// SQL-resolvable side of the permission model — the membership-scoped
// operations list (and any future set-based authz query) joins these
// rows instead of calling permission.Has() per row.
func TestE2E_Bootstrap_PopulatesRolePermissions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := newMembersHarness(t)
	ctx := context.Background()

	org := h.SeedOwnedOrg(t, "bootstrap-perms", "Bootstrap Perms", "owner")

	for _, roleName := range []string{
		permission.RoleOwner, permission.RoleAdmin,
		permission.RoleEditor, permission.RoleViewer,
	} {
		role, err := h.Queries.GetSystemRole(ctx, db.GetSystemRoleParams{
			OrgID: org.Row.ID,
			Name:  roleName,
		})
		require.NoError(t, err, "system role %q should exist after bootstrap", roleName)

		got, err := h.Queries.RolePermissionIDs(ctx, role.ID)
		require.NoError(t, err)

		assert.ElementsMatch(t, permission.RoleGrants[roleName], got,
			"role %q role_permissions must match the generated grant matrix", roleName)
		assert.NotEmpty(t, got, "role %q should have at least one granted permission", roleName)
	}
}
