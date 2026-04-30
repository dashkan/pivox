package organizations

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/permission"
	"github.com/dashkan/pivox/internal/testutil/mocks"
)

// bootstrapOrgRoles is the focused unit under test: it must (a) seed
// exactly the 4 system roles for the new org and (b) bind the
// founder to the owner role via an org_members row. The
// CreateOrganization handler delegates to this helper inside its
// transaction; testing it in isolation against a MockQuerier keeps
// the high-level CreateOrganization test free of role-seeding noise.

func TestBootstrapOrgRoles_SeedsFourSystemRoles(t *testing.T) {
	q := new(mocks.MockQuerier)
	orgID := uuid.MustParse("0192a000-bbbb-7000-8000-000000000001")
	founderUserID := uuid.MustParse("0192a000-bbbb-7000-8000-000000000002")

	// Capture every CreateRole call so we can assert names + flags.
	createdRoles := map[string]db.CreateRoleParams{}
	q.On("CreateRole", mock.Anything, mock.MatchedBy(func(p db.CreateRoleParams) bool {
		createdRoles[p.Name] = p
		return true
	})).Return(nil).Times(4)

	// CreateOrgMember binds founder → owner.
	var memberArg db.CreateOrgMemberParams
	q.On("CreateOrgMember", mock.Anything, mock.MatchedBy(func(p db.CreateOrgMemberParams) bool {
		memberArg = p
		return true
	})).Return(db.CreateOrgMemberRow{}, nil).Once()

	err := bootstrapOrgRoles(context.Background(), q, orgID, founderUserID)
	require.NoError(t, err)

	// Exactly the 4 system roles, all flagged is_system.
	for _, name := range []string{
		permission.RoleOwner,
		permission.RoleAdmin,
		permission.RoleEditor,
		permission.RoleViewer,
	} {
		p, ok := createdRoles[name]
		require.True(t, ok, "system role %q must be seeded", name)
		assert.Equal(t, orgID, p.OrgID, "role %q org_id", name)
		assert.True(t, p.IsSystem, "role %q is_system", name)
		assert.NotEqual(t, uuid.Nil, p.ID, "role %q id assigned", name)
	}
	assert.Len(t, createdRoles, 4, "exactly 4 system roles seeded")

	// Owner-binding sanity: principal is the founder user (not a
	// group), and role_id points at the owner role just seeded.
	assert.Equal(t, orgID, memberArg.OrgID)
	assert.Equal(t, db.PrincipalKindUser, memberArg.PrincipalKind)
	assert.Equal(t, founderUserID, memberArg.PrincipalID)
	assert.Equal(t, createdRoles[permission.RoleOwner].ID, memberArg.RoleID,
		"founder must be bound to the just-created owner role for this org")
	// Audit `created_by` should be the founder UUID (PgUUID-wrapped).
	require.True(t, memberArg.CreatedBy.Valid)
	assert.Equal(t, founderUserID, uuid.UUID(memberArg.CreatedBy.Bytes))

	q.AssertExpectations(t)
}

func TestBootstrapOrgRoles_RoleInsertFailureBubblesUp(t *testing.T) {
	// If any of the role inserts fails, bootstrap returns the error
	// without attempting the org_member insert. Prevents partial state.
	q := new(mocks.MockQuerier)
	q.On("CreateRole", mock.Anything, mock.Anything).
		Return(errors.New("constraint violation")).Once()

	err := bootstrapOrgRoles(context.Background(), q,
		uuid.New(), uuid.New())
	require.Error(t, err)

	// CreateOrgMember was NOT called — fail-fast on the first error.
	q.AssertNotCalled(t, "CreateOrgMember", mock.Anything, mock.Anything)
}

func TestBootstrapOrgRoles_OwnerBindingFailureBubblesUp(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("CreateRole", mock.Anything, mock.Anything).Return(nil).Times(4)
	q.On("CreateOrgMember", mock.Anything, mock.Anything).
		Return(db.CreateOrgMemberRow{}, errors.New("fk violation")).Once()

	err := bootstrapOrgRoles(context.Background(), q,
		uuid.New(), uuid.New())
	require.Error(t, err)
}
