package iam

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/dashkan/pivox/internal/permission"
	iamv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/iam/v1"
	"github.com/dashkan/pivox/internal/server"
)

func TestRegistryEntries_AllExtractorsYieldOrgScope(t *testing.T) {
	const slug = "acme"

	cases := []struct {
		method string
		want   string
		req    any
	}{
		// Users
		{"/pivox.iam.v1.Iam/GetUser",
			permission.UsersRead,
			&iamv1.GetUserRequest{Name: "organizations/" + slug + "/users/u-1"}},
		{"/pivox.iam.v1.Iam/ListUsers",
			permission.UsersRead,
			&iamv1.ListUsersRequest{Parent: "organizations/" + slug}},
		{"/pivox.iam.v1.Iam/DeleteUser",
			permission.UsersDelete,
			&iamv1.DeleteUserRequest{Name: "organizations/" + slug + "/users/u-1"}},

		// Roles
		{"/pivox.iam.v1.Iam/GetRole",
			permission.RolesRead,
			&iamv1.GetRoleRequest{Name: "organizations/" + slug + "/roles/admin"}},
		{"/pivox.iam.v1.Iam/ListRoles",
			permission.RolesRead,
			&iamv1.ListRolesRequest{Parent: "organizations/" + slug}},

		// Groups
		{"/pivox.iam.v1.Iam/GetGroup",
			permission.GroupsRead,
			&iamv1.GetGroupRequest{Name: "organizations/" + slug + "/groups/g-1"}},
		{"/pivox.iam.v1.Iam/ListGroups",
			permission.GroupsRead,
			&iamv1.ListGroupsRequest{Parent: "organizations/" + slug}},
		{"/pivox.iam.v1.Iam/CreateGroup",
			permission.GroupsCreate,
			&iamv1.CreateGroupRequest{Parent: "organizations/" + slug}},
		{"/pivox.iam.v1.Iam/UpdateGroup",
			permission.GroupsUpdate,
			&iamv1.UpdateGroupRequest{Group: &iamv1.Group{Name: "organizations/" + slug + "/groups/g-1"}}},
		{"/pivox.iam.v1.Iam/DeleteGroup",
			permission.GroupsDelete,
			&iamv1.DeleteGroupRequest{Name: "organizations/" + slug + "/groups/g-1"}},
		{"/pivox.iam.v1.Iam/AddGroupMembers",
			permission.GroupsManageMembers,
			&iamv1.AddGroupMembersRequest{Group: "organizations/" + slug + "/groups/g-1"}},
		{"/pivox.iam.v1.Iam/RemoveGroupMembers",
			permission.GroupsManageMembers,
			&iamv1.RemoveGroupMembersRequest{Group: "organizations/" + slug + "/groups/g-1"}},
		{"/pivox.iam.v1.Iam/ListGroupMembers",
			permission.GroupsRead,
			&iamv1.ListGroupMembersRequest{Group: "organizations/" + slug + "/groups/g-1"}},
	}

	reg := RegistryEntries()
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			entry, ok := reg[tc.method]
			require.Truef(t, ok, "method %s missing from registry", tc.method)
			assert.Equal(t, tc.want, entry.Permission)
			got, err := entry.Extract(tc.req)
			require.NoError(t, err)
			assert.Equal(t, server.ScopeOrg, got.Kind)
			assert.Equal(t, slug, got.Slug)
		})
	}
}

func TestRegistryEntries_RejectsInvalidPaths(t *testing.T) {
	reg := RegistryEntries()
	entry := reg["/pivox.iam.v1.Iam/GetUser"]
	_, err := entry.Extract(&iamv1.GetUserRequest{Name: "users/me"})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

// TestExemptMethods_ListPermissionsOnly pins the explicit exempt set.
// ListPermissions is the only Iam RPC without org scope — the
// permission catalog is global reference data, identical for every
// caller, with no parent resource to gate against.
func TestExemptMethods_ListPermissionsOnly(t *testing.T) {
	want := map[string]bool{
		"/pivox.iam.v1.Iam/ListPermissions": true,
	}
	got := ExemptMethods()
	assert.Equal(t, want, got)
}

func TestRegistryAndExempt_NoOverlap(t *testing.T) {
	reg := RegistryEntries()
	exempt := ExemptMethods()
	for method := range exempt {
		_, dup := reg[method]
		assert.Falsef(t, dup, "method %q is in both registry and exempt", method)
	}
}

// TestRegistryCoversAllProtoMethods is the build-time completeness
// guard: every RPC declared on the Iam service descriptor must be
// in the registry OR the exempt set. Adding a proto RPC without
// wiring its permission gate fails this test.
func TestRegistryCoversAllProtoMethods(t *testing.T) {
	uncovered := server.AssertRegistryCoversService(
		&iamv1.Iam_ServiceDesc,
		RegistryEntries(),
		ExemptMethods(),
	)
	assert.Empty(t, uncovered,
		"every Iam RPC must be in RegistryEntries() or ExemptMethods(); these are unwired: %v",
		uncovered)
}
