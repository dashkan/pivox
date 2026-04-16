-- name: CreateInlineArtifactVersion :one
INSERT INTO artifact_versions (artifact_id, name, inline_data, inline_content_type, inline_size_bytes, sequence)
VALUES ($1, $2, $3, $4, $5, (SELECT COALESCE(MAX(sequence), 0) + 1 FROM artifact_versions WHERE artifact_id = $1))
RETURNING *;

-- name: CreateAssetArtifactVersion :one
INSERT INTO artifact_versions (artifact_id, name, asset_version_name, sequence)
VALUES ($1, $2, $3, (SELECT COALESCE(MAX(sequence), 0) + 1 FROM artifact_versions WHERE artifact_id = $1))
RETURNING *;

-- name: GetArtifactVersionByName :one
SELECT * FROM artifact_versions WHERE artifact_id = $1 AND name = $2;

-- name: GetArtifactVersionForContent :one
SELECT id, artifact_id, inline_data, inline_content_type, inline_size_bytes, asset_version_name
FROM artifact_versions
WHERE artifact_id = $1 AND name = $2;

-- name: ListArtifactVersionsByArtifact :many
SELECT id, artifact_id, name, inline_content_type, inline_size_bytes, asset_version_name, sequence, create_time
FROM artifact_versions
WHERE artifact_id = $1
ORDER BY sequence DESC
LIMIT $2 OFFSET $3;

-- name: CountArtifactVersionsByArtifact :one
SELECT count(*) FROM artifact_versions WHERE artifact_id = $1;

-- name: DeleteArtifactVersion :exec
DELETE FROM artifact_versions WHERE id = $1;

-- name: IsOnlyArtifactVersion :one
SELECT count(*) = 1 AS is_only FROM artifact_versions WHERE artifact_id = $1;
