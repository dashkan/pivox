// Package permission encodes the v1 access-control surface — the
// role-to-permission grant matrix for the 4 system roles plus a
// resolver that determines whether a caller has a given permission
// against a target scope (org or space) by joining org_members,
// space_members, group_members, and the matrix.
//
// The catalog and matrix data live in `permissions.yaml` (the source
// of truth). `permissions_gen.go` is regenerated from it by
// `cmd/gen-permissions`; run `make generate` after editing the YAML.
//
// v1 ships system roles only — owner, admin, editor, viewer. Custom
// roles are deferred per the IAM roadmap; this package will gain a
// per-org dynamic permission lookup when they land.
package permission

//go:generate go run ../../cmd/gen-permissions

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
