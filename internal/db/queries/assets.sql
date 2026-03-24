-- name: CreateAsset :one
INSERT INTO assets (id, project_id, endpoint_id, name, display_name, import_path, state, annotations, created_by, updated_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9)
RETURNING *;

-- name: GetAsset :one
SELECT * FROM assets WHERE id = $1;

-- name: GetAssetByName :one
SELECT * FROM assets WHERE project_id = $1 AND name = $2;

-- name: GetAssetByChecksum :one
SELECT * FROM assets WHERE project_id = $1 AND checksum_sha256 = $2 AND delete_time IS NULL;

-- name: ListAssetsByProject :many
SELECT * FROM assets
WHERE project_id = $1 AND delete_time IS NULL
ORDER BY create_time DESC
LIMIT $2 OFFSET $3;

-- name: ListAssetsByProjectWithDeleted :many
SELECT * FROM assets
WHERE project_id = $1
ORDER BY create_time DESC
LIMIT $2 OFFSET $3;

-- name: CountAssetsByProject :one
SELECT count(*) FROM assets WHERE project_id = $1 AND delete_time IS NULL;

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
    mime_type = $4,
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
    deleted_by = '',
    update_time = now(),
    etag = md5(now()::text)
WHERE id = $1;

-- name: ListExpiredAssets :many
SELECT * FROM assets
WHERE expire_time IS NOT NULL AND expire_time < now() AND delete_time IS NULL
LIMIT $1;

-- name: SearchAssets :many
SELECT * FROM assets
WHERE project_id = $1
  AND delete_time IS NULL
  AND search_vector @@ plainto_tsquery('english', $2)
ORDER BY ts_rank(search_vector, plainto_tsquery('english', $2)) DESC
LIMIT $3 OFFSET $4;
