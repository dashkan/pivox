package permission

// Compile-time-checked permission_id constants. Every entry seeded by
// `internal/db/migrations/000001_init.up.sql` MUST have a matching
// constant here, and every constant here MUST appear in the migration's
// `INSERT INTO permissions` blocks. The drift-guard test
// `TestPermissions_MigrationMatchesConstants` parses the migration and
// fails the build if either side is out of sync.
//
// Why exist: handler code (and the matrix below) referenced these IDs
// as raw strings. A typo in any consumer (`"organization.delete"`)
// silently never matches and silently denies — exactly the failure
// mode that's hardest to notice. Routing every reference through these
// constants makes typos compile errors.
//
// Naming convention: dot-delimited permission_id transliterated to
// CamelCase. `organizations.delete` → `OrganizationsDelete`;
// `assets.lineItems.fulfill` → `AssetsLineItemsFulfill`;
// `organizations.ssoConfig.update` → `OrganizationsSsoConfigUpdate`.
const (
	// Organization management
	OrganizationsGet    = "organizations.get"
	OrganizationsUpdate = "organizations.update"
	OrganizationsDelete = "organizations.delete"

	// Space creation (within-space access uses the space-role matrix)
	SpacesCreate = "spaces.create"

	// User management
	UsersGet    = "users.get"
	UsersList   = "users.list"
	UsersDelete = "users.delete"

	// Group management
	GroupsCreate        = "groups.create"
	GroupsGet           = "groups.get"
	GroupsList          = "groups.list"
	GroupsUpdate        = "groups.update"
	GroupsDelete        = "groups.delete"
	GroupsManageMembers = "groups.manageMembers"

	// Role management (custom roles deferred to v2; create/update/
	// delete/manageMembers granted to admin in the matrix as forward-
	// compatible scaffolding — handlers must Unimplemented until v2)
	RolesCreate        = "roles.create"
	RolesGet           = "roles.get"
	RolesList          = "roles.list"
	RolesUpdate        = "roles.update"
	RolesDelete        = "roles.delete"
	RolesManageMembers = "roles.manageMembers"

	// Invitation management
	InvitationsCreate       = "invitations.create"
	InvitationsGet          = "invitations.get"
	InvitationsList         = "invitations.list"
	InvitationsDelete       = "invitations.delete"
	InvitationsUpdatePolicy = "invitations.updatePolicy"

	// API key management
	ApiKeysCreate = "apikeys.create"
	ApiKeysGet    = "apikeys.get"
	ApiKeysUpdate = "apikeys.update"
	ApiKeysDelete = "apikeys.delete"

	// Domain management
	DomainsCreate = "domains.create"
	DomainsGet    = "domains.get"
	DomainsList   = "domains.list"
	DomainsDelete = "domains.delete"

	// SSO config (singleton sub-resource of Organization)
	OrganizationsSsoConfigGet    = "organizations.ssoConfig.get"
	OrganizationsSsoConfigUpdate = "organizations.ssoConfig.update"

	// Member (role bindings at org and space scope)
	MembersCreate            = "members.create"
	MembersGet               = "members.get"
	MembersList              = "members.list"
	MembersUpdate            = "members.update"
	MembersDelete            = "members.delete"
	MembersTransferOwnership = "members.transferOwnership"

	// Storage gateway management
	StorageGatewaysCreate  = "storage.gateways.create"
	StorageGatewaysGet     = "storage.gateways.get"
	StorageGatewaysUpdate  = "storage.gateways.update"
	StorageGatewaysDelete  = "storage.gateways.delete"
	StorageGatewaysUpgrade = "storage.gateways.upgrade"
	StorageEndpointsCreate = "storage.endpoints.create"
	StorageEndpointsGet    = "storage.endpoints.get"
	StorageEndpointsUpdate = "storage.endpoints.update"
	StorageEndpointsDelete = "storage.endpoints.delete"
	StorageAgentsGet       = "storage.agents.get"
	StorageAgentsDrain     = "storage.agents.drain"
	StorageAgentsRemove    = "storage.agents.remove"

	// Assets — assets
	AssetsAssetsGet      = "assets.assets.get"
	AssetsAssetsList     = "assets.assets.list"
	AssetsAssetsCreate   = "assets.assets.create"
	AssetsAssetsUpdate   = "assets.assets.update"
	AssetsAssetsDelete   = "assets.assets.delete"
	AssetsAssetsUndelete = "assets.assets.undelete"
	AssetsAssetsImport   = "assets.assets.import"

	// Assets — requests
	AssetsRequestsGet     = "assets.requests.get"
	AssetsRequestsList    = "assets.requests.list"
	AssetsRequestsCreate  = "assets.requests.create"
	AssetsRequestsUpdate  = "assets.requests.update"
	AssetsRequestsDelete  = "assets.requests.delete"
	AssetsRequestsAssign  = "assets.requests.assign"
	AssetsRequestsClaim   = "assets.requests.claim"
	AssetsRequestsSubmit  = "assets.requests.submit"
	AssetsRequestsDeliver = "assets.requests.deliver"
	AssetsRequestsApprove = "assets.requests.approve"
	AssetsRequestsReject  = "assets.requests.reject"
	AssetsRequestsCancel  = "assets.requests.cancel"

	// Assets — line items
	AssetsLineItemsGet     = "assets.lineItems.get"
	AssetsLineItemsList    = "assets.lineItems.list"
	AssetsLineItemsCreate  = "assets.lineItems.create"
	AssetsLineItemsUpdate  = "assets.lineItems.update"
	AssetsLineItemsDelete  = "assets.lineItems.delete"
	AssetsLineItemsFulfill = "assets.lineItems.fulfill"

	// AI chat
	AiConversationsGet       = "ai.conversations.get"
	AiConversationsList      = "ai.conversations.list"
	AiConversationsCreate    = "ai.conversations.create"
	AiConversationsUpdate    = "ai.conversations.update"
	AiConversationsDelete    = "ai.conversations.delete"
	AiMessagesGet            = "ai.messages.get"
	AiMessagesList           = "ai.messages.list"
	AiArtifactsGet           = "ai.artifacts.get"
	AiArtifactsList          = "ai.artifacts.list"
	AiArtifactsDelete        = "ai.artifacts.delete"
	AiArtifactVersionsGet    = "ai.artifactVersions.get"
	AiArtifactVersionsList   = "ai.artifactVersions.list"
	AiArtifactVersionsDelete = "ai.artifactVersions.delete"
	AiChatStream             = "ai.chat.stream"
)

// All is the canonical, ordered list of every permission_id known to
// the v1 system. Used by the drift-guard test to compare against the
// migration's seeded set, and by `TestPermissions` callers that need
// the full catalog (e.g., to ask "which of these am I allowed to
// call?" for UI gating).
var All = []string{
	OrganizationsGet, OrganizationsUpdate, OrganizationsDelete,
	SpacesCreate,
	UsersGet, UsersList, UsersDelete,
	GroupsCreate, GroupsGet, GroupsList, GroupsUpdate, GroupsDelete, GroupsManageMembers,
	RolesCreate, RolesGet, RolesList, RolesUpdate, RolesDelete, RolesManageMembers,
	InvitationsCreate, InvitationsGet, InvitationsList, InvitationsDelete, InvitationsUpdatePolicy,
	ApiKeysCreate, ApiKeysGet, ApiKeysUpdate, ApiKeysDelete,
	DomainsCreate, DomainsGet, DomainsList, DomainsDelete,
	OrganizationsSsoConfigGet, OrganizationsSsoConfigUpdate,
	MembersCreate, MembersGet, MembersList, MembersUpdate, MembersDelete, MembersTransferOwnership,
	StorageGatewaysCreate, StorageGatewaysGet, StorageGatewaysUpdate, StorageGatewaysDelete, StorageGatewaysUpgrade,
	StorageEndpointsCreate, StorageEndpointsGet, StorageEndpointsUpdate, StorageEndpointsDelete,
	StorageAgentsGet, StorageAgentsDrain, StorageAgentsRemove,
	AssetsAssetsGet, AssetsAssetsList, AssetsAssetsCreate, AssetsAssetsUpdate, AssetsAssetsDelete, AssetsAssetsUndelete, AssetsAssetsImport,
	AssetsRequestsGet, AssetsRequestsList, AssetsRequestsCreate, AssetsRequestsUpdate, AssetsRequestsDelete,
	AssetsRequestsAssign, AssetsRequestsClaim, AssetsRequestsSubmit, AssetsRequestsDeliver,
	AssetsRequestsApprove, AssetsRequestsReject, AssetsRequestsCancel,
	AssetsLineItemsGet, AssetsLineItemsList, AssetsLineItemsCreate, AssetsLineItemsUpdate, AssetsLineItemsDelete, AssetsLineItemsFulfill,
	AiConversationsGet, AiConversationsList, AiConversationsCreate, AiConversationsUpdate, AiConversationsDelete,
	AiMessagesGet, AiMessagesList,
	AiArtifactsGet, AiArtifactsList, AiArtifactsDelete,
	AiArtifactVersionsGet, AiArtifactVersionsList, AiArtifactVersionsDelete,
	AiChatStream,
}
