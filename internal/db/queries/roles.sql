-- name: CreateRole :exec
-- Inserts a role row. Used by CreateOrganization to seed the 4 system
-- roles per org at create time. Will also be used in v2 by the
-- future Iam.CreateRole RPC for custom roles. Caller assigns the id;
-- the schema's `uuidv7()` default applies only when omitted, but we
-- always pass an explicit id so the caller can FK to it without a
-- read-back round-trip.
INSERT INTO roles (id, org_id, name, display_name, description, is_system)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: GetSystemRole :one
-- Looks up a system role by (org, name) — the canonical way to
-- resolve the role id for a known system role like 'owner'. Returns
-- only system roles; custom roles (when v2 ships) are excluded so a
-- collision-named custom role can't shadow the system one.
SELECT * FROM roles
 WHERE org_id = $1
   AND name = $2
   AND is_system = true;

-- name: ListRolesByOrg :many
SELECT * FROM roles
 WHERE org_id = $1
 ORDER BY is_system DESC, name ASC;

-- name: GetRoleByID :one
SELECT * FROM roles
 WHERE id = $1;

-- name: GrantPermissionsToRole :exec
-- Materializes a role's permission grants into role_permissions from a
-- list of catalog permission_id strings (e.g. "organizations.read").
-- Used at org bootstrap (and the dev seed) to write the static
-- system-role grant matrix (permission.RoleGrants) into the DB, so
-- permission checks can also resolve in SQL — e.g. the membership-
-- scoped operations list — without N+1 Has() calls. Joining by the
-- stable permission_id string means callers never need the permission
-- UUIDs. ON CONFLICT makes re-seeding idempotent.
INSERT INTO role_permissions (role_id, permission_id)
SELECT sqlc.arg(role_id), p.id
  FROM permissions p
 WHERE p.permission_id = ANY(sqlc.arg(permission_ids)::text[])
ON CONFLICT DO NOTHING;

-- name: RolePermissionIDs :many
-- Returns the catalog permission_id strings granted to a role via
-- role_permissions (inverse of GrantPermissionsToRole). Used to assert
-- bootstrap/seed populated the grant rows correctly.
SELECT p.permission_id
  FROM role_permissions rp
  JOIN permissions p ON p.id = rp.permission_id
 WHERE rp.role_id = sqlc.arg(role_id)
 ORDER BY p.permission_id;
