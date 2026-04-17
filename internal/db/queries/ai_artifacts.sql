-- name: CreateArtifact :one
INSERT INTO ai_artifacts (conversation_id, name, type, title, description)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetArtifactByName :one
SELECT * FROM ai_artifacts WHERE conversation_id = $1 AND name = $2;

-- name: GetArtifactByID :one
SELECT * FROM ai_artifacts WHERE id = $1;

-- name: CountArtifactsByConversation :one
SELECT count(*) FROM ai_artifacts WHERE conversation_id = $1;

-- name: UpdateArtifactLatestVersion :exec
UPDATE ai_artifacts
SET latest_version_id = $2, update_time = now()
WHERE id = $1;

-- name: DeleteArtifact :exec
DELETE FROM ai_artifacts WHERE id = $1;
