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

-- name: GetOrgMember :one
-- Looks up a single org-scope role binding by (org, principal). Joins
-- to roles so the caller has the role name without a second query;
-- handlers convert this row directly to the Member proto's
-- `name = organizations/{org}/members/{member}` shape.
SELECT om.*, r.name AS role_name
  FROM org_members om
  JOIN roles r ON r.id = om.role_id
 WHERE om.org_id = $1
   AND om.principal_kind = $2
   AND om.principal_id = $3;

-- name: ListOrgMembers :many
-- Lists org-scope role bindings for an org with offset-based
-- pagination. Ordered by (create_time, id) so paging is stable under
-- concurrent inserts. The handler converts AIP-132 page_token /
-- page_size into the offset / limit args here. Caller asks for
-- limit+1 rows to detect "more pages exist" without a separate count
-- query; the handler trims the extra row before responding.
SELECT om.*, r.name AS role_name
  FROM org_members om
  JOIN roles r ON r.id = om.role_id
 WHERE om.org_id = $1
 ORDER BY om.create_time, om.id
 OFFSET sqlc.arg('offset')::bigint
 LIMIT sqlc.arg('limit')::bigint;

-- name: GetSpaceMember :one
-- Companion to GetOrgMember at space scope. Note: this returns ONLY
-- direct space-level bindings; org-level inheritance (an org-admin
-- being implicitly a space-admin) is computed at the resolver layer,
-- not surfaced as a Member resource at the space scope.
SELECT sm.*, r.name AS role_name
  FROM space_members sm
  JOIN roles r ON r.id = sm.role_id
 WHERE sm.space_id = $1
   AND sm.principal_kind = $2
   AND sm.principal_id = $3;

-- name: ListSpaceMembers :many
-- Companion to ListOrgMembers at space scope. Same direct-only
-- semantic as GetSpaceMember and the same offset+limit pagination
-- contract.
SELECT sm.*, r.name AS role_name
  FROM space_members sm
  JOIN roles r ON r.id = sm.role_id
 WHERE sm.space_id = $1
 ORDER BY sm.create_time, sm.id
 OFFSET sqlc.arg('offset')::bigint
 LIMIT sqlc.arg('limit')::bigint;

-- name: CreateOrgMember :one
-- Inserts an org-level role binding and returns the server-generated
-- etag + timestamps so the handler can build the Member proto
-- response without a follow-up GetOrgMember round-trip.
INSERT INTO org_members (id, org_id, role_id, principal_kind, principal_id, created_by)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, etag, create_time, update_time;

-- name: CreateSpaceMember :one
-- Companion to CreateOrgMember at space scope.
INSERT INTO space_members (id, space_id, role_id, principal_kind, principal_id, created_by)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, etag, create_time, update_time;

-- name: UpdateOrgMemberRole :one
-- Mutates only the role; principal and scope are immutable. Bumps
-- revision + etag. Returns the new etag + timestamps so the handler
-- can build the Member proto response without a follow-up
-- GetOrgMember round-trip — the caller already knows the org slug
-- and new role name from validation.
UPDATE org_members
   SET role_id = $4,
       update_time = now(),
       revision = revision + 1,
       etag = md5(now()::text)
 WHERE org_id = $1
   AND principal_kind = $2
   AND principal_id = $3
RETURNING id, etag, create_time, update_time;

-- name: UpdateSpaceMemberRole :one
-- Companion to UpdateOrgMemberRole at space scope.
UPDATE space_members
   SET role_id = $4,
       update_time = now(),
       revision = revision + 1,
       etag = md5(now()::text)
 WHERE space_id = $1
   AND principal_kind = $2
   AND principal_id = $3
RETURNING id, etag, create_time, update_time;

-- name: DeleteOrgMember :execrows
-- Returns the affected-row count so the handler can map "not found"
-- (0 rows) to gRPC NotFound rather than treating it as success.
DELETE FROM org_members
 WHERE org_id = $1
   AND principal_kind = $2
   AND principal_id = $3;

-- name: DeleteSpaceMember :execrows
DELETE FROM space_members
 WHERE space_id = $1
   AND principal_kind = $2
   AND principal_id = $3;

-- name: GetUserByID :one
-- Verifies a user.id belongs to the given org and is not soft-deleted.
-- Used by Member create handlers to confirm the principal exists in
-- this org before inserting a binding — org_members.principal_id has
-- no FK (it's polymorphic), so the check is application-level.
SELECT * FROM users
 WHERE id = $1
   AND org_id = $2
   AND delete_time IS NULL;

-- name: GetGroupByID :one
-- Companion to GetUserByID for groups.
SELECT * FROM groups
 WHERE id = $1
   AND org_id = $2;

-- name: ListOrgOwnerMembers :many
-- Returns all org_members rows currently bound to the system 'owner'
-- role for the given org. Used by TransferOwnership to find the
-- current owner(s) to demote; in normal operation returns ≥1 row.
SELECT om.id, om.org_id, om.role_id, om.principal_kind, om.principal_id,
       om.etag, om.revision, om.created_by, om.updated_by,
       om.create_time, om.update_time
  FROM org_members om
  JOIN roles r ON r.id = om.role_id
 WHERE om.org_id = $1
   AND r.is_system = true
   AND r.name = 'owner'
 ORDER BY om.create_time, om.id;

-- name: GetSpaceParentOrg :one
-- Resolves a space's parent org_id. Used by the permission resolver
-- when a space-scoped permission check needs to fold in org-level
-- inheritance.
SELECT org_id FROM spaces WHERE id = $1 AND delete_time IS NULL;
