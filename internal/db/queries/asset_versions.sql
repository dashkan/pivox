-- name: CreateAssetVersion :one
INSERT INTO asset_versions (id, asset_id, version_number, checksum_sha256, size_bytes, mime_type, storage_key, change_note, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetAssetVersion :one
SELECT * FROM asset_versions WHERE id = $1;

-- name: GetAssetVersionByNumber :one
SELECT * FROM asset_versions WHERE asset_id = $1 AND version_number = $2;

-- name: GetLatestAssetVersion :one
SELECT * FROM asset_versions
WHERE asset_id = $1
ORDER BY version_number DESC
LIMIT 1;

-- name: ListAssetVersions :many
SELECT * FROM asset_versions
WHERE asset_id = $1
ORDER BY version_number DESC
LIMIT $2 OFFSET $3;

-- name: CountAssetVersions :one
SELECT count(*) FROM asset_versions WHERE asset_id = $1;

-- name: NextVersionNumber :one
SELECT COALESCE(MAX(version_number), 0) + 1 FROM asset_versions WHERE asset_id = $1;

-- name: UpdateAssetVersionError :exec
UPDATE asset_versions
SET ingestion_error = $2
WHERE id = $1;

-- name: CreateAssetRendition :one
INSERT INTO asset_renditions (id, version_id, type, storage_key, mime_type, width, height, size_bytes)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: ListAssetRenditions :many
SELECT * FROM asset_renditions WHERE version_id = $1;

-- name: DeleteAssetRenditionsByVersion :exec
DELETE FROM asset_renditions WHERE version_id = $1;
