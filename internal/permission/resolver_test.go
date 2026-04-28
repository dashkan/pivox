package permission

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/testutil/mocks"
)

// fixtures shared across resolver tests.
var (
	testOrgID    = uuid.MustParse("0192a000-0001-7000-8000-000000000001")
	testSpaceID  = uuid.MustParse("0192a000-0002-7000-8000-000000000002")
	testIdentity = uuid.MustParse("0192a000-0003-7000-8000-000000000003")
)

// Org-scoped checks: caller's effective roles come from
// GetEffectiveOrgRoles only.

func TestResolver_OrgScope_AdminGrantsExpectedPermission(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("GetEffectiveOrgRoles", mock.Anything, db.GetEffectiveOrgRolesParams{
		OrgID:              testOrgID,
		FirebaseIdentityID: testIdentity,
	}).Return([]string{RoleAdmin}, nil)

	r := NewResolver(q)
	allowed, err := r.HasPermission(context.Background(), testIdentity, OrgTarget(testOrgID), "organizations.update")
	assert.NoError(t, err)
	assert.True(t, allowed, "admin should be allowed organizations.update")

	q.AssertExpectations(t)
}

func TestResolver_OrgScope_AdminDeniedDestructionClass(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("GetEffectiveOrgRoles", mock.Anything, mock.Anything).
		Return([]string{RoleAdmin}, nil)

	r := NewResolver(q)
	allowed, err := r.HasPermission(context.Background(), testIdentity, OrgTarget(testOrgID), "organizations.delete")
	assert.NoError(t, err)
	assert.False(t, allowed, "admin should NOT be allowed organizations.delete (owner-only)")
}

func TestResolver_OrgScope_OwnerAllowedDestructionClass(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("GetEffectiveOrgRoles", mock.Anything, mock.Anything).
		Return([]string{RoleOwner}, nil)

	r := NewResolver(q)
	allowed, err := r.HasPermission(context.Background(), testIdentity, OrgTarget(testOrgID), "organizations.delete")
	assert.NoError(t, err)
	assert.True(t, allowed, "owner should be allowed organizations.delete")
}

func TestResolver_OrgScope_NoMembershipDenies(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("GetEffectiveOrgRoles", mock.Anything, mock.Anything).
		Return([]string{}, nil)

	r := NewResolver(q)
	allowed, err := r.HasPermission(context.Background(), testIdentity, OrgTarget(testOrgID), "organizations.get")
	assert.NoError(t, err)
	assert.False(t, allowed, "non-member should be denied")
}

// Resolver returns true if ANY effective role grants the permission.
// A user can hold multiple roles via direct + group bindings.
func TestResolver_OrgScope_UnionAcrossMultipleRoles(t *testing.T) {
	q := new(mocks.MockQuerier)
	// Caller is direct viewer + (via a group) editor.
	q.On("GetEffectiveOrgRoles", mock.Anything, mock.Anything).
		Return([]string{RoleViewer, RoleEditor}, nil)

	r := NewResolver(q)
	// editor grants ai.conversations.create; viewer doesn't.
	allowed, err := r.HasPermission(context.Background(), testIdentity, OrgTarget(testOrgID), "ai.conversations.create")
	assert.NoError(t, err)
	assert.True(t, allowed, "any role granting the permission allows the call")
}

// Space-scoped checks: caller's effective roles are the union of
// GetEffectiveSpaceRoles (direct space-level bindings) and
// GetEffectiveOrgRoles (org-level inheritance — locked decision #1).

func TestResolver_SpaceScope_OrgRoleInherits(t *testing.T) {
	q := new(mocks.MockQuerier)
	// User has no direct space membership but is admin at the org.
	q.On("GetSpaceParentOrg", mock.Anything, testSpaceID).
		Return(testOrgID, nil)
	q.On("GetEffectiveSpaceRoles", mock.Anything, db.GetEffectiveSpaceRolesParams{
		SpaceID:            testSpaceID,
		FirebaseIdentityID: testIdentity,
	}).Return([]string{}, nil)
	q.On("GetEffectiveOrgRoles", mock.Anything, db.GetEffectiveOrgRolesParams{
		OrgID:              testOrgID,
		FirebaseIdentityID: testIdentity,
	}).Return([]string{RoleAdmin}, nil)

	r := NewResolver(q)
	// admin grants assets.assets.create; viewer doesn't.
	allowed, err := r.HasPermission(context.Background(), testIdentity, SpaceTarget(testSpaceID), "assets.assets.create")
	assert.NoError(t, err)
	assert.True(t, allowed, "org-admin should inherit at space scope (union)")
}

func TestResolver_SpaceScope_DirectSpaceRoleSuffices(t *testing.T) {
	q := new(mocks.MockQuerier)
	// User has a direct space-level editor binding; no org-level role.
	q.On("GetSpaceParentOrg", mock.Anything, testSpaceID).
		Return(testOrgID, nil)
	q.On("GetEffectiveSpaceRoles", mock.Anything, mock.Anything).
		Return([]string{RoleEditor}, nil)
	q.On("GetEffectiveOrgRoles", mock.Anything, mock.Anything).
		Return([]string{}, nil)

	r := NewResolver(q)
	allowed, err := r.HasPermission(context.Background(), testIdentity, SpaceTarget(testSpaceID), "assets.assets.create")
	assert.NoError(t, err)
	assert.True(t, allowed, "direct space-editor should suffice without org role")
}

func TestResolver_SpaceScope_NoBindingDenies(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("GetSpaceParentOrg", mock.Anything, testSpaceID).
		Return(testOrgID, nil)
	q.On("GetEffectiveSpaceRoles", mock.Anything, mock.Anything).
		Return([]string{}, nil)
	q.On("GetEffectiveOrgRoles", mock.Anything, mock.Anything).
		Return([]string{}, nil)

	r := NewResolver(q)
	allowed, err := r.HasPermission(context.Background(), testIdentity, SpaceTarget(testSpaceID), "assets.assets.get")
	assert.NoError(t, err)
	assert.False(t, allowed, "no binding at any scope should deny")
}

// Error paths. The resolver bubbles DB errors so the interceptor can
// return Internal — never silently denying a request that hit a DB
// fault.

func TestResolver_OrgScope_DBError_BubblesUp(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("GetEffectiveOrgRoles", mock.Anything, mock.Anything).
		Return([]string(nil), errors.New("connection refused"))

	r := NewResolver(q)
	_, err := r.HasPermission(context.Background(), testIdentity, OrgTarget(testOrgID), "organizations.get")
	assert.Error(t, err, "DB faults must surface, not be silently mapped to deny")
}

func TestResolver_SpaceScope_ParentOrgLookupError_BubblesUp(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("GetSpaceParentOrg", mock.Anything, testSpaceID).
		Return(uuid.Nil, errors.New("not found"))

	r := NewResolver(q)
	_, err := r.HasPermission(context.Background(), testIdentity, SpaceTarget(testSpaceID), "assets.assets.get")
	assert.Error(t, err)
}
