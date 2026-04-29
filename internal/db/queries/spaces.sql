-- name: CreateSpace :one
INSERT INTO spaces (id, org_id, name, display_name, labels, created_by, updated_by)
VALUES ($1, $2, $3, $4, $5, $6, $6)
RETURNING *;

-- name: GetSpace :one
SELECT * FROM spaces WHERE id = $1 AND delete_time IS NULL;

-- name: GetSpaceIncludingDeleted :one
SELECT * FROM spaces WHERE id = $1;

-- name: GetSpaceByName :one
SELECT * FROM spaces WHERE org_id = $1 AND name = $2 AND delete_time IS NULL;

-- GetSpaceByNameForGate looks up a space by (org, slug) regardless of
-- soft-delete state. Mirrors GetOrganizationByNameForGate: lets the
-- permission interceptor resolve a soft-deleted space so reads still
-- work during the grace window and UndeleteSpace can target the row.
-- name: GetSpaceByNameForGate :one
SELECT * FROM spaces WHERE org_id = $1 AND name = $2;

-- name: UpdateSpace :one
UPDATE spaces
SET display_name = COALESCE(sqlc.narg('display_name'), display_name),
    labels = COALESCE(sqlc.narg('labels'), labels),
    revision = revision + 1,
    updated_by = $2,
    update_time = now(),
    etag = md5(now()::text)
WHERE id = $1 AND delete_time IS NULL
RETURNING *;

-- name: SoftDeleteSpace :one
UPDATE spaces
SET state = 'DELETE_REQUESTED',
    delete_time = now(),
    purge_time = now() + INTERVAL '30 days',
    revision = revision + 1,
    deleted_by = $2,
    updated_by = $2,
    update_time = now(),
    etag = md5(now()::text)
WHERE id = $1 AND delete_time IS NULL
RETURNING *;

-- name: UndeleteSpace :one
UPDATE spaces
SET state = 'ACTIVE',
    delete_time = NULL,
    purge_time = NULL,
    deleted_by = '',
    revision = revision + 1,
    updated_by = $2,
    update_time = now(),
    etag = md5(now()::text)
WHERE id = $1 AND delete_time IS NOT NULL
RETURNING *;

-- PurgeSpace hard-deletes a space row, race-guarded by etag. FK
-- ON DELETE CASCADE removes space_members, assets, and asset_requests
-- transitively. Used by force=true DeleteSpace where the caller has
-- already validated state and pinned the row's revision; the etag
-- check refuses to fire if the row has been mutated since the
-- handler read it. Returns the deleted id on success; pgx.ErrNoRows
-- on etag drift, which the LRO surfaces as FailedPrecondition.
-- name: PurgeSpace :one
DELETE FROM spaces WHERE id = $1 AND etag = $2 RETURNING id;

-- PurgeExpiredSpace is the purge-worker variant: deletes only
-- soft-deleted spaces whose grace window has elapsed. The WHERE
-- clause race-guards against a concurrent UndeleteSpace that could
-- fire between ListSpacesPastPurgeTime and this DELETE — a restored
-- space is left alone, matching user intent.
-- name: PurgeExpiredSpace :exec
DELETE FROM spaces
 WHERE id = $1
   AND delete_time IS NOT NULL
   AND purge_time < now();

-- ListSpacesPastPurgeTime returns soft-deleted spaces whose
-- purge_time has elapsed. Used by SpacePurgeWorker to drive the
-- final cascade for spaces that finished their 30-day grace window
-- without being undeleted. The 100-row LIMIT bounds the per-tick
-- batch size; multi-replica deploys serialize on the worker's
-- advisory lock so only one runs at a time per cluster.
-- name: ListSpacesPastPurgeTime :many
SELECT * FROM spaces
 WHERE delete_time IS NOT NULL
   AND purge_time IS NOT NULL
   AND purge_time < now()
 ORDER BY purge_time ASC
 LIMIT 100;
