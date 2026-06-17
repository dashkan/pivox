-- name: GetOrgMemberByUser :one
-- Looks up a single org-scope user binding. After the principal_id
-- split, user and group lookups are separate queries — the
-- predicate uses the typed column directly, and the filtered unique
-- indexes on (org_id, user_id) / (org_id, group_id) make these
-- lookups index-only.
SELECT om.*, r.name AS role_name
  FROM org_members om
  JOIN roles r ON r.id = om.role_id
 WHERE om.org_id = $1
   AND om.user_id = $2;

-- name: GetOrgMemberByGroup :one
SELECT om.*, r.name AS role_name
  FROM org_members om
  JOIN roles r ON r.id = om.role_id
 WHERE om.org_id = $1
   AND om.group_id = $2;

-- name: ListOrgMembers :many
-- Lists org-scope role bindings for an org with offset-based
-- pagination. Ordered by (create_time, id) so paging is stable under
-- concurrent inserts. Caller asks for limit+1 rows to detect "more
-- pages exist" without a separate count query; the handler trims the
-- extra row before responding.
SELECT om.*, r.name AS role_name
  FROM org_members om
  JOIN roles r ON r.id = om.role_id
 WHERE om.org_id = $1
 ORDER BY om.create_time, om.id
 OFFSET sqlc.arg('offset')::bigint
 LIMIT sqlc.arg('limit')::bigint;

-- name: GetSpaceMemberByUser :one
SELECT sm.*, r.name AS role_name
  FROM space_members sm
  JOIN roles r ON r.id = sm.role_id
 WHERE sm.space_id = $1
   AND sm.user_id = $2;

-- name: GetSpaceMemberByGroup :one
SELECT sm.*, r.name AS role_name
  FROM space_members sm
  JOIN roles r ON r.id = sm.role_id
 WHERE sm.space_id = $1
   AND sm.group_id = $2;

-- name: ListSpaceMembers :many
SELECT sm.*, r.name AS role_name
  FROM space_members sm
  JOIN roles r ON r.id = sm.role_id
 WHERE sm.space_id = $1
 ORDER BY sm.create_time, sm.id
 OFFSET sqlc.arg('offset')::bigint
 LIMIT sqlc.arg('limit')::bigint;

-- name: CreateOrgUserMember :one
-- Inserts a user binding. The XOR check on the table guarantees
-- group_id stays NULL.
INSERT INTO org_members (id, org_id, role_id, user_id, created_by)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, etag, create_time, update_time;

-- name: CreateOrgGroupMember :one
INSERT INTO org_members (id, org_id, role_id, group_id, created_by)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, etag, create_time, update_time;

-- name: CreateSpaceUserMember :one
INSERT INTO space_members (id, space_id, role_id, user_id, created_by)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, etag, create_time, update_time;

-- name: CreateSpaceGroupMember :one
INSERT INTO space_members (id, space_id, role_id, group_id, created_by)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, etag, create_time, update_time;

-- name: UpdateOrgUserMemberRole :one
-- Mutates only the role; principal and scope are immutable. Bumps
-- revision + etag. Returns the new etag + timestamps so the handler
-- can build the Member proto response without a follow-up
-- GetOrgMember* round-trip.
UPDATE org_members
   SET role_id = $3,
       update_time = now(),
       revision = revision + 1,
       etag = md5(now()::text)
 WHERE org_id = $1
   AND user_id = $2
RETURNING id, etag, create_time, update_time;

-- name: UpdateOrgGroupMemberRole :one
UPDATE org_members
   SET role_id = $3,
       update_time = now(),
       revision = revision + 1,
       etag = md5(now()::text)
 WHERE org_id = $1
   AND group_id = $2
RETURNING id, etag, create_time, update_time;

-- name: UpdateSpaceUserMemberRole :one
UPDATE space_members
   SET role_id = $3,
       update_time = now(),
       revision = revision + 1,
       etag = md5(now()::text)
 WHERE space_id = $1
   AND user_id = $2
RETURNING id, etag, create_time, update_time;

-- name: UpdateSpaceGroupMemberRole :one
UPDATE space_members
   SET role_id = $3,
       update_time = now(),
       revision = revision + 1,
       etag = md5(now()::text)
 WHERE space_id = $1
   AND group_id = $2
RETURNING id, etag, create_time, update_time;

-- name: DeleteOrgUserMember :execrows
-- Returns the affected-row count so the handler can map "not found"
-- (0 rows) to gRPC NotFound rather than treating it as success.
DELETE FROM org_members
 WHERE org_id = $1
   AND user_id = $2;

-- name: DeleteOrgGroupMember :execrows
DELETE FROM org_members
 WHERE org_id = $1
   AND group_id = $2;

-- name: DeleteSpaceUserMember :execrows
DELETE FROM space_members
 WHERE space_id = $1
   AND user_id = $2;

-- name: DeleteSpaceGroupMember :execrows
DELETE FROM space_members
 WHERE space_id = $1
   AND group_id = $2;

-- name: ListOrgOwnerMembers :many
-- Returns all org_members rows currently bound to the system 'owner'
-- role for the given org. Used by TransferOwnership to find the
-- current owner(s) to demote; in normal operation returns ≥1 row.
SELECT om.id, om.org_id, om.role_id, om.user_id, om.group_id,
       om.etag, om.revision, om.created_by, om.updated_by,
       om.create_time, om.update_time
  FROM org_members om
  JOIN roles r ON r.id = om.role_id
 WHERE om.org_id = $1
   AND r.is_system = true
   AND r.name = 'owner'
 ORDER BY om.create_time, om.id;

-- name: ListSpaceMembershipsForIdentityInOrg :many
-- Returns the spaces in `org_id` that `identity_id` is a member of —
-- via direct user binding (space_members.user_id) OR group-derived
-- binding (space_members.group_id where the user is in that group).
-- Mirrors GetEffectiveSpaceRoles' resolution shape but inverts the
-- direction: instead of "for this (user, space), what roles?" it
-- asks "for this (user, org), which spaces?".
--
-- Used by CreateStorageSession (#27 phase 2) to derive the per-space
-- prefix patterns the storage agent will glob-match against incoming
-- request URLs. Excludes soft-deleted spaces (a session that
-- authorizes paths under a deleted space would be a bug).
--
-- NOTE: this query does NOT honor org-role inheritance — an
-- org-owner with no direct space_members row gets zero results here.
-- That's the intentional #27-phase-2 scope; org-role inheritance is
-- a follow-up.
SELECT DISTINCT s.id, s.org_id, s.name
  FROM spaces s
  JOIN space_members sm ON sm.space_id = s.id
 WHERE s.org_id = sqlc.arg(org_id)
   AND s.delete_time IS NULL
   AND (
     sm.user_id = sqlc.arg(identity_id)
     OR
     sm.group_id IN (
       SELECT gm.group_id
         FROM group_members gm
        WHERE gm.user_id = sqlc.arg(identity_id)
     )
   )
 ORDER BY s.name;

-- name: EffectiveOrgPermissions :many
-- Catalog permission_id strings the identity holds at the org, resolved
-- via role_permissions (direct user bindings + group-derived). DB-side
-- source of truth for authorization; resolves system + custom roles
-- identically (no is_system filter — role_permissions is keyed by role_id).
SELECT DISTINCT perm.permission_id
  FROM org_members om
  JOIN role_permissions rp ON rp.role_id = om.role_id
  JOIN permissions perm ON perm.id = rp.permission_id
 WHERE om.org_id = sqlc.arg(org_id)
   AND ( om.user_id = sqlc.arg(identity_id)
         OR om.group_id IN (
              SELECT gm.group_id FROM group_members gm
               WHERE gm.user_id = sqlc.arg(identity_id) ) );

-- name: EffectiveSpacePermissions :many
-- Catalog permission_id strings the identity holds at the space: direct
-- space-level bindings UNION inherited parent-org bindings, via
-- role_permissions. One query (replaces the old parent-org lookup +
-- space-roles + org-roles round-trips).
SELECT DISTINCT perm.permission_id
  FROM permissions perm
 WHERE perm.id IN (
   SELECT rp.permission_id
     FROM space_members sm
     JOIN role_permissions rp ON rp.role_id = sm.role_id
    WHERE sm.space_id = sqlc.arg(space_id)
      AND ( sm.user_id = sqlc.arg(identity_id)
            OR sm.group_id IN (
                 SELECT gm.group_id FROM group_members gm
                  WHERE gm.user_id = sqlc.arg(identity_id) ) )
   UNION
   SELECT rp.permission_id
     FROM spaces s
     JOIN org_members om ON om.org_id = s.org_id
     JOIN role_permissions rp ON rp.role_id = om.role_id
    WHERE s.id = sqlc.arg(space_id)
      AND ( om.user_id = sqlc.arg(identity_id)
            OR om.group_id IN (
                 SELECT gm.group_id FROM group_members gm
                  WHERE gm.user_id = sqlc.arg(identity_id) ) )
 );
