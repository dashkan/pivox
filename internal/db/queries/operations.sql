-- CreateOperation creates a new operation row. `org_id` is the
-- optional reverse pointer that links the LRO to its target org
-- (NULL for ops that aren't org-scoped or where the org isn't
-- known at create time, including DeleteOrganization which must
-- not self-cancel).
-- name: CreateOperation :one
INSERT INTO operations (id, parent, metadata, created_by, org_id, space_id)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetOperation :one
SELECT * FROM operations WHERE id = $1;

-- name: ListOperations :many
SELECT * FROM operations
WHERE (sqlc.narg('parent_filter')::text IS NULL OR parent = sqlc.narg('parent_filter'))
ORDER BY create_time DESC
LIMIT $1;

-- name: ListAuthorizedOperations :many
-- Operations the caller is permitted to see, scope-trimmed in one query
-- (no N+1):
--   - account-scoped (no org/space): only the creator;
--   - org-scoped: caller has organizations.read at the op's org;
--   - space-scoped: caller has spaces.read at the op's space, via direct
--     space membership OR inherited parent-org membership.
-- Membership resolves both direct (user_id) and group (group_id)
-- bindings, mirroring GetEffectiveOrgRoles/GetEffectiveSpaceRoles;
-- role_permissions supplies the generic read grant (all system roles
-- hold it today, but the join future-proofs custom roles that may not).
SELECT o.* FROM operations o
WHERE
  (o.org_id IS NULL AND o.space_id IS NULL AND o.created_by = sqlc.arg(caller))
  OR (o.space_id IS NULL AND o.org_id IS NOT NULL AND EXISTS (
        SELECT 1 FROM org_members om
          JOIN role_permissions rp ON rp.role_id = om.role_id
          JOIN permissions perm ON perm.id = rp.permission_id
         WHERE om.org_id = o.org_id
           AND perm.permission_id = 'organizations.read'
           AND (om.user_id = sqlc.arg(caller)
                OR om.group_id IN (SELECT gm.group_id FROM group_members gm WHERE gm.user_id = sqlc.arg(caller)))))
  OR (o.space_id IS NOT NULL AND (
        EXISTS (SELECT 1 FROM space_members sm
                  JOIN role_permissions rp ON rp.role_id = sm.role_id
                  JOIN permissions perm ON perm.id = rp.permission_id
                 WHERE sm.space_id = o.space_id
                   AND perm.permission_id = 'spaces.read'
                   AND (sm.user_id = sqlc.arg(caller)
                        OR sm.group_id IN (SELECT gm.group_id FROM group_members gm WHERE gm.user_id = sqlc.arg(caller))))
        OR EXISTS (SELECT 1 FROM spaces s
                     JOIN org_members om ON om.org_id = s.org_id
                     JOIN role_permissions rp ON rp.role_id = om.role_id
                     JOIN permissions perm ON perm.id = rp.permission_id
                    WHERE s.id = o.space_id
                      AND perm.permission_id = 'spaces.read'
                      AND (om.user_id = sqlc.arg(caller)
                           OR om.group_id IN (SELECT gm.group_id FROM group_members gm WHERE gm.user_id = sqlc.arg(caller))))))
ORDER BY o.create_time DESC
LIMIT sqlc.arg(page_size);

-- name: CompleteOperation :one
UPDATE operations
SET done = true, result = $2, update_time = now()
WHERE id = $1
RETURNING *;

-- name: FailOperation :one
UPDATE operations
SET done = true, error_code = $2, error_message = $3, update_time = now()
WHERE id = $1
RETURNING *;

-- name: UpdateOperationMetadata :exec
UPDATE operations
SET metadata = $2, update_time = now()
WHERE id = $1;

-- name: CancelOperation :one
UPDATE operations
SET done = true, error_code = 1, error_message = 'cancelled by user', update_time = now()
WHERE id = $1 AND done = false
RETURNING *;

-- name: DeleteOperation :exec
DELETE FROM operations WHERE id = $1;

-- name: ListPendingOperations :many
SELECT * FROM operations WHERE done = false ORDER BY create_time ASC;

-- name: DeleteExpiredOperations :exec
DELETE FROM operations WHERE done = true AND expire_time < now();
