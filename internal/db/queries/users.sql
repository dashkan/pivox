-- Phase 7 unified per-org identity with `identities`. The per-org
-- `users` join table was dropped; queries that used to resolve
-- "users.id from (org, identity)" now reference `identities.id`
-- directly. The handler reads it from the token `sub` — no DB
-- lookup required.
--
-- Post-principal-split (Phase 3 of the identities rework): the
-- polymorphic `principal_kind/principal_id` pair on org_members /
-- space_members was replaced by typed `user_id`/`group_id`
-- columns with an XOR check. Direct user binding queries below
-- read `user_id` directly; group-mediated lookups read `group_id`.

-- name: ListOrganizationsForIdentity :many
-- Lists all orgs the given identity has membership in, counting
-- both direct user bindings AND group-mediated bindings (the user
-- is in a group that has an org_members row).
-- Caller-scoped for `ListOrganizations` and the
-- membership-required interceptor — every authenticated user is
-- only ever shown orgs they actually belong to. Includes
-- soft-deleted orgs so an owner can reach UndeleteOrganization
-- during the 30-day grace window. Excludes purged orgs (the JOIN
-- naturally drops those once their org row is gone).
SELECT DISTINCT o.*
  FROM organizations o
  JOIN org_members om ON om.org_id = o.id
 WHERE (
   om.user_id = $1
   OR
   om.group_id IN (
     SELECT gm.group_id FROM group_members gm WHERE gm.user_id = $1
   )
 )
 ORDER BY o.id ASC
 LIMIT 1000;

-- name: ListAccountOrganizationsForIdentity :many
-- Caller-scoped slim view: (active org, highest-precedence role) for
-- an identity. Combines direct user bindings and group-mediated
-- bindings, then collapses to one row per org with the highest
-- precedence role winning (owner > admin > editor > viewer).
--
-- Differences from ListOrganizationsForIdentity (above):
--   - Excludes soft-deleted orgs. The undelete UX runs against the
--     full Organizations.ListOrganizations; this slim view is for
--     post-sign-in bootstrap + org-picker, which doesn't want
--     tombstones in the list.
--   - JOINs roles and surfaces role_name. The CASE expression pins
--     v1's static system-role set; bindings to any other role are
--     excluded entirely by the WHERE filter. Adding a v2 role
--     requires updating this expression AND the precedence test —
--     otherwise the binding silently disappears from the view.
SELECT DISTINCT ON (o.id)
  o.id, o.name AS slug, o.display_name, r.name AS role_name
  FROM organizations o
  JOIN org_members om ON om.org_id = o.id
  JOIN roles r ON r.id = om.role_id
 WHERE o.state = 'ACTIVE'
   AND r.name IN ('owner', 'admin', 'editor', 'viewer')
   AND (
     om.user_id = $1
     OR om.group_id IN (
       SELECT gm.group_id FROM group_members gm WHERE gm.user_id = $1
     )
   )
 ORDER BY o.id,
   CASE r.name
     WHEN 'owner'  THEN 1
     WHEN 'admin'  THEN 2
     WHEN 'editor' THEN 3
     WHEN 'viewer' THEN 4
   END
 LIMIT 1000;

-- name: CountOwnersByOrg :one
-- Counts org_members rows whose role is the system 'owner' role for
-- this org, regardless of principal kind. Used by membership-mutation
-- handlers to enforce "≥1 owner". Group principals are counted only
-- when the group itself is ACTIVE (a soft-deleted group's binding
-- doesn't keep the org owner-ful). User principals are counted
-- unconditionally — an identity row is preserved across soft-delete
-- but the membership cascade in DeleteAccount removes their
-- org_members row before tombstoning, so a counted user binding
-- always points at a live identity.
SELECT COUNT(*)
  FROM org_members om
  JOIN roles r ON r.id = om.role_id
 WHERE om.org_id = $1
   AND r.is_system = true
   AND r.name = 'owner'
   AND (
     om.user_id IS NOT NULL
     OR
     (om.group_id IS NOT NULL
      AND EXISTS (
        SELECT 1 FROM groups g
         WHERE g.id = om.group_id
           AND g.state = 'ACTIVE'))
   );

-- ListSoleOwnerOrgsForIdentity returns active orgs where the given
-- identity is the ONLY owner. Used by DeleteAccount's VALIDATING
-- phase to refuse deletion when the caller would leave any org
-- without an owner.
--
-- An org is "ONLY owned by this identity" iff:
--   - exactly one user-owner binding exists, and it points at this
--     identity, AND
--   - zero ACTIVE-group-owner bindings exist.
-- name: ListSoleOwnerOrgsForIdentity :many
SELECT o.*
  FROM organizations o
 WHERE o.delete_time IS NULL
   AND o.id IN (
     SELECT om.org_id
       FROM org_members om
       JOIN roles r ON r.id = om.role_id
      WHERE om.user_id IS NOT NULL
        AND r.is_system = true
        AND r.name = 'owner'
      GROUP BY om.org_id
     HAVING count(*) = 1
        AND bool_or(om.user_id = $1) = true
   )
   AND NOT EXISTS (
     SELECT 1
       FROM org_members om2
       JOIN roles r2 ON r2.id = om2.role_id
       JOIN groups g2 ON g2.id = om2.group_id
      WHERE om2.org_id = o.id
        AND r2.is_system = true
        AND r2.name = 'owner'
        AND g2.state = 'ACTIVE'
   );

-- DeleteOrgMembersForIdentity removes ALL org-scope role bindings
-- across every org for this identity (direct user bindings only —
-- group-derived bindings stay because the group itself isn't being
-- deleted; the user just gets removed from the group via
-- DeleteGroupMembersForIdentity below). Used by DeleteAccount's
-- cross-org cascade.
-- name: DeleteOrgMembersForIdentity :exec
DELETE FROM org_members WHERE user_id = $1;

-- DeleteSpaceMembersForIdentity is the cross-org space-scope
-- analogue used by DeleteAccount.
-- name: DeleteSpaceMembersForIdentity :exec
DELETE FROM space_members WHERE user_id = $1;

-- DeleteGroupMembersForIdentity removes the identity from every
-- group it belongs to, across all orgs. Cross-org variant for
-- DeleteAccount. group_members.user_id is `identities.id` directly,
-- so this is a single straight DELETE.
-- name: DeleteGroupMembersForIdentity :exec
DELETE FROM group_members WHERE user_id = $1;

-- HardDeleteIdentity removes the identity row entirely. Operator-only
-- — the public DeleteAccount LRO uses SoftDeleteIdentity (which
-- preserves the row so historical *_by audit references continue to
-- resolve as is_deleted=true). HardDelete remains for terminal
-- purges only.
-- name: HardDeleteIdentity :exec
DELETE FROM identities WHERE id = $1;

-- ===========================================================================
-- Org-scoped cascade queries used by Iam.DeleteUser (org-scoped, sync
-- post-Phase-7 — no LRO, no grace). They remove a single user's
-- bindings within ONE org, leaving every other org untouched.
-- ===========================================================================

-- DeleteOrgMembersForUserInOrg removes the user's org-scope role
-- bindings in a single org. Bounded by (org_id, user_id).
-- $2 is `identities.id`.
-- name: DeleteOrgMembersForUserInOrg :exec
DELETE FROM org_members
 WHERE org_id = $1
   AND user_id = $2;

-- DeleteSpaceMembersForUserInOrg removes the user's space-scope
-- bindings for spaces in this org. Joins to spaces to bound by
-- org_id, since space_members rows themselves only carry space_id.
-- $2 is `identities.id`.
-- name: DeleteSpaceMembersForUserInOrg :exec
DELETE FROM space_members
 WHERE user_id = $2
   AND space_id IN (SELECT id FROM spaces WHERE org_id = $1);

-- DeleteGroupMembersForUserInOrg removes the user's membership in
-- groups belonging to this org. group_members.user_id IS
-- identities.id (post-Phase-7).
-- $2 is `identities.id`.
-- name: DeleteGroupMembersForUserInOrg :exec
DELETE FROM group_members
 WHERE user_id = $2
   AND group_id IN (SELECT id FROM groups WHERE org_id = $1);

-- CountOrgOwnersExcludingUser is the org-local sole-owner guard for
-- Iam.DeleteUser: it counts owner bindings in this org EXCLUDING the
-- user being removed, so the handler can refuse if removing them
-- would leave the org with zero owners. Counts both user and
-- ACTIVE-group principals (matches CountOwnersByOrg).
-- $2 is `identities.id`.
-- name: CountOrgOwnersExcludingUser :one
SELECT COUNT(*)
  FROM org_members om
  JOIN roles r ON r.id = om.role_id
 WHERE om.org_id = $1
   AND r.is_system = true
   AND r.name = 'owner'
   -- IS DISTINCT FROM treats NULL as a value, so group-owner rows
   -- (where om.user_id IS NULL) survive — `NULL = $2` would yield
   -- NULL and silently drop them, breaking the sole-owner guard.
   AND (om.user_id IS DISTINCT FROM $2)
   AND (
     om.user_id IS NOT NULL
     OR
     (om.group_id IS NOT NULL
      AND EXISTS (
        SELECT 1 FROM groups g
         WHERE g.id = om.group_id
           AND g.state = 'ACTIVE'))
   );
