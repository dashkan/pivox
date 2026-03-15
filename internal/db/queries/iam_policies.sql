-- name: GetIamPolicy :one
SELECT * FROM iam_policies WHERE resource_id = $1;

-- name: UpsertIamPolicy :one
INSERT INTO iam_policies (resource_id, resource_type, policy, updated_by)
VALUES ($1, $2, $3, $4)
ON CONFLICT (resource_id) DO UPDATE
SET policy = EXCLUDED.policy,
    etag = md5(now()::text),
    updated_by = EXCLUDED.updated_by,
    update_time = now()
RETURNING *;

-- name: DeleteIamPolicy :exec
DELETE FROM iam_policies WHERE resource_id = $1;
