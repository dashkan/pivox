-- Dashboards (USER_MANAGED, space-scoped) CRUD.
--
-- The handler always re-marshals the full Dashboard proto into
-- payload on every write; display_name and description are mirrored
-- as scalar columns for AIP-160 filter / index use. management_mode
-- is stored as a column so the SYSTEM_MANAGED-mutation guard can
-- gate without unmarshaling JSONB on the hot path.

-- name: CreateDashboard :one
INSERT INTO dashboards (
    space_id,
    name,
    display_name,
    description,
    management_mode,
    payload,
    created_by,
    updated_by
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $7
)
RETURNING *;

-- name: GetDashboardByName :one
-- Returns the live (non-soft-deleted) dashboard with the given slug
-- in the given space. Used by GetDashboard, UpdateDashboard's
-- pre-read, and DeleteDashboard's pre-read.
SELECT * FROM dashboards
WHERE space_id = $1 AND name = $2 AND delete_time IS NULL;

-- name: GetDashboardByNameForUpdate :one
-- SELECT ... FOR UPDATE variant for UpdateDashboard / DeleteDashboard,
-- which need the row pinned for the duration of the surrounding tx
-- so a concurrent mutation can't race the etag check.
SELECT * FROM dashboards
WHERE space_id = $1 AND name = $2 AND delete_time IS NULL
FOR UPDATE;

-- name: ListDashboardsBySpace :many
-- Live dashboards in a space, newest-first. Pagination is offset-
-- based for v1 — the catalog is small (≤ 100s of dashboards per
-- space) and the surface won't grow until customers start
-- composing them programmatically.
SELECT * FROM dashboards
WHERE space_id = $1 AND delete_time IS NULL
ORDER BY create_time DESC, id DESC
LIMIT $2 OFFSET $3;

-- name: CountDashboardsBySpace :one
-- Companion to ListDashboardsBySpace for next_page_token computation:
-- emit a token iff (offset + page_size) < count.
SELECT COUNT(*) FROM dashboards
WHERE space_id = $1 AND delete_time IS NULL;

-- name: UpdateDashboardByName :one
-- Optimistic-concurrency update: if etag in the WHERE doesn't match,
-- the UPDATE returns zero rows and the handler maps that to Aborted.
-- The caller is expected to re-marshal the full Dashboard proto
-- into @payload on every Update; this query does not partially
-- patch JSONB.
UPDATE dashboards
SET
    display_name = $3,
    description  = $4,
    payload      = $5,
    updated_by   = $6,
    update_time  = now(),
    etag         = md5(now()::text || random()::text),
    revision     = revision + 1
WHERE space_id = $1 AND name = $2 AND etag = $7 AND delete_time IS NULL
RETURNING *;

-- name: SoftDeleteDashboardByName :one
-- Sets delete_time + deleted_by; row is no longer returned by
-- GetDashboardByName / ListDashboardsBySpace. Hard delete is
-- operator-only — no public RPC issues `DELETE FROM dashboards`.
UPDATE dashboards
SET
    delete_time = now(),
    deleted_by  = $3,
    update_time = now(),
    etag        = md5(now()::text || random()::text),
    revision    = revision + 1
WHERE space_id = $1 AND name = $2 AND delete_time IS NULL
RETURNING *;
