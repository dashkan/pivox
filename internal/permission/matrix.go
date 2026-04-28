// Package permission encodes the v1 access-control surface: the
// (role, permission) → allow matrix for the 4 system roles, plus a
// resolver that determines whether a caller has a given permission
// against a target scope (org or space) by joining org_members,
// space_members, group_members, and the matrix.
//
// v1 ships with system roles only — owner, admin, editor, viewer.
// Custom roles are deferred per the IAM roadmap; this package will
// gain a per-org dynamic permission lookup when they land.
package permission

// Role names. Values match the seeded `roles.name` slug for system
// roles created per-org at CreateOrganization time.
const (
	RoleOwner  = "owner"
	RoleAdmin  = "admin"
	RoleEditor = "editor"
	RoleViewer = "viewer"
)

// Has reports whether `role` (a system-role name) grants `permission`
// (a permission_id from the seeded catalog). Unknown role or
// permission denies.
//
// This is the only entry point for permission checks against the
// static v1 matrix; callers that need to resolve effective roles for
// a principal should use Resolver, which composes Has with the
// org_members / space_members / group_members lookups.
func Has(role, permission string) bool {
	perms, ok := matrix[role]
	if !ok {
		return false
	}
	return perms[permission]
}

// matrix encodes the entire v1 permission grant. Each entry is a role
// name → set-of-permission-ids the role grants. New permissions added
// to the seeded catalog must appear here; the
// `TestMatrix_AllSeededPermissionsCovered` test guards drift.
//
// Roles cumulate: viewer is the floor; editor extends viewer; admin
// extends editor; owner extends admin with destruction-class grants.
var matrix = map[string]map[string]bool{
	RoleOwner:  ownerPermissions(),
	RoleAdmin:  adminPermissions(),
	RoleEditor: editorPermissions(),
	RoleViewer: viewerPermissions(),
}

// ownerPermissions returns owner's grant set. Owner = admin grants
// + the destruction-class permissions admin doesn't get.
func ownerPermissions() map[string]bool {
	out := adminPermissions()
	for _, p := range []string{
		OrganizationsDelete,
		MembersTransferOwnership,
		OrganizationsSsoConfigUpdate,
		UsersDelete,
	} {
		out[p] = true
	}
	return out
}

// adminPermissions returns admin's grant set. Admin handles day-to-day
// org management — IAM, domains, SSO config (read), API keys, storage,
// invitations, content. Excludes destruction-class (org delete,
// transfer ownership, SSO update, user delete) — those go to owner.
func adminPermissions() map[string]bool {
	out := editorPermissions()
	for _, p := range []string{
		// Organization
		OrganizationsUpdate,
		SpacesCreate,
		// IAM
		MembersCreate,
		MembersGet,
		MembersList,
		MembersUpdate,
		MembersDelete,
		// Groups (mutations)
		GroupsCreate,
		GroupsUpdate,
		GroupsDelete,
		GroupsManageMembers,
		// Roles (read-only in v1; the corresponding RolesService
		// handlers must remain Unimplemented until custom roles ship
		// in v2 — these grants are forward-compatible scaffolding so
		// admin doesn't need a matrix edit when v2 lands).
		RolesCreate,
		RolesUpdate,
		RolesDelete,
		RolesManageMembers,
		// Domains
		DomainsCreate,
		DomainsGet,
		DomainsList,
		DomainsDelete,
		// SSO config (read only — update is owner-class)
		OrganizationsSsoConfigGet,
		// API keys
		ApiKeysCreate,
		ApiKeysGet,
		ApiKeysUpdate,
		ApiKeysDelete,
		// Invitations
		InvitationsCreate,
		InvitationsGet,
		InvitationsList,
		InvitationsDelete,
		InvitationsUpdatePolicy,
		// Storage gateways (mutations)
		StorageGatewaysCreate,
		StorageGatewaysUpdate,
		StorageGatewaysDelete,
		StorageGatewaysUpgrade,
		StorageEndpointsCreate,
		StorageEndpointsUpdate,
		StorageEndpointsDelete,
		StorageAgentsDrain,
		StorageAgentsRemove,
		// Assets workflow ops admin can drive on behalf of editors
		AssetsRequestsAssign,
		AssetsRequestsApprove,
		AssetsRequestsReject,
		AssetsRequestsCancel,
	} {
		out[p] = true
	}
	return out
}

// editorPermissions returns editor's grant set. Editor handles
// content — assets, requests, line items, AI chat. Read-everything
// from viewer + content mutations.
func editorPermissions() map[string]bool {
	out := viewerPermissions()
	for _, p := range []string{
		// Storage gateway read access (editors need to know which
		// endpoint backs an asset they're viewing/editing)
		StorageGatewaysGet,
		// Asset content mutations
		AssetsAssetsCreate,
		AssetsAssetsUpdate,
		AssetsAssetsDelete,
		AssetsAssetsUndelete,
		AssetsAssetsImport,
		// Asset request workflow (excluding admin-class
		// assign / approve / reject / cancel)
		AssetsRequestsCreate,
		AssetsRequestsUpdate,
		AssetsRequestsDelete,
		AssetsRequestsClaim,
		AssetsRequestsSubmit,
		AssetsRequestsDeliver,
		// Line items
		AssetsLineItemsCreate,
		AssetsLineItemsUpdate,
		AssetsLineItemsDelete,
		AssetsLineItemsFulfill,
		// AI chat mutations
		AiConversationsCreate,
		AiConversationsUpdate,
		AiConversationsDelete,
		AiArtifactsDelete,
		AiArtifactVersionsDelete,
		AiChatStream,
	} {
		out[p] = true
	}
	return out
}

// viewerPermissions returns viewer's grant set. Viewer is read-only
// across the board — sees the org, its users, groups, roles, assets,
// requests, AI conversations. No mutations, no IAM ops, no SSO/domain
// visibility.
func viewerPermissions() map[string]bool {
	out := map[string]bool{}
	for _, p := range []string{
		// Organization
		OrganizationsGet,
		// Users
		UsersGet,
		UsersList,
		// Groups (read)
		GroupsGet,
		GroupsList,
		// Roles (read)
		RolesGet,
		RolesList,
		// Storage (read access — viewers can see which gateway/
		// endpoint/agent backs an asset they're browsing)
		StorageEndpointsGet,
		StorageAgentsGet,
		// Assets (read)
		AssetsAssetsGet,
		AssetsAssetsList,
		AssetsRequestsGet,
		AssetsRequestsList,
		AssetsLineItemsGet,
		AssetsLineItemsList,
		// AI chat (read)
		AiConversationsGet,
		AiConversationsList,
		AiMessagesGet,
		AiMessagesList,
		AiArtifactsGet,
		AiArtifactsList,
		AiArtifactVersionsGet,
		AiArtifactVersionsList,
	} {
		out[p] = true
	}
	return out
}
