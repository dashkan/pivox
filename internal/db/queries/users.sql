-- Phase 7 unified per-org identity with identities. The
-- per-org `users` join table was dropped; queries that used to
-- resolve "users.id from (org, firebase_identity)" now reference
-- `identities.id` directly. The handler reads it from the
-- `pivox_user_id` ID-token claim — no DB lookup required.
--
-- Queries below are the reduced set still useful post-unification:
-- listing orgs a user is a member of (for ListOrganizations and the
-- membership interceptor), counting owners (for the ≥1-owner
-- invariant), the cross-org cascades for DeleteAccount, and the
-- per-org cascades for the now-sync `Iam.DeleteUser`.

-- name: ListOrganizationsForIdentity :many
-- Lists all orgs the given firebase_identity has membership in,
-- counting both direct user bindings AND group-mediated bindings
-- (the user is in a group that has an org_members row).
-- Caller-scoped for `ListOrganizations` and the
-- membership-required interceptor — every authenticated user is
-- only ever shown orgs they actually belong to. Includes
-- soft-deleted orgs so an owner can reach UndeleteOrganization
-- during the 30-day grace window. Excludes purged orgs (the JOIN
-- naturally drops those once their org row is gone).
--
-- "Member" post-Phase-7 unification = at least one `org_members`
-- row whose principal resolves to this firebase_identity:
--
--   - Direct: `(principal_kind = 'user', principal_id = $1)`
--   - Group-mediated: `(principal_kind = 'group', principal_id IN
--     (groups the user belongs to via group_members.user_id = $1))`
--
-- Both must count or the membership-gate would reject a user whose
-- only role-binding is via a group, even though they can clearly
-- reach RPCs gated by that group's permissions. Group-mediated
-- access was the old behavior pre-Phase-7 (users.id existed for
-- group-only members and ListUsersByIdentity counted them);
-- preserved here in the unified shape.
SELECT DISTINCT o.*
  FROM organizations o
  JOIN org_members om ON om.org_id = o.id
 WHERE (
   (om.principal_kind = 'user' AND om.principal_id = $1)
   OR
   (om.principal_kind = 'group' AND om.principal_id IN (
     SELECT gm.group_id FROM group_members gm WHERE gm.user_id = $1
   ))
 )
 ORDER BY o.id ASC
 LIMIT 1000;

-- name: CountOwnersByOrg :one
-- Counts org_members rows whose role is the system 'owner' role for
-- this org, regardless of principal kind. Used by membership-mutation
-- handlers to enforce "≥1 owner". Group principals are counted only
-- when the group itself is ACTIVE (a soft-deleted group's binding
-- doesn't keep the org owner-ful). User principals are counted
-- unconditionally — there's no per-org-user soft-delete state any
-- more (Phase 7); a binding's existence equals its liveness.
-- Keys on the stable `roles.name` slug, not display_name.
SELECT COUNT(*)
  FROM org_members om
  JOIN roles r ON r.id = om.role_id
 WHERE om.org_id = $1
   AND r.is_system = true
   AND r.name = 'owner'
   AND (
     om.principal_kind = 'user'
     OR
     (om.principal_kind = 'group'
      AND EXISTS (
        SELECT 1 FROM groups g
         WHERE g.id = om.principal_id
           AND g.state = 'ACTIVE'))
   );

-- ListSoleOwnerOrgsForIdentity returns active orgs where
-- the given firebase_identity is the ONLY owner. Used by
-- DeleteAccount's VALIDATING phase to refuse deletion when the
-- caller would leave any org without an owner.
--
-- An org is "ONLY owned by this firebase_identity" iff:
--   - exactly one user-owner binding exists, and it points at this
--     firebase_identity, AND
--   - zero ACTIVE-group-owner bindings exist.
-- name: ListSoleOwnerOrgsForIdentity :many
SELECT o.*
  FROM organizations o
 WHERE o.delete_time IS NULL
   AND o.id IN (
     SELECT om.org_id
       FROM org_members om
       JOIN roles r ON r.id = om.role_id
      WHERE om.principal_kind = 'user'
        AND r.is_system = true
        AND r.name = 'owner'
      GROUP BY om.org_id
     HAVING count(*) = 1
        AND bool_or(om.principal_id = $1) = true
   )
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

-- DeleteOrgMembersForIdentity removes ALL org-scope role
-- bindings across every org for this firebase_identity. Used by
-- DeleteAccount's cross-org cascade. Org-scoped DeleteUser uses
-- the per-org variant `DeleteOrgMembersForUserInOrg` below.
-- name: DeleteOrgMembersForIdentity :exec
DELETE FROM org_members
 WHERE principal_kind = 'user'
   AND principal_id = $1;

-- DeleteSpaceMembersForIdentity is the cross-org space-
-- scope analogue used by DeleteAccount.
-- name: DeleteSpaceMembersForIdentity :exec
DELETE FROM space_members
 WHERE principal_kind = 'user'
   AND principal_id = $1;

-- DeleteGroupMembersForIdentity removes the firebase_identity
-- from every group it belongs to, across all orgs. Cross-org variant
-- for DeleteAccount. After Phase 7 unification, group_members.user_id
-- IS identities.id, so this is a single straight DELETE
-- without the prior subquery.
-- name: DeleteGroupMembersForIdentity :exec
DELETE FROM group_members WHERE user_id = $1;

-- HardDeleteIdentity removes the firebase_identity row.
-- group_members.user_id has ON DELETE CASCADE so group memberships
-- transitively delete; org_members and space_members principal_id
-- columns aren't FK'd (polymorphic by principal_kind) so the
-- cross-org cascades above must run first. Called as the
-- second-to-last step of DeleteAccount; the Firebase Auth identity
-- itself is deleted last so a partial failure leaves a recoverable
-- Firebase identity rather than orphaned Pivox state.
-- name: HardDeleteIdentity :exec
DELETE FROM identities WHERE id = $1;

-- ===========================================================================
-- Org-scoped cascade queries used by Iam.DeleteUser (org-scoped, sync
-- post-Phase-7 — no LRO, no grace). They remove a single user's
-- bindings within ONE org, leaving every other org untouched.
-- ===========================================================================

-- DeleteOrgMembersForUserInOrg removes the user's org-scope role
-- bindings in a single org. Bounded by (org_id, principal_id).
-- $2 is `identities.id` (post-Phase-7 unification).
-- name: DeleteOrgMembersForUserInOrg :exec
DELETE FROM org_members
 WHERE org_id = $1
   AND principal_kind = 'user'
   AND principal_id = $2;

-- DeleteSpaceMembersForUserInOrg removes the user's space-scope
-- bindings for spaces in this org. Joins to spaces to bound by
-- org_id, since space_members rows themselves only carry space_id.
-- $2 is `identities.id`.
-- name: DeleteSpaceMembersForUserInOrg :exec
DELETE FROM space_members
 WHERE principal_kind = 'user'
   AND principal_id = $2
   AND space_id IN (SELECT id FROM spaces WHERE org_id = $1);

-- DeleteGroupMembersForUserInOrg removes the user's membership in
-- groups belonging to this org. After Phase 7 unification,
-- group_members.user_id IS identities.id directly — same
-- type as principal_id on the sibling tables, just a different
-- column name (group_members has no principal_kind discriminator).
-- $2 is `identities.id`.
-- name: DeleteGroupMembersForUserInOrg :exec
DELETE FROM group_members
 WHERE user_id = $2
   AND group_id IN (SELECT id FROM groups WHERE org_id = $1);

-- CountOrgOwnersExcludingUser is the org-local sole-owner guard for
-- Iam.DeleteUser: it counts owner bindings in this org EXCLUDING the
-- user being removed, so the handler can refuse if removing them
-- would leave the org with zero owners. Counts both user and group
-- principals (matches CountOwnersByOrg's group-owner support).
-- $2 is `identities.id`.
-- name: CountOrgOwnersExcludingUser :one
SELECT COUNT(*)
  FROM org_members om
  JOIN roles r ON r.id = om.role_id
 WHERE om.org_id = $1
   AND r.is_system = true
   AND r.name = 'owner'
   AND NOT (om.principal_kind = 'user' AND om.principal_id = $2)
   AND (
     om.principal_kind = 'user'
     OR
     (om.principal_kind = 'group'
      AND EXISTS (
        SELECT 1 FROM groups g
         WHERE g.id = om.principal_id
           AND g.state = 'ACTIVE'))
   );
