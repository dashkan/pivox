// Package permission encodes the v1 access-control surface — the
// role-to-permission grant data for the 4 system roles plus a
// resolver that determines whether a caller has a given permission
// against a target scope (org or space). Permissions resolve via the
// `role_permissions` table (joining org_members, space_members, and
// group_members against it), not against an in-code matrix.
//
// The catalog and grant data live in `permissions.yaml` (the source
// of truth). `permissions_gen.go` is regenerated from it by
// `cmd/gen-permissions`; run `make generate` after editing the YAML.
//
// v1 ships system roles only — owner, admin, editor, viewer. Custom
// roles are deferred per the IAM roadmap; the resolver already reads
// role_permissions by role_id, so it resolves custom roles identically
// when they land.
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
