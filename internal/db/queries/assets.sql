-- name: CreateAsset :one
INSERT INTO assets (id, space_id, endpoint_id, name, display_name, import_path, filename, state, annotations, created_by, updated_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10)
RETURNING *;

-- name: GetAsset :one
SELECT * FROM assets WHERE id = $1;

-- GetAssetNamesByIDs is the batched lookup used to resolve
-- line-item → asset resource names without an N+1 fetch loop.
-- Returns (id, name) pairs for the IDs that exist; missing IDs
-- (e.g. a line_item whose asset was purged via SET NULL cascade,
-- or whose asset row was hard-deleted before this column moved
-- to soft-delete) are simply absent from the result set.
-- name: GetAssetNamesByIDs :many
SELECT id, name FROM assets WHERE id = ANY(@ids::uuid[]);

-- name: GetAssetByName :one
SELECT * FROM assets WHERE space_id = $1 AND name = $2;

-- name: GetAssetByChecksum :one
SELECT * FROM assets WHERE space_id = $1 AND checksum_sha256 = $2 AND delete_time IS NULL;

-- name: ListAssetsBySpace :many
-- Tiebreaker on id DESC keeps pagination stable when multiple assets
-- share a create_time (concurrent ingest can collide on µs precision):
-- without it, offset-based pagination can drop or duplicate rows.
--
-- The right-side joins populate the `latest_version_*` and
-- `endpoint_slug` columns the dashboards synthesizer needs to compose
-- storage URLs (Phase 6c). Latest version is computed via DISTINCT ON
-- in a derived table joined LEFT.
--
-- Why COALESCE on the latest_version_* columns: sqlc v1.31's
-- nullability inference looks at the underlying column's NOT NULL
-- constraint and gets LEFT-JOIN-of-derived-table nullability wrong
-- (asset_versions.version_number is NOT NULL, so sqlc types
-- av.version_number as int32 even though the LEFT JOIN can yield
-- NULL). Forcing a non-null shape via COALESCE with a sentinel
-- (0 for version_number — real versions start at 1; '' for mime_type)
-- avoids the runtime "scan NULL into int32" error on assets with no
-- versions. The synthesizer treats `version_number == 0` as the "no
-- version exists" signal — same semantics as a real LEFT JOIN NULL.
-- endpoint_slug doesn't need this trick because it's a base-table
-- column reference through a regular LEFT JOIN, which sqlc infers
-- correctly as pgtype.Text.
SELECT
  sqlc.embed(assets),
  COALESCE(av.version_number, 0) AS latest_version_number,
  COALESCE(av.mime_type, '')     AS latest_version_mime_type,
  e.name AS endpoint_slug,
  g.hostname AS gateway_hostname
FROM assets
LEFT JOIN (
  SELECT DISTINCT ON (asset_id)
    asset_id, version_number, mime_type
  FROM asset_versions
  ORDER BY asset_id, version_number DESC
) av ON av.asset_id = assets.id
LEFT JOIN storage_endpoints e ON e.id = assets.endpoint_id
LEFT JOIN storage_gateways  g ON g.id = e.gateway_id
WHERE assets.space_id = $1 AND assets.delete_time IS NULL
ORDER BY assets.create_time DESC, assets.id DESC
LIMIT $2 OFFSET $3;

-- name: ListAssetsBySpaceWithDeleted :many
SELECT * FROM assets
WHERE space_id = $1
ORDER BY create_time DESC, id DESC
LIMIT $2 OFFSET $3;

-- name: CountAssetsBySpace :one
SELECT count(*) FROM assets WHERE space_id = $1 AND delete_time IS NULL;

-- name: ListAssetsByOrg :many
-- Lists every active asset across every space in an organization,
-- with the space's slug attached so the caller can compose AIP
-- resource names without an N+1 lookup. Used by
-- Dashboards.QueryDashboardData at org-scoped parent so the system
-- Library dashboard can render assets across spaces in one round
-- trip. Soft-deleted spaces are skipped (their assets aren't
-- surfaced) — same effect as `assets.space_id` referencing a row
-- with a non-null `spaces.delete_time`.
--
-- Latest-version + storage_endpoints columns mirror ListAssetsBySpace;
-- same shape feeds the same synthesizer (Phase 6c). See
-- ListAssetsBySpace for the LEFT-JOIN-derived-table rationale.
SELECT
  sqlc.embed(assets),
  spaces.name AS space_slug,
  COALESCE(av.version_number, 0) AS latest_version_number,
  COALESCE(av.mime_type, '')     AS latest_version_mime_type,
  e.name AS endpoint_slug,
  g.hostname AS gateway_hostname
FROM assets
JOIN spaces ON assets.space_id = spaces.id
LEFT JOIN (
  SELECT DISTINCT ON (asset_id)
    asset_id, version_number, mime_type
  FROM asset_versions
  ORDER BY asset_id, version_number DESC
) av ON av.asset_id = assets.id
LEFT JOIN storage_endpoints e ON e.id = assets.endpoint_id
LEFT JOIN storage_gateways  g ON g.id = e.gateway_id
WHERE spaces.org_id = $1
  AND assets.delete_time IS NULL
  AND spaces.delete_time IS NULL
ORDER BY assets.create_time DESC, assets.id DESC
LIMIT $2 OFFSET $3;

-- name: CountAssetsByOrg :one
SELECT count(*) FROM assets
JOIN spaces ON assets.space_id = spaces.id
WHERE spaces.org_id = $1
  AND assets.delete_time IS NULL
  AND spaces.delete_time IS NULL;

-- name: UpdateAsset :one
UPDATE assets
SET display_name = COALESCE(sqlc.narg('display_name'), display_name),
    annotations = COALESCE(sqlc.narg('annotations'), annotations),
    expire_time = COALESCE(sqlc.narg('expire_time'), expire_time),
    revision = revision + 1,
    updated_by = $2,
    update_time = now(),
    etag = md5(now()::text)
WHERE id = $1
RETURNING *;

-- name: UpdateAssetState :exec
UPDATE assets
SET state = $2, update_time = now(), etag = md5(now()::text)
WHERE id = $1;

-- name: UpdateAssetIngestion :exec
UPDATE assets
SET state = $2,
    media_type = $3,
    content_type = $4,
    checksum_sha256 = $5,
    size_bytes = $6,
    technical_metadata = $7,
    ai_description = COALESCE(sqlc.narg('ai_description'), ai_description),
    transcription = COALESCE(sqlc.narg('transcription'), transcription),
    duration_seconds = COALESCE(sqlc.narg('duration_seconds'), duration_seconds),
    width = COALESCE(sqlc.narg('width'), width),
    height = COALESCE(sqlc.narg('height'), height),
    endpoint_id = COALESCE(sqlc.narg('endpoint_id'), endpoint_id),
    update_time = now(),
    etag = md5(now()::text)
WHERE id = $1;

-- name: SoftDeleteAsset :exec
UPDATE assets
SET state = 'DELETE_REQUESTED',
    delete_time = now(),
    purge_time = now() + INTERVAL '30 days',
    deleted_by = $2,
    update_time = now(),
    etag = md5(now()::text)
WHERE id = $1;

-- name: UndeleteAsset :exec
UPDATE assets
SET state = CASE WHEN endpoint_id IS NULL THEN 'PLACEHOLDER'::asset_state ELSE 'ACTIVE'::asset_state END,
    delete_time = NULL,
    purge_time = NULL,
    deleted_by = NULL,
    update_time = now(),
    etag = md5(now()::text)
WHERE id = $1;

-- name: ListExpiredAssets :many
SELECT * FROM assets
WHERE expire_time IS NOT NULL AND expire_time < now() AND delete_time IS NULL
LIMIT $1;

-- name: SearchAssets :many
SELECT * FROM assets
WHERE space_id = $1
  AND delete_time IS NULL
  AND search_vector @@ plainto_tsquery('english', $2)
ORDER BY ts_rank(search_vector, plainto_tsquery('english', $2)) DESC
LIMIT $3 OFFSET $4;
