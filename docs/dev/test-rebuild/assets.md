# `internal/service/assets` — test rebuild spec

## Existing grpcharness coverage (kept)

- `server_integration_test.go` — placeholder lifecycle (create →
  get → update → delete → undelete), list with show_deleted,
  pagination, "create with file" pipeline

## CreateAsset

- [x] Placeholder (no filename) → state=PLACEHOLDER
- [x] With filename → state=ACTIVE after sync pipeline
- [ ] Permission gate: `assets.create` on the parent space
- [ ] Cross-org parent → NotFound (org-resolution failure path)
- [ ] DELETE_REQUESTED parent org/space → FailedPrecondition

## GetAsset / UpdateAsset

- [x] Get + Update happy paths
- [ ] Get: NotFound on unknown slug
- [ ] Update: field-mask honors `display_name`, `annotations`,
  `expire_time` independently
- [ ] Update: no-mask updates all writable fields
- [ ] Update on a deleted asset: NotFound

## DeleteAsset / UndeleteAsset

- [x] Delete → DELETE_REQUESTED, Undelete → restored to prior state
- [ ] Delete on PLACEHOLDER vs ACTIVE: both transition the same way
- [ ] Undelete restores to PLACEHOLDER if `endpoint_id` IS NULL,
  ACTIVE otherwise (the CASE WHEN in the SQL — was just fixed in
  #75; deserves an explicit test that exercises both branches)
- [ ] Undelete on never-deleted asset → FailedPrecondition

## ListAssets

- [x] List w/ pagination + show_deleted

## Drop list

- ~~Per-handler `*_DBError` and `*_NotFound` matrix~~ — mock theater.
  NotFound paths are hit organically by the happy-path tests when
  setup is wrong; we don't need 8 explicit "X returns NotFound"
  tests.
- ~~`TestCreateAsset_DBError`, `TestUpdateAsset_DBError` etc.~~ —
  same.
- ~~`TestUndeleteAsset_NotDeleted`~~ — covered by the integration
  test's natural flow.

## Shape of the rewrite

- `server_integration_test.go` — extend with the field-mask matrix
  (one table-driven test) and the placeholder-vs-active undelete
  branch. That's it.
