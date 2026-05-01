-- name: CreateArtifact :one
INSERT INTO ai_artifacts (conversation_id, name, type, title, description)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetArtifactByName :one
SELECT * FROM ai_artifacts WHERE conversation_id = $1 AND name = $2;

-- name: GetArtifactByID :one
SELECT * FROM ai_artifacts WHERE id = $1;

-- GetArtifactByNameForUpdate is the locking variant used by
-- DeleteArtifact (force=false) inside its tx. The FOR UPDATE row
-- lock conflicts with the FK SHARE lock that a concurrent
-- CreateArtifactVersion takes on this artifact, so a concurrent
-- version-create blocks until our tx resolves — eliminating the
-- TOCTOU window between "no versions" and "delete artifact".
-- name: GetArtifactByNameForUpdate :one
SELECT * FROM ai_artifacts WHERE conversation_id = $1 AND name = $2 FOR UPDATE;

-- GetArtifactByIDForUpdate is the locking variant used by
-- DeleteArtifactVersion inside its tx. We already know the parent
-- artifact id from resolveArtifact; take the FOR UPDATE lock on it
-- inside the tx so a concurrent CreateArtifactVersion can't land a
-- new sibling version between our IsOnlyArtifactVersion check and
-- our DELETE — without the lock, we could observe "this is the only
-- version", let a concurrent insert land, then delete our version
-- AND cascade-delete the parent (orphaning the just-inserted
-- sibling, depending on FK semantics).
-- name: GetArtifactByIDForUpdate :one
SELECT * FROM ai_artifacts WHERE id = $1 FOR UPDATE;

-- name: CountArtifactsByConversation :one
SELECT count(*) FROM ai_artifacts WHERE conversation_id = $1;

-- name: UpdateArtifactLatestVersion :exec
UPDATE ai_artifacts
SET latest_version_id = $2, update_time = now()
WHERE id = $1;

-- name: DeleteArtifact :exec
DELETE FROM ai_artifacts WHERE id = $1;
