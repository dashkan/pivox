-- name: CreateInlineArtifactVersion :one
INSERT INTO ai_artifact_versions (artifact_id, name, inline_data, inline_content_type, inline_size_bytes, sequence)
VALUES ($1, $2, $3, $4, $5, (SELECT COALESCE(MAX(sequence), 0) + 1 FROM ai_artifact_versions WHERE artifact_id = $1))
RETURNING *;

-- name: CreateAssetArtifactVersion :one
INSERT INTO ai_artifact_versions (artifact_id, name, asset_version_name, sequence)
VALUES ($1, $2, $3, (SELECT COALESCE(MAX(sequence), 0) + 1 FROM ai_artifact_versions WHERE artifact_id = $1))
RETURNING *;

-- name: GetArtifactVersionByName :one
SELECT * FROM ai_artifact_versions WHERE artifact_id = $1 AND name = $2;

-- name: GetArtifactVersionForContent :one
SELECT id, artifact_id, inline_data, inline_content_type, inline_size_bytes, asset_version_name
FROM ai_artifact_versions
WHERE artifact_id = $1 AND name = $2;

-- name: CountArtifactVersionsByArtifact :one
SELECT count(*) FROM ai_artifact_versions WHERE artifact_id = $1;

-- name: DeleteArtifactVersion :exec
DELETE FROM ai_artifact_versions WHERE id = $1;

-- name: IsOnlyArtifactVersion :one
SELECT count(*) = 1 AS is_only FROM ai_artifact_versions WHERE artifact_id = $1;
