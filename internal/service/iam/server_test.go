package iam

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	db "github.com/dashkan/pivox/internal/db/generated"
	iampb "github.com/dashkan/pivox/internal/pkg/gen/pivox/iam/v1"
	"github.com/dashkan/pivox/internal/testutil/mocks"
)

// fixtures shared across handler tests.
var (
	testOrgID   = uuid.MustParse("0192a000-0001-7000-8000-000000000001")
	testOrgName = "acme"
	testOrg     = db.Organization{
		ID:   testOrgID,
		Name: testOrgName,
	}
	now = time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
)

func newServer(q *mocks.MockQuerier) *IamServer {
	return NewIamServer(q, nil /* resolver wired in TestIamPermissions tests */, nil /* readUID */)
}

// ---------------------------------------------------------------------------
// ListPermissions
// ---------------------------------------------------------------------------

func TestListPermissions_Success(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("ListPermissions", mock.Anything).Return([]db.Permission{
		{PermissionID: "organizations.get", DisplayName: "Get Organization", Description: "View"},
		{PermissionID: "spaces.create", DisplayName: "Create Space", Description: "Create"},
	}, nil)

	srv := newServer(q)
	resp, err := srv.ListPermissions(context.Background(), &iampb.ListPermissionsRequest{})
	require.NoError(t, err)
	require.Len(t, resp.GetPermissions(), 2)
	assert.Equal(t, "permissions/organizations.get", resp.GetPermissions()[0].GetName())
	assert.Equal(t, "Get Organization", resp.GetPermissions()[0].GetDisplayName())
	assert.Equal(t, "permissions/spaces.create", resp.GetPermissions()[1].GetName())
}

// ---------------------------------------------------------------------------
// GetRole
// ---------------------------------------------------------------------------

func TestGetRole_Success(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("GetOrganizationByName", mock.Anything, testOrgName).Return(testOrg, nil)
	q.On("GetSystemRole", mock.Anything, db.GetSystemRoleParams{
		OrgID: testOrgID,
		Name:  "owner",
	}).Return(db.Role{
		ID:          uuid.New(),
		OrgID:       testOrgID,
		Name:        "owner",
		DisplayName: "Owner",
		Description: "Full administrative access",
		IsSystem:    true,
		Etag:        "etag-owner",
		CreateTime:  now,
		UpdateTime:  now,
	}, nil)

	srv := newServer(q)
	resp, err := srv.GetRole(context.Background(), &iampb.GetRoleRequest{
		Name: "organizations/acme/roles/owner",
	})
	require.NoError(t, err)
	assert.Equal(t, "organizations/acme/roles/owner", resp.GetName())
	assert.Equal(t, "Owner", resp.GetDisplayName())
	assert.True(t, resp.GetSystem())
	// Owner role must include the destruction-class permissions
	// (matrix-derived); spot check one.
	assert.Contains(t, resp.GetPermissions(), "organizations.delete")
}

func TestGetRole_InvalidPath(t *testing.T) {
	q := new(mocks.MockQuerier)
	srv := newServer(q)
	_, err := srv.GetRole(context.Background(), &iampb.GetRoleRequest{
		Name: "not-a-role-path",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.NotEqual(t, codes.OK, st.Code())
}

func TestGetRole_OrgNotFound(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("GetOrganizationByName", mock.Anything, "ghost").
		Return(db.Organization{}, pgx.ErrNoRows)

	srv := newServer(q)
	_, err := srv.GetRole(context.Background(), &iampb.GetRoleRequest{
		Name: "organizations/ghost/roles/owner",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
}

func TestGetRole_RoleNotFound(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("GetOrganizationByName", mock.Anything, testOrgName).Return(testOrg, nil)
	q.On("GetSystemRole", mock.Anything, mock.Anything).
		Return(db.Role{}, pgx.ErrNoRows)

	srv := newServer(q)
	_, err := srv.GetRole(context.Background(), &iampb.GetRoleRequest{
		Name: "organizations/acme/roles/notarealrole",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
}

// ---------------------------------------------------------------------------
// ListRoles
// ---------------------------------------------------------------------------

func TestListRoles_ReturnsAllSystemRoles(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("GetOrganizationByName", mock.Anything, testOrgName).Return(testOrg, nil)
	q.On("ListRolesByOrg", mock.Anything, testOrgID).Return([]db.Role{
		{ID: uuid.New(), OrgID: testOrgID, Name: "owner", DisplayName: "Owner", IsSystem: true, CreateTime: now, UpdateTime: now},
		{ID: uuid.New(), OrgID: testOrgID, Name: "admin", DisplayName: "Admin", IsSystem: true, CreateTime: now, UpdateTime: now},
		{ID: uuid.New(), OrgID: testOrgID, Name: "editor", DisplayName: "Editor", IsSystem: true, CreateTime: now, UpdateTime: now},
		{ID: uuid.New(), OrgID: testOrgID, Name: "viewer", DisplayName: "Viewer", IsSystem: true, CreateTime: now, UpdateTime: now},
	}, nil)

	srv := newServer(q)
	resp, err := srv.ListRoles(context.Background(), &iampb.ListRolesRequest{
		Parent: "organizations/acme",
	})
	require.NoError(t, err)
	require.Len(t, resp.GetRoles(), 4)
	names := []string{}
	for _, r := range resp.GetRoles() {
		names = append(names, r.GetName())
	}
	assert.Contains(t, names, "organizations/acme/roles/owner")
	assert.Contains(t, names, "organizations/acme/roles/viewer")
}

// ---------------------------------------------------------------------------
// TestIamPermissions
// ---------------------------------------------------------------------------

func TestTestIamPermissions_FiltersToAllowedSubset(t *testing.T) {
	// Caller is admin in the org. Asks: of these 4 permissions, which
	// am I allowed? Server returns the subset (3) — the destruction-
	// class one (organizations.delete) is filtered out.
	q := new(mocks.MockQuerier)
	q.On("GetEffectiveOrgRoles", mock.Anything, mock.Anything).
		Return([]string{"admin"}, nil)

	identity := uuid.MustParse("0192a000-0002-7000-8000-000000000002")
	srv := NewIamServer(q, nil /* resolver constructed inline */, func(_ context.Context) (uuid.UUID, error) {
		return identity, nil
	})

	resp, err := srv.TestIamPermissions(context.Background(), &iampb.TestIamPermissionsRequest{
		Resource: "organizations/" + testOrgID.String(),
		Permissions: []string{
			"organizations.get",
			"organizations.update",
			"organizations.delete",
			"spaces.create",
		},
	})
	require.NoError(t, err)
	assert.ElementsMatch(t,
		[]string{"organizations.get", "organizations.update", "spaces.create"},
		resp.GetPermissions(),
		"admin should have these 3, not organizations.delete (owner-only)")
}

func TestTestIamPermissions_NoMembershipReturnsEmpty(t *testing.T) {
	// Caller has no role bindings in the org → the resolver finds no
	// effective roles → no permissions granted.
	q := new(mocks.MockQuerier)
	q.On("GetEffectiveOrgRoles", mock.Anything, mock.Anything).
		Return([]string{}, nil)

	identity := uuid.New()
	srv := NewIamServer(q, nil, func(_ context.Context) (uuid.UUID, error) {
		return identity, nil
	})

	resp, err := srv.TestIamPermissions(context.Background(), &iampb.TestIamPermissionsRequest{
		Resource:    "organizations/" + testOrgID.String(),
		Permissions: []string{"organizations.get", "spaces.create"},
	})
	require.NoError(t, err)
	assert.Empty(t, resp.GetPermissions())
}

func TestTestIamPermissions_MissingCallerIdentityRejects(t *testing.T) {
	q := new(mocks.MockQuerier)
	srv := NewIamServer(q, nil, func(_ context.Context) (uuid.UUID, error) {
		return uuid.Nil, status.Error(codes.Unauthenticated, "missing authenticated caller")
	})

	_, err := srv.TestIamPermissions(context.Background(), &iampb.TestIamPermissionsRequest{
		Resource:    "organizations/anything",
		Permissions: []string{"organizations.get"},
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Unauthenticated, st.Code())
}
