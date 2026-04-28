package spaces

import (
	"github.com/dashkan/pivox/internal/permission"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	iamv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/iam/v1"
	"github.com/dashkan/pivox/internal/server"
)

// RegistryEntries returns the permission-gating registry for every
// RPC on the Spaces service. main.go merges this with the
// Organizations and Iam registries before passing the union to
// server.PermissionInterceptor.
//
// Two scope kinds are mixed here:
//   - Org-scope (ListSpaces, CreateSpace) — the action is at the
//     organization level; resolved via OrgScopeFromPath against the
//     `parent` field.
//   - Space-scope (everything else) — the action targets a specific
//     space; resolved via SpaceScopeFromPath, which extracts both
//     the parent org slug and the space slug. The interceptor's
//     resolver path checks SpaceTarget which inherits org-level
//     bindings (sub-decision #1).
func RegistryEntries() server.Registry {
	return server.Registry{
		// --- Org-scope: list and create live at the org level ---
		"/pivox.api.v1.Spaces/ListSpaces": {
			Permission: permission.SpacesRead,
			Extract: func(req any) (server.ScopeRef, error) {
				return server.OrgScopeFromPath("parent", req.(*apiv1.ListSpacesRequest).GetParent())
			},
		},
		"/pivox.api.v1.Spaces/CreateSpace": {
			Permission: permission.SpacesCreate,
			Extract: func(req any) (server.ScopeRef, error) {
				return server.OrgScopeFromPath("parent", req.(*apiv1.CreateSpaceRequest).GetParent())
			},
		},

		// --- Space-scope: a specific space is the resource ---
		"/pivox.api.v1.Spaces/GetSpace": {
			Permission: permission.SpacesRead,
			Extract: func(req any) (server.ScopeRef, error) {
				return server.SpaceScopeFromPath("name", req.(*apiv1.GetSpaceRequest).GetName())
			},
		},
		"/pivox.api.v1.Spaces/UpdateSpace": {
			Permission: permission.SpacesUpdate,
			Extract: func(req any) (server.ScopeRef, error) {
				return server.SpaceScopeFromPath("space.name", req.(*apiv1.UpdateSpaceRequest).GetSpace().GetName())
			},
		},
		"/pivox.api.v1.Spaces/DeleteSpace": {
			Permission: permission.SpacesDelete,
			Extract: func(req any) (server.ScopeRef, error) {
				return server.SpaceScopeFromPath("name", req.(*apiv1.DeleteSpaceRequest).GetName())
			},
		},
		"/pivox.api.v1.Spaces/UndeleteSpace": {
			// Undelete reverses Delete; same destruction-class tier
			// (admin-allowed for spaces, distinct from org Undelete
			// which is owner-only).
			Permission: permission.SpacesDelete,
			Extract: func(req any) (server.ScopeRef, error) {
				return server.SpaceScopeFromPath("name", req.(*apiv1.UndeleteSpaceRequest).GetName())
			},
		},

		// --- Members at space scope (same RPC types as org-scope
		//     members; the extractor resolves the space rather than
		//     the org) ---
		"/pivox.api.v1.Spaces/GetMember": {
			Permission: permission.MembersRead,
			Extract: func(req any) (server.ScopeRef, error) {
				return server.SpaceScopeFromPath("name", req.(*iamv1.GetMemberRequest).GetName())
			},
		},
		"/pivox.api.v1.Spaces/ListMembers": {
			Permission: permission.MembersRead,
			Extract: func(req any) (server.ScopeRef, error) {
				return server.SpaceScopeFromPath("parent", req.(*iamv1.ListMembersRequest).GetParent())
			},
		},
		"/pivox.api.v1.Spaces/CreateMember": {
			Permission: permission.MembersCreate,
			Extract: func(req any) (server.ScopeRef, error) {
				return server.SpaceScopeFromPath("parent", req.(*iamv1.CreateMemberRequest).GetParent())
			},
		},
		"/pivox.api.v1.Spaces/UpdateMember": {
			Permission: permission.MembersUpdate,
			Extract: func(req any) (server.ScopeRef, error) {
				return server.SpaceScopeFromPath("member.name", req.(*iamv1.UpdateMemberRequest).GetMember().GetName())
			},
		},
		"/pivox.api.v1.Spaces/DeleteMember": {
			Permission: permission.MembersDelete,
			Extract: func(req any) (server.ScopeRef, error) {
				return server.SpaceScopeFromPath("name", req.(*iamv1.DeleteMemberRequest).GetName())
			},
		},
	}
}

// ExemptMethods returns the explicit allowlist of Spaces RPCs that
// the permission interceptor passes through without a check.
//
// TestIamPermissions answers "which permissions do I have here?" —
// gating it would be circular. The handler does its own caller
// resolution. Same SECURITY constraint as the org-scope variant on
// Organizations.
func ExemptMethods() map[string]bool {
	return map[string]bool{
		"/pivox.api.v1.Spaces/TestIamPermissions": true,
	}
}
