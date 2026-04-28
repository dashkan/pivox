-- name: GetEffectiveOrgRoles :many
-- Returns the system-role names a Firebase identity has at the given
-- org, considering both direct user bindings and group-derived
-- bindings (groups the user is a member of, which themselves have
-- org_members rows). Custom roles are excluded — v1 only resolves
-- against the system-role permission matrix.
--
-- Used by the permission resolver as the org-scope half of effective-
-- role resolution. Space-scope inheritance is handled at the resolver
-- layer by unioning this with `GetEffectiveSpaceRoles`.
--
-- Returns the empty set if the firebase_identity has no live user row
-- in the org (caller is not a member, or membership was soft-deleted).
WITH caller AS (
  SELECT id FROM users
   WHERE org_id = sqlc.arg(org_id)
     AND firebase_identity_id = sqlc.arg(firebase_identity_id)
     AND delete_time IS NULL
)
SELECT DISTINCT r.name
  FROM org_members om
  JOIN roles r ON r.id = om.role_id
 WHERE om.org_id = sqlc.arg(org_id)
   -- v1 only resolves system roles against the in-memory matrix.
   -- v2 (custom roles): drop this filter AND teach the resolver to
   -- look up role_permissions rows for the non-system roles in the
   -- result set; without that change, custom-role bindings would
   -- silently never grant any permission.
   AND r.is_system = true
   AND (
     (om.principal_kind = 'user'
      AND om.principal_id IN (SELECT id FROM caller))
     OR
     (om.principal_kind = 'group'
      AND om.principal_id IN (
        SELECT gm.group_id
          FROM group_members gm
         WHERE gm.user_id IN (SELECT id FROM caller)
      ))
   );

-- name: GetEffectiveSpaceRoles :many
-- Returns the system-role names a Firebase identity has at the given
-- space — direct + group-derived space-level bindings only. Org-level
-- inheritance (an org-admin is also a space-admin) is the resolver's
-- responsibility to union in via GetEffectiveOrgRoles against the
-- space's parent org.
--
-- Returns the empty set if the firebase_identity has no live user row
-- in the org that owns this space.
WITH caller AS (
  SELECT u.id
    FROM users u
    JOIN spaces s ON s.org_id = u.org_id
   WHERE s.id = sqlc.arg(space_id)
     AND u.firebase_identity_id = sqlc.arg(firebase_identity_id)
     AND u.delete_time IS NULL
)
SELECT DISTINCT r.name
  FROM space_members sm
  JOIN roles r ON r.id = sm.role_id
 WHERE sm.space_id = sqlc.arg(space_id)
   -- v1 only resolves system roles against the in-memory matrix.
   -- v2 (custom roles): drop this filter AND teach the resolver to
   -- look up role_permissions rows for the non-system roles in the
   -- result set; without that change, custom-role bindings would
   -- silently never grant any permission.
   AND r.is_system = true
   AND (
     (sm.principal_kind = 'user'
      AND sm.principal_id IN (SELECT id FROM caller))
     OR
     (sm.principal_kind = 'group'
      AND sm.principal_id IN (
        SELECT gm.group_id
          FROM group_members gm
         WHERE gm.user_id IN (SELECT id FROM caller)
      ))
   );

-- name: CreateOrgMember :exec
-- Inserts an org-level role binding. Caller assigns the id; the
-- schema's `uuidv7()` default applies only when omitted, but we
-- always pass an explicit id for symmetry with CreateOrganization
-- and CreateUserMembership.
INSERT INTO org_members (id, org_id, role_id, principal_kind, principal_id, created_by)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: GetSpaceParentOrg :one
-- Resolves a space's parent org_id. Used by the permission resolver
-- when a space-scoped permission check needs to fold in org-level
-- inheritance.
SELECT org_id FROM spaces WHERE id = $1 AND delete_time IS NULL;
