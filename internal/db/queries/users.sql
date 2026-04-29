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
-- Lists all org memberships for a firebase_identity, including those
-- in soft-deleted orgs so the owner can reach UndeleteOrganization
-- during the 30-day grace window. Excludes:
--   - soft-deleted user rows (the per-org membership itself was
--     soft-deleted, distinct from the org being soft-deleted), and
--   - purged orgs (the org row is hard-deleted; the JOIN naturally
--     drops those).
--
-- Used by the membership interceptor's gate, which decides whether a
-- caller is "memberful enough" to reach the permission interceptor.
-- Membership in a DELETE_REQUESTED org counts: the permission
-- interceptor's soft-delete gate then narrows allowed permissions to
-- reads + organizations.delete (which gates UndeleteOrganization),
-- so the bootstrap path stays intact without granting mutate access.
SELECT u.*
  FROM users u
  JOIN organizations o ON o.id = u.org_id
 WHERE u.firebase_identity_id = $1
   AND u.delete_time IS NULL
 ORDER BY u.create_time;

-- name: CountOwnersByOrg :one
-- Used by membership-mutation handlers to enforce "≥1 owner" — call
-- before any role-change or delete that would reduce the owner count.
-- Counts org_members rows whose role is the system 'owner' role for
-- this org, regardless of principal kind. A binding is only counted
-- when its principal is still live: users.delete_time IS NULL for
-- user principals, groups.state = 'ACTIVE' for group principals.
-- Keys on the stable `roles.name` slug, not display_name —
-- display_name is mutable and i18n-eligible.
SELECT COUNT(*)
  FROM org_members om
  JOIN roles r ON r.id = om.role_id
 WHERE om.org_id = $1
   AND r.is_system = true
   AND r.name = 'owner'
   AND (
     (om.principal_kind = 'user'
      AND EXISTS (
        SELECT 1 FROM users u
         WHERE u.id = om.principal_id
           AND u.delete_time IS NULL))
     OR
     (om.principal_kind = 'group'
      AND EXISTS (
        SELECT 1 FROM groups g
         WHERE g.id = om.principal_id
           AND g.state = 'ACTIVE'))
   );

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
-- An org is "ONLY owned by this firebase_identity" iff:
--   - exactly one live user-owner binding exists, and it points to a
--     user belonging to this firebase_identity, AND
--   - zero live group-owner bindings exist.
--
-- Live = users.delete_time IS NULL for user principals,
--        groups.state = 'ACTIVE' for group principals.
--
-- A group-owner binding (even on a group with zero members) keeps the
-- org out of this result set: the role is held by the group, so the
-- org is not "only" owned by the user. This mirrors the
-- CountOwnersByOrg invariant — both queries must count both
-- principal kinds or the ≥1-owner gate has a hole.
-- name: ListSoleOwnerOrgsForFirebaseIdentity :many
SELECT o.*
  FROM organizations o
 WHERE o.delete_time IS NULL
   AND o.id IN (
     -- exactly one user-owner, and it's this firebase_identity
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
   )
   -- no live group-owners
   AND NOT EXISTS (
     SELECT 1
       FROM org_members om2
       JOIN roles r2 ON r2.id = om2.role_id
       JOIN groups g2 ON g2.id = om2.principal_id
      WHERE om2.org_id = o.id
        AND om2.principal_kind = 'group'
        AND r2.is_system = true
        AND r2.name = 'owner'
        AND g2.state = 'ACTIVE'
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
-- Called as the second-to-last step of DeleteAccount; the Firebase
-- Auth identity itself is deleted last so a partial failure leaves
-- a recoverable Firebase identity rather than orphaned Pivox state.
-- name: HardDeleteFirebaseIdentity :exec
DELETE FROM firebase_identities WHERE id = $1;

-- ===========================================================================
-- Org-scoped cascade queries used by Iam.DeleteUser (org-scoped).
-- These are the per-org analogues of the Delete*ForFirebaseIdentity
-- queries above: they remove a single user's bindings within ONE org,
-- leaving every other org untouched. Used exclusively by DeleteUser;
-- DeleteAccount uses the cross-org variants since account deletion
-- spans every org the firebase_identity is in.
-- ===========================================================================

-- DeleteOrgMembersForUserInOrg removes the user's org-scope role
-- bindings in a single org. Bounded by (org_id, principal_id) so
-- bindings in other orgs are unaffected.
-- name: DeleteOrgMembersForUserInOrg :exec
DELETE FROM org_members
 WHERE org_id = $1
   AND principal_kind = 'user'
   AND principal_id = $2;

-- DeleteSpaceMembersForUserInOrg removes the user's space-scope
-- bindings for spaces in this org. Joins to spaces to bound by
-- org_id, since space_members rows themselves only carry space_id.
-- name: DeleteSpaceMembersForUserInOrg :exec
DELETE FROM space_members
 WHERE principal_kind = 'user'
   AND principal_id = $2
   AND space_id IN (SELECT id FROM spaces WHERE org_id = $1);

-- DeleteGroupMembersForUserInOrg removes the user's membership in
-- groups belonging to this org. group_members.user_id is the per-org
-- users.id (NOT a firebase_identity_id), matching the column name in
-- the schema. Sibling queries in this cascade family
-- (DeleteOrgMembersForUserInOrg, DeleteSpaceMembersForUserInOrg) use
-- `principal_id` because their tables discriminate user-vs-group
-- principals; group_members has no such discriminator, so the column
-- is just `user_id`. The generated sqlc struct field name therefore
-- diverges (UserID vs PrincipalID) — that's intentional and reflects
-- the schema, not a refactor leftover.
--
-- groups themselves are scoped by org_id, so the join bounds the
-- delete by org without an explicit principal-kind filter.
-- name: DeleteGroupMembersForUserInOrg :exec
DELETE FROM group_members
 WHERE user_id = $2
   AND group_id IN (SELECT id FROM groups WHERE org_id = $1);

-- SoftDeleteUserInOrg marks the per-org users row as soft-deleted
-- with the standard 30-day grace + purge_time. Mirrors the org's
-- own soft-delete pattern: the row remains queryable for grace,
-- the purge worker hard-deletes after grace expiry. Race-safe via
-- WHERE delete_time IS NULL — a second soft-delete is absorbed.
-- name: SoftDeleteUserInOrg :exec
UPDATE users
   SET delete_time = now(),
       purge_time  = now() + INTERVAL '30 days',
       update_time = now(),
       revision    = revision + 1,
       etag        = md5(now()::text || revision::text)
 WHERE id = $1
   AND delete_time IS NULL;

-- CountOrgOwnersExcludingUser is the org-local sole-owner guard for
-- Iam.DeleteUser: it counts owner bindings in this org EXCLUDING the
-- user being removed, so the handler can refuse if removing them
-- would leave the org with zero owners. Counts both user and group
-- principals (matches CountOwnersByOrg's group-owner support).
-- name: CountOrgOwnersExcludingUser :one
SELECT COUNT(*)
  FROM org_members om
  JOIN roles r ON r.id = om.role_id
 WHERE om.org_id = $1
   AND r.is_system = true
   AND r.name = 'owner'
   AND NOT (om.principal_kind = 'user' AND om.principal_id = $2)
   AND (
     (om.principal_kind = 'user'
      AND EXISTS (
        SELECT 1 FROM users u
         WHERE u.id = om.principal_id
           AND u.delete_time IS NULL))
     OR
     (om.principal_kind = 'group'
      AND EXISTS (
        SELECT 1 FROM groups g
         WHERE g.id = om.principal_id
           AND g.state = 'ACTIVE'))
   );
