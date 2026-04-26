-- name: CreateUserMembership :one
-- Creates a per-org membership row joining an account to an org with a role.
-- Used by `CreateOrganization` (founder, role='owner') and the future
-- `AcceptInvitation` flow (invitee, role from invite).
INSERT INTO users (id, org_id, account_id, role)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetUserMembership :one
SELECT * FROM users WHERE org_id = $1 AND account_id = $2;

-- name: ListUsersByOrg :many
SELECT * FROM users WHERE org_id = $1 ORDER BY create_time;

-- name: ListUsersByAccount :many
-- Lists all live org memberships for an account, excluding memberships
-- in soft-deleted orgs. Used by the membership interceptor's gate and
-- by any consumer that needs the "is this caller in any active org?"
-- signal. Joining out the deleted orgs here keeps that signal in sync
-- with `ListOrganizationsForAccount` — without it, a caller whose only
-- memberships are in deleted orgs would pass the membership check but
-- see an empty org list, soft-bricking onboarding.
SELECT u.*
  FROM users u
  JOIN organizations o ON o.id = u.org_id
 WHERE u.account_id = $1
   AND o.delete_time IS NULL
 ORDER BY u.create_time;

-- name: CountOwnersByOrg :one
-- Used by membership-mutation handlers to enforce "≥1 owner" — call
-- before any role-change or delete that would reduce the owner count.
SELECT COUNT(*) FROM users WHERE org_id = $1 AND role = 'owner';

-- name: UpdateUserRole :one
UPDATE users
   SET role = $3, update_time = now(), revision = revision + 1
 WHERE org_id = $1 AND account_id = $2
 RETURNING *;

-- name: DeleteUserMembership :exec
DELETE FROM users WHERE org_id = $1 AND account_id = $2;

-- name: ListOrganizationsForAccount :many
-- Lists all organizations the given account has membership in.
-- Caller-scoped for `ListOrganizations`: every authenticated user is
-- only ever shown orgs they belong to. Excludes soft-deleted orgs.
-- No pagination — typical users are in 1-3 orgs. The 1000-row LIMIT
-- is a defensive backstop, not a paging mechanism; if anyone ever
-- needs more we'll know because something is very wrong.
SELECT o.*
  FROM organizations o
  JOIN users u ON u.org_id = o.id
 WHERE u.account_id = $1
   AND o.delete_time IS NULL
 ORDER BY o.id ASC
 LIMIT 1000;
