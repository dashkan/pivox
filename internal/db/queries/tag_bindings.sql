-- name: CreateTagBinding :one
INSERT INTO tag_bindings (id, parent_resource, tag_value_id, created_by)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetTagBinding :one
SELECT * FROM tag_bindings WHERE id = $1;

-- name: DeleteTagBinding :exec
DELETE FROM tag_bindings WHERE id = $1;

-- name: ListEffectiveTags :many
SELECT tb.tag_value_id,
       tv.tag_key_id,
       tv.namespaced_name AS tag_value_namespaced_name,
       tk.namespaced_name AS tag_key_namespaced_name
FROM tag_bindings tb
JOIN tag_values tv ON tb.tag_value_id = tv.id
JOIN tag_keys tk ON tv.tag_key_id = tk.id
WHERE tb.parent_resource = $1
ORDER BY tk.id ASC;
