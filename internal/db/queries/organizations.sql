-- name: GetOrganization :one
SELECT * FROM organizations WHERE id = $1 AND delete_time IS NULL;

-- name: GetOrganizationByName :one
SELECT * FROM organizations WHERE name = $1 AND delete_time IS NULL;

-- GetOrganizationByNameForGate looks up an org by slug regardless of
-- soft-delete state. Used by the permission interceptor so callers
-- can still target a soft-deleted org for reads and Undelete; the
-- gate enforces FAILED_PRECONDITION on mutating ops against a
-- DELETE_REQUESTED org.
-- name: GetOrganizationByNameForGate :one
SELECT * FROM organizations WHERE name = $1;

-- name: CreateOrganization :one
INSERT INTO organizations (id, name, display_name, created_by_firebase_identity_id, created_by, updated_by)
VALUES ($1, $2, $3, $4, $5, $5)
RETURNING *;

-- SoftDeleteOrganization transitions an ACTIVE org to DELETE_REQUESTED.
-- Sets delete_time=now, purge_time=now+30 days, deleted_by=$2. Refuses
-- to soft-delete an already-soft-deleted org (matches no rows).
-- Returns the updated row.
-- name: SoftDeleteOrganization :one
UPDATE organizations
SET state       = 'DELETE_REQUESTED',
    delete_time = now(),
    purge_time  = now() + INTERVAL '30 days',
    deleted_by  = $2,
    update_time = now(),
    revision    = revision + 1,
    etag        = md5(now()::text || revision::text)
WHERE id = $1 AND state = 'ACTIVE'
RETURNING *;

-- UndeleteOrganization restores a DELETE_REQUESTED org to ACTIVE,
-- clearing the soft-delete fields. Refuses to undelete past the
-- purge window (purge_time must still be in the future).
-- name: UndeleteOrganization :one
UPDATE organizations
SET state       = 'ACTIVE',
    delete_time = NULL,
    purge_time  = NULL,
    deleted_by  = '',
    update_time = now(),
    revision    = revision + 1,
    etag        = md5(now()::text || revision::text)
WHERE id = $1 AND state = 'DELETE_REQUESTED' AND purge_time > now()
RETURNING *;

-- PurgeOrganization hard-deletes an org row. FK ON DELETE CASCADE
-- removes spaces, members, domains, sso_configs, assets, requests,
-- tags, api keys, and ai conversations transitively. Used by force=true
-- DeleteOrganization and by the purge worker after grace expiry.
-- name: PurgeOrganization :exec
DELETE FROM organizations WHERE id = $1;

-- CancelRunningOpsForOrg marks all running operations linked to the
-- given org (via the operations.org_id reverse pointer) as
-- cancelled. Used by DeleteOrganization's CANCELLING_OPERATIONS
-- phase to interrupt in-flight org-scoped LROs before the cascade
-- deletes their target rows.
--
-- Scope: this matches operations whose creator passed `org_id` to
-- Manager.CreateAndRun. Today the only org-targeting LROs in code
-- are DeleteOrganization itself (which intentionally passes NULL
-- to avoid self-cancellation) and UndeleteOrganization (also NULL
-- so concurrent undeletes don't kill each other mid-flight). Future
-- LROs (asset imports, domain verifications, gateway upgrades,
-- etc.) populate org_id when implemented and will be cancellable
-- through this query without further changes.
--
-- error_code = 1 is gRPC codes.Cancelled.
-- name: CancelRunningOpsForOrg :exec
UPDATE operations
SET done          = true,
    error_code    = 1,
    error_message = 'cancelled by DeleteOrganization',
    update_time   = now()
WHERE done = false AND org_id = $1;
