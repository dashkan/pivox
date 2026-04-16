-- name: CreateArtifact :one
INSERT INTO artifacts (conversation_id, name, type, title, description)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetArtifactByName :one
SELECT * FROM artifacts WHERE conversation_id = $1 AND name = $2;

-- name: GetArtifactByID :one
SELECT * FROM artifacts WHERE id = $1;

-- name: ListArtifactsByConversation :many
SELECT * FROM artifacts
WHERE conversation_id = $1
ORDER BY create_time DESC
LIMIT $2 OFFSET $3;

-- name: CountArtifactsByConversation :one
SELECT count(*) FROM artifacts WHERE conversation_id = $1;

-- name: UpdateArtifactLatestVersion :exec
UPDATE artifacts
SET latest_version_id = $2, update_time = now()
WHERE id = $1;

-- name: DeleteArtifact :exec
DELETE FROM artifacts WHERE id = $1;
