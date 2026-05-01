-- name: CreateRequest :one
INSERT INTO asset_requests (id, space_id, name, display_name, description, state, priority, assignee, due_time, created_by, updated_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10)
RETURNING *;

-- name: GetRequest :one
SELECT * FROM asset_requests WHERE id = $1;

-- name: GetRequestByName :one
SELECT * FROM asset_requests WHERE space_id = $1 AND name = $2;

-- GetRequestByNameForUpdate is the locking variant used by
-- CancelRequest inside its tx. CancelRequest accepts any state
-- except APPROVED/CANCELLED, so the precondition can't be folded
-- into a WHERE clause as cleanly as the simple from→to transitions
-- (which use UpdateRequestStateIfFrom). The lock instead serializes
-- concurrent transitions on the same row, so a concurrent
-- ApproveRequest blocks until our tx resolves and we read the
-- post-Approve state.
-- name: GetRequestByNameForUpdate :one
SELECT * FROM asset_requests WHERE space_id = $1 AND name = $2 FOR UPDATE;

-- name: ListRequestsBySpace :many
SELECT * FROM asset_requests
WHERE space_id = $1
ORDER BY create_time DESC
LIMIT $2 OFFSET $3;

-- name: CountRequestsBySpace :one
SELECT count(*) FROM asset_requests WHERE space_id = $1;

-- name: UpdateRequest :one
UPDATE asset_requests
SET display_name = COALESCE(sqlc.narg('display_name'), display_name),
    description = COALESCE(sqlc.narg('description'), description),
    priority = COALESCE(sqlc.narg('priority'), priority),
    due_time = COALESCE(sqlc.narg('due_time'), due_time),
    annotations = COALESCE(sqlc.narg('annotations'), annotations),
    revision = revision + 1,
    updated_by = $2,
    update_time = now(),
    etag = md5(now()::text)
WHERE id = $1
RETURNING *;

-- name: UpdateRequestState :one
UPDATE asset_requests
SET state = $2,
    revision = revision + 1,
    updated_by = $3,
    update_time = now(),
    etag = md5(now()::text)
WHERE id = $1
RETURNING *;

-- UpdateRequestStateIfFrom is the precondition-guarded variant used
-- by the per-state transition handlers (Submit, Approve, Reject,
-- RequestRevision, Cancel, Claim, Deliver). The application-layer
-- read-then-check-then-update pattern is racy: two callers could
-- both observe state=$from and both try to transition; the WHERE
-- clause here makes the precondition atomic with the write, so only
-- one tx commits and the other returns ErrNoRows. The handler maps
-- ErrNoRows to FailedPrecondition (or NotFound after a re-read to
-- disambiguate row-missing vs state-mismatch).
-- name: UpdateRequestStateIfFrom :one
UPDATE asset_requests
SET state = $2,
    revision = revision + 1,
    updated_by = $3,
    update_time = now(),
    etag = md5(now()::text)
WHERE id = $1 AND state = $4
RETURNING *;

-- name: UpdateRequestAssignee :one
UPDATE asset_requests
SET assignee = $2,
    state = $3,
    revision = revision + 1,
    updated_by = $4,
    update_time = now(),
    etag = md5(now()::text)
WHERE id = $1
RETURNING *;

-- name: UpdateRequestDelivered :one
UPDATE asset_requests
SET state = 'DELIVERED',
    delivered_time = now(),
    revision = revision + 1,
    updated_by = $2,
    update_time = now(),
    etag = md5(now()::text)
WHERE id = $1
RETURNING *;

-- name: UpdateRequestApproved :one
UPDATE asset_requests
SET state = 'APPROVED',
    approved_time = now(),
    revision = revision + 1,
    updated_by = $2,
    update_time = now(),
    etag = md5(now()::text)
WHERE id = $1
RETURNING *;

-- name: DeleteRequest :exec
DELETE FROM asset_requests WHERE id = $1;
