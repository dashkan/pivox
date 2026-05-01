-- name: CreateTagValue :one
INSERT INTO tag_values (id, tag_key_id, short_name, namespaced_name, description, created_by, updated_by)
VALUES ($1, $2, $3, $4, $5, $6, $6)
RETURNING *;

-- name: GetTagValue :one
SELECT * FROM tag_values WHERE id = $1;

-- GetTagValueForUpdate is the locking variant used by DeleteTagValue
-- inside its tx to serialize the empty-check + DELETE against
-- concurrent CreateTagBinding inserts. The FOR UPDATE row lock
-- conflicts with the FK SHARE lock that a binding INSERT takes on
-- this row, so a concurrent CreateTagBinding blocks until our tx
-- commits — eliminating the TOCTOU window between "no bindings"
-- and "delete value".
-- name: GetTagValueForUpdate :one
SELECT * FROM tag_values WHERE id = $1 FOR UPDATE;

-- name: GetTagValueByNamespacedName :one
SELECT * FROM tag_values WHERE namespaced_name = $1;

-- name: UpdateTagValue :one
UPDATE tag_values
SET description = COALESCE(sqlc.narg('description'), description),
    revision = revision + 1,
    updated_by = $2,
    update_time = now(),
    etag = md5(now()::text)
WHERE id = $1
RETURNING *;

-- name: DeleteTagValue :exec
DELETE FROM tag_values WHERE id = $1;

-- name: CountTagBindingsByTagValue :one
SELECT count(*) FROM tag_bindings WHERE tag_value_id = $1;
