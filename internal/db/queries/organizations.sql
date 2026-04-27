-- name: GetOrganization :one
SELECT * FROM organizations WHERE id = $1 AND delete_time IS NULL;

-- name: GetOrganizationByName :one
SELECT * FROM organizations WHERE name = $1 AND delete_time IS NULL;

-- name: CreateOrganization :one
INSERT INTO organizations (id, name, display_name, created_by_firebase_identity_id, created_by, updated_by)
VALUES ($1, $2, $3, $4, $5, $5)
RETURNING *;
