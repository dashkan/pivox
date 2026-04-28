-- name: ListPermissions :many
-- Returns the entire global permission catalog. Static / code-defined
-- in v1 (seeded by the migration); the Iam.ListPermissions RPC just
-- echoes this set so clients can render UI permission pickers without
-- hardcoding the list. Ordered by permission_id for stable
-- pagination — the catalog is small (~100 rows) so v1 returns the
-- full set in one call without paging.
SELECT * FROM permissions ORDER BY permission_id;

-- name: GetPermission :one
-- Looks up a permission by its string id (e.g. 'organizations.delete').
-- Used by Iam.GetPermission and as a validity check on caller-supplied
-- permission strings in TestIamPermissions paths.
SELECT * FROM permissions WHERE permission_id = $1;
