package organizations

import (
	"github.com/dashkan/pivox/internal/permission"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	iamv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/iam/v1"
	"github.com/dashkan/pivox/internal/server"
)

// RegistryEntries returns the permission-gating registry for every
// RPC on the Organizations service that requires an org-scope
// permission check. main.go merges this with the registries from
// other services (Iam, Spaces) before passing the union to
// server.PermissionInterceptor.
//
// Each entry's Extract closure pulls the resource path from the
// request (`name`, `parent`, or a nested message field) and routes
// it through server.OrgScopeFromPath, which performs all
// shape-validation in one place.
func RegistryEntries() server.Registry {
	return server.Registry{
		// --- Organization itself ---
		"/pivox.api.v1.Organizations/GetOrganization": {
			Permission: permission.OrganizationsRead,
			Extract: func(req any) (server.ScopeRef, error) {
				return server.OrgScopeFromPath("name", req.(*apiv1.GetOrganizationRequest).GetName())
			},
		},
		"/pivox.api.v1.Organizations/UpdateOrganization": {
			Permission: permission.OrganizationsUpdate,
			Extract: func(req any) (server.ScopeRef, error) {
				return server.OrgScopeFromPath("organization.name", req.(*apiv1.UpdateOrganizationRequest).GetOrganization().GetName())
			},
		},
		"/pivox.api.v1.Organizations/DeleteOrganization": {
			Permission: permission.OrganizationsDelete,
			Extract: func(req any) (server.ScopeRef, error) {
				return server.OrgScopeFromPath("name", req.(*apiv1.DeleteOrganizationRequest).GetName())
			},
		},
		"/pivox.api.v1.Organizations/UndeleteOrganization": {
			// Undelete reverses a destruction-class op; same tier as
			// Delete (owner-only).
			Permission: permission.OrganizationsDelete,
			Extract: func(req any) (server.ScopeRef, error) {
				return server.OrgScopeFromPath("name", req.(*apiv1.UndeleteOrganizationRequest).GetName())
			},
		},

		// --- SSO config (singleton sub-resource) ---
		"/pivox.api.v1.Organizations/GetSsoConfig": {
			Permission: permission.OrganizationsSsoConfigRead,
			Extract: func(req any) (server.ScopeRef, error) {
				return server.OrgScopeFromPath("name", req.(*apiv1.GetSsoConfigRequest).GetName())
			},
		},
		"/pivox.api.v1.Organizations/UpdateSsoConfig": {
			Permission: permission.OrganizationsSsoConfigUpdate,
			Extract: func(req any) (server.ScopeRef, error) {
				return server.OrgScopeFromPath("sso_config.name", req.(*apiv1.UpdateSsoConfigRequest).GetSsoConfig().GetName())
			},
		},

		// --- Domains ---
		"/pivox.api.v1.Organizations/CreateDomain": {
			Permission: permission.DomainsCreate,
			Extract: func(req any) (server.ScopeRef, error) {
				return server.OrgScopeFromPath("parent", req.(*apiv1.CreateDomainRequest).GetParent())
			},
		},
		"/pivox.api.v1.Organizations/ListDomains": {
			Permission: permission.DomainsRead,
			Extract: func(req any) (server.ScopeRef, error) {
				return server.OrgScopeFromPath("parent", req.(*apiv1.ListDomainsRequest).GetParent())
			},
		},
		"/pivox.api.v1.Organizations/GetDomain": {
			Permission: permission.DomainsRead,
			Extract: func(req any) (server.ScopeRef, error) {
				return server.OrgScopeFromPath("name", req.(*apiv1.GetDomainRequest).GetName())
			},
		},
		"/pivox.api.v1.Organizations/DeleteDomain": {
			Permission: permission.DomainsDelete,
			Extract: func(req any) (server.ScopeRef, error) {
				return server.OrgScopeFromPath("name", req.(*apiv1.DeleteDomainRequest).GetName())
			},
		},

		// --- Invitations ---
		"/pivox.api.v1.Organizations/CreateInvitation": {
			Permission: permission.InvitationsCreate,
			Extract: func(req any) (server.ScopeRef, error) {
				return server.OrgScopeFromPath("parent", req.(*apiv1.CreateInvitationRequest).GetParent())
			},
		},
		"/pivox.api.v1.Organizations/ListInvitations": {
			Permission: permission.InvitationsRead,
			Extract: func(req any) (server.ScopeRef, error) {
				return server.OrgScopeFromPath("parent", req.(*apiv1.ListInvitationsRequest).GetParent())
			},
		},
		"/pivox.api.v1.Organizations/DeleteInvitation": {
			Permission: permission.InvitationsDelete,
			Extract: func(req any) (server.ScopeRef, error) {
				return server.OrgScopeFromPath("name", req.(*apiv1.DeleteInvitationRequest).GetName())
			},
		},
		"/pivox.api.v1.Organizations/GetInvitationPolicy": {
			Permission: permission.InvitationsRead,
			Extract: func(req any) (server.ScopeRef, error) {
				return server.OrgScopeFromPath("name", req.(*apiv1.GetInvitationPolicyRequest).GetName())
			},
		},
		"/pivox.api.v1.Organizations/UpdateInvitationPolicy": {
			Permission: permission.InvitationsUpdatePolicy,
			Extract: func(req any) (server.ScopeRef, error) {
				return server.OrgScopeFromPath("invitation_policy.name", req.(*apiv1.UpdateInvitationPolicyRequest).GetInvitationPolicy().GetName())
			},
		},

		// --- Members (org-scope; the same RPC types are reused on
		//     Spaces with a space-scope extractor) ---
		"/pivox.api.v1.Organizations/GetMember": {
			Permission: permission.MembersRead,
			Extract: func(req any) (server.ScopeRef, error) {
				return server.OrgScopeFromPath("name", req.(*iamv1.GetMemberRequest).GetName())
			},
		},
		"/pivox.api.v1.Organizations/ListMembers": {
			Permission: permission.MembersRead,
			Extract: func(req any) (server.ScopeRef, error) {
				return server.OrgScopeFromPath("parent", req.(*iamv1.ListMembersRequest).GetParent())
			},
		},
		"/pivox.api.v1.Organizations/CreateMember": {
			Permission: permission.MembersCreate,
			Extract: func(req any) (server.ScopeRef, error) {
				return server.OrgScopeFromPath("parent", req.(*iamv1.CreateMemberRequest).GetParent())
			},
		},
		"/pivox.api.v1.Organizations/UpdateMember": {
			Permission: permission.MembersUpdate,
			Extract: func(req any) (server.ScopeRef, error) {
				return server.OrgScopeFromPath("member.name", req.(*iamv1.UpdateMemberRequest).GetMember().GetName())
			},
		},
		"/pivox.api.v1.Organizations/DeleteMember": {
			Permission: permission.MembersDelete,
			Extract: func(req any) (server.ScopeRef, error) {
				return server.OrgScopeFromPath("name", req.(*iamv1.DeleteMemberRequest).GetName())
			},
		},

		// --- Transfer ownership (org-scope; resource is the new
		//     owner's member name `organizations/{org}/members/{user}`) ---
		"/pivox.api.v1.Organizations/TransferOwnership": {
			Permission: permission.OrganizationsTransferOwnership,
			Extract: func(req any) (server.ScopeRef, error) {
				return server.OrgScopeFromPath("name", req.(*apiv1.TransferOwnershipRequest).GetName())
			},
		},
	}
}

// ExemptMethods returns the explicit allowlist of Organizations RPCs
// that the permission interceptor passes through without a check.
//
// Two categories:
//
//   - Bootstrap: caller has no org membership yet (or is acquiring
//     one), so a per-org permission check is meaningless. These
//     overlap with the membership-interceptor allowlist.
//   - TestIamPermissions: doesn't gate on a permission — it answers
//     "which permissions do I have here?". The handler resolves the
//     caller's identity itself; gating it would be circular.
//
// Adding to this list is a security-sensitive change.
func ExemptMethods() map[string]bool {
	return map[string]bool{
		"/pivox.api.v1.Organizations/CreateOrganization": true,
		"/pivox.api.v1.Organizations/ListOrganizations":  true,
		"/pivox.api.v1.Organizations/AcceptInvitation":   true,
		"/pivox.api.v1.Organizations/DeclineInvitation":  true,
		"/pivox.api.v1.Organizations/GetInvitation":      true,
		"/pivox.api.v1.Organizations/TestIamPermissions": true,
	}
}
