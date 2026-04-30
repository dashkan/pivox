-- name: GetEffectiveOrgRoles :many
-- Returns the system-role names an identity has at the given org,
-- considering both direct user bindings (org_members.user_id) and
-- group-derived bindings (org_members.group_id matching a group the
-- user is a member of via group_members). Custom roles are excluded
-- — v1 only resolves against the system-role permission matrix.
--
-- Used by the permission resolver as the org-scope half of effective-
-- role resolution. Space-scope inheritance is handled at the resolver
-- layer by unioning this with `GetEffectiveSpaceRoles`.
--
-- Post-principal-split: the polymorphic `principal_kind/principal_id`
-- pair was replaced by typed `user_id`/`group_id` columns (XOR
-- enforced at the row level). The OR branches below select on the
-- live column for each binding shape.
SELECT DISTINCT r.name
  FROM org_members om
  JOIN roles r ON r.id = om.role_id
 WHERE om.org_id = sqlc.arg(org_id)
   AND r.is_system = true
   AND (
     om.user_id = sqlc.arg(identity_id)
     OR
     om.group_id IN (
       SELECT gm.group_id
         FROM group_members gm
        WHERE gm.user_id = sqlc.arg(identity_id)
     )
   );

-- name: GetEffectiveSpaceRoles :many
-- Returns the system-role names an identity has at the given space —
-- direct + group-derived space-level bindings only. Org-level
-- inheritance (an org-admin is also a space-admin) is the resolver's
-- responsibility to union in via GetEffectiveOrgRoles against the
-- space's parent org.
SELECT DISTINCT r.name
  FROM space_members sm
  JOIN roles r ON r.id = sm.role_id
 WHERE sm.space_id = sqlc.arg(space_id)
   AND r.is_system = true
   AND (
     sm.user_id = sqlc.arg(identity_id)
     OR
     sm.group_id IN (
       SELECT gm.group_id
         FROM group_members gm
        WHERE gm.user_id = sqlc.arg(identity_id)
     )
   );

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

-- name: GetIdentityForMember :one
-- Verifies that an identity row exists for the given uuid. Used by
-- Member create handlers as the principal-existence check before
-- inserting a binding. The org_members.user_id column DOES carry an
-- FK now (post-split), so an INSERT against a non-existent
-- identity_id would fail with a constraint violation — this query
-- is kept to surface the failure as a clean NotFound at the gRPC
-- layer rather than letting the FK error bubble up as Internal.
SELECT * FROM identities WHERE id = $1;

-- name: GetGroupByID :one
-- Companion to GetIdentityForMember for groups.
SELECT * FROM groups
 WHERE id = $1
   AND org_id = $2;

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

-- name: GetSpaceParentOrg :one
-- Resolves a space's parent org_id. Used by the permission resolver
-- when a space-scoped permission check needs to fold in org-level
-- inheritance.
--
-- Returns the parent org regardless of the space's soft-delete state:
-- the parent relationship is immutable, and the resolver runs for
-- soft-deleted spaces too (UndeleteSpace, reads during the grace
-- window). Filtering on delete_time would break those flows by
-- returning ErrNoRows after the gate has already admitted the row.
SELECT org_id FROM spaces WHERE id = $1;
