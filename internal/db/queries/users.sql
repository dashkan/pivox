-- name: CreateUserMembership :one
-- Creates a per-org identity row joining a firebase_identity to an
-- org. Role bindings live in `org_members`, not on this table; the
-- caller follows up with InsertOrgMember to bind the new user to a
-- role. Used by `CreateOrganization` (founder) and the future
-- `AcceptInvitation` flow (invitee).
INSERT INTO users (id, org_id, firebase_identity_id)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetUserMembership :one
SELECT * FROM users
 WHERE org_id = $1
   AND firebase_identity_id = $2
   AND delete_time IS NULL;

-- name: ListUsersByOrg :many
SELECT * FROM users
 WHERE org_id = $1
   AND delete_time IS NULL
 ORDER BY create_time;

-- name: ListUsersByFirebaseIdentity :many
-- Lists all live org memberships for a firebase_identity, excluding
-- memberships in soft-deleted orgs and soft-deleted user rows. Used
-- by the membership interceptor's gate and by any consumer that
-- needs the "is this caller in any active org?" signal.
SELECT u.*
  FROM users u
  JOIN organizations o ON o.id = u.org_id
 WHERE u.firebase_identity_id = $1
   AND u.delete_time IS NULL
   AND o.delete_time IS NULL
 ORDER BY u.create_time;

-- name: CountOwnersByOrg :one
-- Used by membership-mutation handlers to enforce "≥1 owner" — call
-- before any role-change or delete that would reduce the owner count.
-- Counts org_members rows whose role is the system 'owner' role for
-- this org and whose principal is a (non-deleted) user. Keys on the
-- stable `roles.name` slug, not display_name — display_name is mutable
-- and i18n-eligible, name is the machine identifier.
SELECT COUNT(*)
  FROM org_members om
  JOIN roles r ON r.id = om.role_id
  JOIN users u ON u.id = om.principal_id
 WHERE om.org_id = $1
   AND om.principal_kind = 'user'
   AND r.is_system = true
   AND r.name = 'owner'
   AND u.delete_time IS NULL;

-- name: DeleteUserMembership :exec
-- Hard-delete used by tests + the DeleteUser LRO post-soft-delete
-- purge. For ordinary user removal, prefer SoftDeleteUserMembership.
DELETE FROM users WHERE org_id = $1 AND firebase_identity_id = $2;

-- name: SoftDeleteUserMembership :exec
UPDATE users
   SET delete_time = now(),
       purge_time = now() + INTERVAL '30 days',
       update_time = now(),
       revision = revision + 1
 WHERE org_id = $1 AND firebase_identity_id = $2;

-- name: ListOrganizationsForFirebaseIdentity :many
-- Lists all organizations the given firebase_identity has membership in.
-- Caller-scoped for `ListOrganizations`: every authenticated user is
-- only ever shown orgs they belong to. Excludes soft-deleted orgs and
-- soft-deleted user rows. No pagination — typical users are in 1-3
-- orgs. The 1000-row LIMIT is a defensive backstop.
SELECT o.*
  FROM organizations o
  JOIN users u ON u.org_id = o.id
 WHERE u.firebase_identity_id = $1
   AND u.delete_time IS NULL
   AND o.delete_time IS NULL
 ORDER BY o.id ASC
 LIMIT 1000;
