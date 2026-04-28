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
// GetMember (org scope)
// ---------------------------------------------------------------------------

func TestGetMember_OrgScope_UserPrincipal(t *testing.T) {
	q := new(mocks.MockQuerier)
	userID := uuid.MustParse("0192a000-0010-7000-8000-000000000010")
	q.On("GetOrganizationByName", mock.Anything, testOrgName).Return(testOrg, nil)
	q.On("GetOrgMember", mock.Anything, db.GetOrgMemberParams{
		OrgID:         testOrgID,
		PrincipalKind: db.PrincipalKindUser,
		PrincipalID:   userID,
	}).Return(db.GetOrgMemberRow{
		ID:            uuid.New(),
		OrgID:         testOrgID,
		RoleID:        uuid.New(),
		PrincipalKind: db.PrincipalKindUser,
		PrincipalID:   userID,
		RoleName:      "admin",
		Etag:          "etag-m",
		CreateTime:    now,
		UpdateTime:    now,
	}, nil)

	srv := newServer(q)
	resp, err := srv.GetMember(context.Background(), &iampb.GetMemberRequest{
		Name: "organizations/acme/members/user-" + userID.String(),
	})
	require.NoError(t, err)
	assert.Equal(t, "organizations/acme/members/user-"+userID.String(), resp.GetName())
	assert.Equal(t, "organizations/acme/roles/admin", resp.GetRole())
	assert.Equal(t, "organizations/acme/users/"+userID.String(), resp.GetUser())
	assert.Empty(t, resp.GetGroup())
}

func TestGetMember_SpaceScope_GroupPrincipal(t *testing.T) {
	spaceID := uuid.MustParse("0192a000-0020-7000-8000-000000000020")
	groupID := uuid.MustParse("0192a000-0030-7000-8000-000000000030")
	q := new(mocks.MockQuerier)
	q.On("GetOrganizationByName", mock.Anything, testOrgName).Return(testOrg, nil)
	q.On("GetSpaceByName", mock.Anything, db.GetSpaceByNameParams{
		OrgID: testOrgID,
		Name:  "news",
	}).Return(db.Space{ID: spaceID, OrgID: testOrgID, Name: "news"}, nil)
	q.On("GetSpaceMember", mock.Anything, db.GetSpaceMemberParams{
		SpaceID:       spaceID,
		PrincipalKind: db.PrincipalKindGroup,
		PrincipalID:   groupID,
	}).Return(db.GetSpaceMemberRow{
		ID:            uuid.New(),
		SpaceID:       spaceID,
		RoleID:        uuid.New(),
		PrincipalKind: db.PrincipalKindGroup,
		PrincipalID:   groupID,
		RoleName:      "editor",
		Etag:          "etag-sm",
		CreateTime:    now,
		UpdateTime:    now,
	}, nil)

	srv := newServer(q)
	resp, err := srv.GetMember(context.Background(), &iampb.GetMemberRequest{
		Name: "organizations/acme/spaces/news/members/group-" + groupID.String(),
	})
	require.NoError(t, err)
	assert.Equal(t, "organizations/acme/spaces/news/members/group-"+groupID.String(), resp.GetName())
	assert.Equal(t, "organizations/acme/roles/editor", resp.GetRole())
	assert.Equal(t, "organizations/acme/groups/"+groupID.String(), resp.GetGroup())
	assert.Empty(t, resp.GetUser())
}

func TestGetMember_InvalidNameShape(t *testing.T) {
	cases := []string{
		"organizations/acme/members/no-prefix",
		"organizations/acme/members/user-not-a-uuid",
		"organizations/acme/members/owner-1234",              // wrong prefix
		"organizations/acme/members/",                        // empty member
		"members/user-x",                                     // missing org
		"organizations//members/user-" + uuid.New().String(), // empty org slug
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			q := new(mocks.MockQuerier)
			srv := newServer(q)
			_, err := srv.GetMember(context.Background(), &iampb.GetMemberRequest{Name: name})
			require.Error(t, err)
			st, _ := status.FromError(err)
			assert.Equal(t, codes.InvalidArgument, st.Code())
		})
	}
}

func TestGetMember_OrgNotFound(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("GetOrganizationByName", mock.Anything, "ghost").Return(db.Organization{}, pgx.ErrNoRows)

	srv := newServer(q)
	_, err := srv.GetMember(context.Background(), &iampb.GetMemberRequest{
		Name: "organizations/ghost/members/user-" + uuid.New().String(),
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
}

// ---------------------------------------------------------------------------
// ListMembers
// ---------------------------------------------------------------------------

func TestListMembers_OrgScope(t *testing.T) {
	userA := uuid.MustParse("0192a000-0040-7000-8000-000000000040")
	userB := uuid.MustParse("0192a000-0041-7000-8000-000000000041")
	q := new(mocks.MockQuerier)
	q.On("GetOrganizationByName", mock.Anything, testOrgName).Return(testOrg, nil)
	q.On("ListOrgMembers", mock.Anything, testOrgID).Return([]db.ListOrgMembersRow{
		{
			ID: uuid.New(), OrgID: testOrgID, RoleID: uuid.New(),
			PrincipalKind: db.PrincipalKindUser, PrincipalID: userA,
			RoleName: "owner", Etag: "e1", CreateTime: now, UpdateTime: now,
		},
		{
			ID: uuid.New(), OrgID: testOrgID, RoleID: uuid.New(),
			PrincipalKind: db.PrincipalKindUser, PrincipalID: userB,
			RoleName: "editor", Etag: "e2", CreateTime: now, UpdateTime: now,
		},
	}, nil)

	srv := newServer(q)
	resp, err := srv.ListMembers(context.Background(), &iampb.ListMembersRequest{
		Parent: "organizations/acme",
	})
	require.NoError(t, err)
	require.Len(t, resp.GetMembers(), 2)
	assert.Equal(t, "organizations/acme/roles/owner", resp.GetMembers()[0].GetRole())
	assert.Equal(t, "organizations/acme/roles/editor", resp.GetMembers()[1].GetRole())
}

func TestListMembers_SpaceScope_DirectBindingsOnly(t *testing.T) {
	// Direct space-level binding only — org-level inheritance is the
	// resolver's concern and does NOT show up as a Member at space scope.
	spaceID := uuid.MustParse("0192a000-0050-7000-8000-000000000050")
	userA := uuid.MustParse("0192a000-0051-7000-8000-000000000051")
	q := new(mocks.MockQuerier)
	q.On("GetOrganizationByName", mock.Anything, testOrgName).Return(testOrg, nil)
	q.On("GetSpaceByName", mock.Anything, mock.Anything).Return(db.Space{ID: spaceID}, nil)
	q.On("ListSpaceMembers", mock.Anything, spaceID).Return([]db.ListSpaceMembersRow{
		{
			ID: uuid.New(), SpaceID: spaceID, RoleID: uuid.New(),
			PrincipalKind: db.PrincipalKindUser, PrincipalID: userA,
			RoleName: "viewer", Etag: "e", CreateTime: now, UpdateTime: now,
		},
	}, nil)

	srv := newServer(q)
	resp, err := srv.ListMembers(context.Background(), &iampb.ListMembersRequest{
		Parent: "organizations/acme/spaces/news",
	})
	require.NoError(t, err)
	require.Len(t, resp.GetMembers(), 1)
	assert.Equal(t, "organizations/acme/spaces/news/members/user-"+userA.String(),
		resp.GetMembers()[0].GetName())
}

func TestListMembers_InvalidParentShape(t *testing.T) {
	cases := []string{
		"",
		"organizations",
		"organizations/",
		"organizations/acme/spaces",
		"organizations/acme/spaces/",
		"members",
	}
	for _, p := range cases {
		t.Run(p, func(t *testing.T) {
			q := new(mocks.MockQuerier)
			srv := newServer(q)
			_, err := srv.ListMembers(context.Background(), &iampb.ListMembersRequest{Parent: p})
			require.Error(t, err)
			st, _ := status.FromError(err)
			assert.Equal(t, codes.InvalidArgument, st.Code())
		})
	}
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
