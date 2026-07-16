-- ============================================================================
-- workflows (the container)
-- ============================================================================

-- name: CreateWorkflow :one
-- id is app-generated so the caller has it before the write. `version` is
-- always NULL at create — a workflow is created version-less, then a version
-- is minted and promoted (workflow-first insert order avoids the circular FK).
INSERT INTO workflows (id, org_id, space_id, slug, display_name, description, enabled, config, origin, annotations, created_by, updated_by)
VALUES ($1, $2, sqlc.narg('space_id'), $3, $4, $5, $6, $7, $8, $9, $10, $10)
RETURNING *;

-- name: GetWorkflow :one
-- Resolves a Workflow by its internal uuid. Used by the Worker Process and the
-- execution engine, which hold a run's workflow_id FK (a uuid, not the slug).
SELECT * FROM workflows WHERE id = $1;

-- name: GetWorkflowByParent :one
-- Resolves a Workflow from its parent + slug (the resource-name leaf).
-- space_id IS NOT DISTINCT FROM treats NULL (org-scoped) as a matchable value.
SELECT * FROM workflows
WHERE org_id = $1
  AND space_id IS NOT DISTINCT FROM sqlc.narg('space_id')
  AND slug = sqlc.arg('slug');

-- GetWorkflowByParentForUpdate resolves a Workflow by parent + slug AND locks
-- the row for the update/promote/delete/version-create tx, so the etag check
-- and the write serialize against a concurrent mutation. The slug is the
-- resource-name leaf, so handlers resolve their target by scope + slug in one
-- statement.
-- name: GetWorkflowByParentForUpdate :one
SELECT * FROM workflows
WHERE org_id = $1
  AND space_id IS NOT DISTINCT FROM sqlc.narg('space_id')
  AND slug = sqlc.arg('slug')
FOR UPDATE;

-- name: WorkflowSlugsByIDs :many
-- Maps a set of workflow uuids to their slug. The scope-wide run listings
-- (the workflows/- wildcard) return workflow_runs rows carrying only the
-- workflow_id uuid FK; the run's resource name needs the workflow slug, so the
-- page's distinct workflow ids are resolved in one round-trip (no N+1).
SELECT id, slug FROM workflows WHERE id = ANY(@ids::uuid[]);

-- name: UpdateWorkflow :one
-- Masked update of the container fields. `version` (promoted pointer) and
-- `origin` are set elsewhere (SetWorkflowVersion / create), never here.
UPDATE workflows
SET display_name = COALESCE(sqlc.narg('display_name'), display_name),
    description = COALESCE(sqlc.narg('description'), description),
    enabled = COALESCE(sqlc.narg('enabled'), enabled),
    config = COALESCE(sqlc.narg('config'), config),
    annotations = COALESCE(sqlc.narg('annotations'), annotations),
    updated_by = $2,
    update_time = now(),
    etag = md5(now()::text)
WHERE id = $1
RETURNING *;

-- name: SetWorkflowVersion :one
-- Promote: point the container at one of its OWN versions, making it live.
-- The join enforces v.workflow_id = w.id — fk_workflows_version only
-- guarantees the version row exists, NOT that it belongs to this workflow, so
-- without the join a cross-workflow promote would corrupt the
-- container→definition link (Workflow A executing B's definition). No row is
-- returned (→ ErrNoRows) when the version isn't this workflow's; the handler
-- maps that to FailedPrecondition. Serialized under the workflow row lock taken
-- by GetWorkflowByParentForUpdate.
UPDATE workflows w
SET version = v.id,
    updated_by = $2,
    update_time = now(),
    etag = md5(now()::text)
FROM workflow_versions v
WHERE w.id = $1
  AND v.id = @version
  AND v.workflow_id = w.id
RETURNING w.*;

-- name: DeleteWorkflow :exec
DELETE FROM workflows WHERE id = $1;

-- name: CountActiveWorkflowRuns :one
-- Counts a workflow's runs in an active state (RUNNING or WAITING). Used by
-- DeleteWorkflow's force guard: with force=false a nonzero count blocks the
-- delete (the caller must cancel the runs or pass force=true).
SELECT count(*) FROM workflow_runs
WHERE workflow_id = $1
  AND state IN ('RUNNING', 'WAITING');

-- name: CancelActiveWorkflowRuns :exec
-- Force-cancels a workflow's active runs (DB state only). DeleteWorkflow calls
-- this under force=true before dropping the workflow. The rows are then
-- removed by the FK cascade, so this update models intent for Phase 6, when
-- cancelling must also stop the River job backing each run.
UPDATE workflow_runs
SET state = 'CANCELLED',
    end_time = COALESCE(end_time, now())
WHERE workflow_id = $1
  AND state IN ('RUNNING', 'WAITING');

-- name: ListWorkflowsByParent :many
-- Keyset pagination on id. Fetch page_limit+1 to detect a next page.
SELECT * FROM workflows
WHERE org_id = @org_id
  AND space_id IS NOT DISTINCT FROM sqlc.narg('space_id')
  AND (sqlc.narg('cursor')::uuid IS NULL OR id > sqlc.narg('cursor'))
ORDER BY id
LIMIT @page_limit;

-- ============================================================================
-- workflow_versions (immutable definitions)
-- ============================================================================

-- name: NextWorkflowVersionNumber :one
-- The next monotonic version_number for a workflow. Called inside the create
-- tx (under the workflow row lock) so concurrent version creates don't collide.
SELECT COALESCE(MAX(version_number), 0) + 1 AS next_version_number
FROM workflow_versions
WHERE workflow_id = $1;

-- name: CreateWorkflowVersion :one
INSERT INTO workflow_versions (id, workflow_id, version_number, note, definition, created_by)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetWorkflowVersion :one
SELECT * FROM workflow_versions WHERE id = $1;

-- name: WorkflowVersionNumbersByIDs :many
-- Maps a set of version uuids to their monotonic version_number. A Workflow's
-- promoted `version` column stores the version uuid, but the resource name
-- renders the number — List/Get resolve the page's promoted pointers in one
-- round-trip (no N+1 per workflow).
SELECT id, version_number FROM workflow_versions
WHERE id = ANY(@ids::uuid[]);

-- name: GetWorkflowVersionByNumber :one
-- Resolves a version from its parent workflow + monotonic {version} segment.
SELECT * FROM workflow_versions
WHERE workflow_id = $1
  AND version_number = $2;

-- name: ListWorkflowVersions :many
-- Keyset pagination on id (uuidv7 is time-ordered, matching version_number
-- order). Fetch page_limit+1 to detect a next page.
SELECT * FROM workflow_versions
WHERE workflow_id = @workflow_id
  AND (sqlc.narg('cursor')::uuid IS NULL OR id > sqlc.narg('cursor'))
ORDER BY id
LIMIT @page_limit;

-- name: DeleteWorkflowVersion :exec
DELETE FROM workflow_versions WHERE id = $1;

-- ============================================================================
-- workflow_runs (executions)
-- ============================================================================

-- name: CreateWorkflowRun :one
-- A run is created PENDING; output/error/end_time are filled in later via
-- UpdateWorkflowRunState. triggered_by is NULL for system triggers. org_id /
-- space_id are the run's workflow's scope, denormalized for scope-wide listing.
INSERT INTO workflow_runs (id, workflow_id, org_id, space_id, version_id, state, trigger, subject, input, steps, triggered_by)
VALUES ($1, $2, $3, sqlc.narg('space_id'), $4, $5, $6, $7, sqlc.narg('input'), $8, sqlc.narg('triggered_by'))
RETURNING *;

-- name: GetWorkflowRun :one
SELECT * FROM workflow_runs WHERE id = $1;

-- name: GetWorkflowRunForUpdate :one
-- Locks the run row for a cancel/transition tx so the terminal-state check and
-- the state write serialize against a concurrent transition — the Phase-6
-- engine advancing the run, or DeleteWorkflow's force cancel (both take
-- run-row locks).
SELECT * FROM workflow_runs WHERE id = $1 FOR UPDATE;

-- name: ListWorkflowRuns :many
-- Runs of one workflow. Keyset pagination on id (fetch page_limit+1 to detect a
-- next page), with an optional state filter (NULL = all states). order_by is not
-- honored — runs are always id (creation) order, the keyset column.
SELECT * FROM workflow_runs
WHERE workflow_id = @workflow_id
  AND (sqlc.narg('state')::text IS NULL OR state = sqlc.narg('state'))
  AND (sqlc.narg('cursor')::uuid IS NULL OR id > sqlc.narg('cursor'))
ORDER BY id
LIMIT @page_limit;

-- name: ListWorkflowRunsByOrg :many
-- Org-scope wildcard listing (organizations/{org}/workflows/-/runs): ALL runs in
-- the org — both org-direct runs (space_id NULL) and runs of the org's
-- space-scoped workflows (the "org + all its spaces" global view). Keyset on id
-- via idx_workflow_runs_org, optional state filter.
SELECT * FROM workflow_runs
WHERE org_id = @org_id
  AND (sqlc.narg('state')::text IS NULL OR state = sqlc.narg('state'))
  AND (sqlc.narg('cursor')::uuid IS NULL OR id > sqlc.narg('cursor'))
ORDER BY id
LIMIT @page_limit;

-- name: ListWorkflowRunsBySpace :many
-- Space-scope wildcard listing
-- (organizations/{org}/spaces/{space}/workflows/-/runs): runs of that space's
-- workflows. Keyset on id via idx_workflow_runs_space, optional state filter.
SELECT * FROM workflow_runs
WHERE space_id = @space_id
  AND (sqlc.narg('state')::text IS NULL OR state = sqlc.narg('state'))
  AND (sqlc.narg('cursor')::uuid IS NULL OR id > sqlc.narg('cursor'))
ORDER BY id
LIMIT @page_limit;

-- name: UpdateWorkflowRunSteps :exec
-- Checkpoints live per-step progress into the steps JSONB as the executor
-- walks the run, WITHOUT touching state or any lifecycle field. Guarded on
-- state = 'RUNNING' so a step write that lands after a concurrent cancel
-- (state → CANCELLED) or terminal finalize is a no-op — it can never resurrect
-- a run out of a terminal state. A no-match (0 rows) is expected and ignored.
UPDATE workflow_runs
SET steps = $2
WHERE id = $1 AND state = 'RUNNING';

-- name: UpdateWorkflowRunState :one
-- Advances a run's lifecycle. state is always set; output/steps/error/end_time
-- are masked (a nil arg leaves the column unchanged) so a mid-run step update
-- doesn't clobber a not-yet-set terminal field.
UPDATE workflow_runs
SET state = $2,
    output = COALESCE(sqlc.narg('output'), output),
    steps = COALESCE(sqlc.narg('steps'), steps),
    error = COALESCE(sqlc.narg('error'), error),
    start_time = COALESCE(sqlc.narg('start_time'), start_time),
    end_time = COALESCE(sqlc.narg('end_time'), end_time)
WHERE id = $1
RETURNING *;
