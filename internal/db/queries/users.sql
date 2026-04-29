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

-- ListSoleOwnerOrgsForFirebaseIdentity returns the set of active orgs
-- where this firebase_identity is the ONLY owner. Used by DeleteUser's
-- VALIDATING phase to refuse deletion when the caller would leave any
-- org without an owner. Empty result means deletion is safe.
--
-- The `having count(*) = 1` clause runs over org_members keyed on the
-- system 'owner' role for that org; `bool_or` then asserts the single
-- owner is THIS firebase_identity (true for exactly one row in the
-- group, false otherwise). Soft-deleted orgs and soft-deleted users
-- are excluded.
-- name: ListSoleOwnerOrgsForFirebaseIdentity :many
SELECT o.*
  FROM organizations o
 WHERE o.delete_time IS NULL
   AND o.id IN (
     SELECT om.org_id
       FROM org_members om
       JOIN roles r ON r.id = om.role_id
       JOIN users u ON u.id = om.principal_id
      WHERE om.principal_kind = 'user'
        AND r.is_system = true
        AND r.name = 'owner'
        AND u.delete_time IS NULL
      GROUP BY om.org_id
     HAVING count(*) = 1
        AND bool_or(u.firebase_identity_id = $1) = true
   );

-- DeleteOrgMembersForFirebaseIdentity removes all org-scope role
-- bindings for users owned by this firebase_identity. The DELETE is
-- explicit because org_members.principal_id has no FK on users
-- (principal_kind discriminates user vs group).
-- name: DeleteOrgMembersForFirebaseIdentity :exec
DELETE FROM org_members
 WHERE principal_kind = 'user'
   AND principal_id IN (
     SELECT id FROM users WHERE firebase_identity_id = $1
   );

-- DeleteSpaceMembersForFirebaseIdentity is the space-scope analogue.
-- name: DeleteSpaceMembersForFirebaseIdentity :exec
DELETE FROM space_members
 WHERE principal_kind = 'user'
   AND principal_id IN (
     SELECT id FROM users WHERE firebase_identity_id = $1
   );

-- HardDeleteFirebaseIdentity removes the firebase_identity row.
-- Cascade chain (via FK ON DELETE CASCADE):
--   firebase_identities → users → group_members
-- Explicit revocation queries above handle org_members and
-- space_members because their `principal_id` is unFK'd (the
-- principal_kind discriminator means we can't add a FK directly).
-- Called as the second-to-last step of DeleteUser; the Firebase
-- Auth identity itself is deleted last so a partial failure leaves
-- a recoverable Firebase identity rather than orphaned Pivox state.
-- name: HardDeleteFirebaseIdentity :exec
DELETE FROM firebase_identities WHERE id = $1;
