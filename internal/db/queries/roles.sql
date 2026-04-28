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
