-- name: CreateLineItem :one
INSERT INTO line_items (id, request_id, asset_id, name, display_name, description, media_type, annotations, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetLineItem :one
SELECT * FROM line_items WHERE id = $1;

-- name: GetLineItemByName :one
SELECT * FROM line_items WHERE request_id = $1 AND name = $2;

-- name: ListLineItemsByRequest :many
SELECT * FROM line_items
WHERE request_id = $1
ORDER BY create_time ASC
LIMIT $2 OFFSET $3;

-- name: CountLineItemsByRequest :one
SELECT count(*) FROM line_items WHERE request_id = $1;

-- name: CountFulfilledLineItems :one
SELECT count(*) FROM line_items li
JOIN assets a ON li.asset_id = a.id
WHERE li.request_id = $1 AND a.state = 'ACTIVE';

-- name: UpdateLineItem :one
UPDATE line_items
SET display_name = COALESCE(sqlc.narg('display_name'), display_name),
    description = COALESCE(sqlc.narg('description'), description),
    annotations = COALESCE(sqlc.narg('annotations'), annotations),
    update_time = now()
WHERE id = $1
RETURNING *;

-- name: UpdateLineItemState :exec
UPDATE line_items
SET state = $2, update_time = now()
WHERE id = $1;

-- name: DeleteLineItem :exec
DELETE FROM line_items WHERE id = $1;
