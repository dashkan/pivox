package iam

import (
	"github.com/dashkan/pivox/internal/permission"
	iamv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/iam/v1"
	"github.com/dashkan/pivox/internal/server"
)

// RegistryEntries returns the permission-gating registry for every
// org-scoped RPC on the Iam service. main.go merges this with
// the registries from Organizations and Spaces before passing the
// union to server.PermissionInterceptor.
func RegistryEntries() server.Registry {
	return server.Registry{
		// --- Users (cross-org reads of users that are members of
		//     the org named in the path; DeleteUser is the global
		//     account-deletion LRO, owner-only) ---
		"/pivox.iam.v1.Iam/GetUser": {
			Permission: permission.UsersRead,
			Extract: func(req any) (server.ScopeRef, error) {
				return server.OrgScopeFromPath("name", req.(*iamv1.GetUserRequest).GetName())
			},
		},
		"/pivox.iam.v1.Iam/ListUsers": {
			Permission: permission.UsersRead,
			Extract: func(req any) (server.ScopeRef, error) {
				return server.OrgScopeFromPath("parent", req.(*iamv1.ListUsersRequest).GetParent())
			},
		},
		"/pivox.iam.v1.Iam/DeleteUser": {
			Permission: permission.UsersDelete,
			Extract: func(req any) (server.ScopeRef, error) {
				return server.OrgScopeFromPath("name", req.(*iamv1.DeleteUserRequest).GetName())
			},
		},

		// --- Roles (read-only in v1; CreateRole/UpdateRole/DeleteRole
		//     are unimplemented per the IAM roadmap) ---
		"/pivox.iam.v1.Iam/GetRole": {
			Permission: permission.RolesRead,
			Extract: func(req any) (server.ScopeRef, error) {
				return server.OrgScopeFromPath("name", req.(*iamv1.GetRoleRequest).GetName())
			},
		},
		"/pivox.iam.v1.Iam/ListRoles": {
			Permission: permission.RolesRead,
			Extract: func(req any) (server.ScopeRef, error) {
				return server.OrgScopeFromPath("parent", req.(*iamv1.ListRolesRequest).GetParent())
			},
		},

		// --- Groups (full CRUD plus group-member management) ---
		"/pivox.iam.v1.Iam/GetGroup": {
			Permission: permission.GroupsRead,
			Extract: func(req any) (server.ScopeRef, error) {
				return server.OrgScopeFromPath("name", req.(*iamv1.GetGroupRequest).GetName())
			},
		},
		"/pivox.iam.v1.Iam/ListGroups": {
			Permission: permission.GroupsRead,
			Extract: func(req any) (server.ScopeRef, error) {
				return server.OrgScopeFromPath("parent", req.(*iamv1.ListGroupsRequest).GetParent())
			},
		},
		"/pivox.iam.v1.Iam/CreateGroup": {
			Permission: permission.GroupsCreate,
			Extract: func(req any) (server.ScopeRef, error) {
				return server.OrgScopeFromPath("parent", req.(*iamv1.CreateGroupRequest).GetParent())
			},
		},
		"/pivox.iam.v1.Iam/UpdateGroup": {
			Permission: permission.GroupsUpdate,
			Extract: func(req any) (server.ScopeRef, error) {
				return server.OrgScopeFromPath("group.name", req.(*iamv1.UpdateGroupRequest).GetGroup().GetName())
			},
		},
		"/pivox.iam.v1.Iam/DeleteGroup": {
			Permission: permission.GroupsDelete,
			Extract: func(req any) (server.ScopeRef, error) {
				return server.OrgScopeFromPath("name", req.(*iamv1.DeleteGroupRequest).GetName())
			},
		},
		"/pivox.iam.v1.Iam/AddGroupMembers": {
			Permission: permission.GroupsManageMembers,
			Extract: func(req any) (server.ScopeRef, error) {
				return server.OrgScopeFromPath("group", req.(*iamv1.AddGroupMembersRequest).GetGroup())
			},
		},
		"/pivox.iam.v1.Iam/RemoveGroupMembers": {
			Permission: permission.GroupsManageMembers,
			Extract: func(req any) (server.ScopeRef, error) {
				return server.OrgScopeFromPath("group", req.(*iamv1.RemoveGroupMembersRequest).GetGroup())
			},
		},
		"/pivox.iam.v1.Iam/ListGroupMembers": {
			Permission: permission.GroupsRead,
			Extract: func(req any) (server.ScopeRef, error) {
				return server.OrgScopeFromPath("group", req.(*iamv1.ListGroupMembersRequest).GetGroup())
			},
		},
	}
}

// ExemptMethods returns the explicit allowlist of Iam RPCs that the
// permission interceptor passes through without a check.
//
// ListPermissions returns the global permission catalog — code-
// defined reference data, identical for every caller, with no
// parent resource to gate against. The membership interceptor
// still runs first, so memberless callers are denied; gating
// per-org on top adds nothing.
func ExemptMethods() map[string]bool {
	return map[string]bool{
		"/pivox.iam.v1.Iam/ListPermissions": true,
	}
}
