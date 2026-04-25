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
-- Lists all org memberships for an account. Used by the native app's
-- "which orgs am I in?" query — drives the org selector and the
-- "zero orgs → onboarding" detection.
SELECT * FROM users WHERE account_id = $1 ORDER BY create_time;

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
