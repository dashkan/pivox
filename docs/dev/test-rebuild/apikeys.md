# `internal/service/apikeys` — test rebuild spec

## Existing grpcharness coverage (kept)

- `server_integration_test.go` — full CRUD round-trip, key string
  retrieval + lookup, list w/ show_deleted, pagination

## CreateKey

- [x] Happy path returns proto without `KeyString`
- [ ] Auto-generated `KeyId` when not supplied (slug shape)
- [ ] `KeyString` is generated, stored hashed, returned plaintext
  exactly once via GetKeyString
- [ ] Permission gate: `apikeys.create`
- [ ] Refuses on DELETE_REQUESTED parent org

## GetKey / GetKeyString

- [x] GetKey happy path (integration covers)
- [x] GetKeyString returns plaintext (integration covers)
- [ ] GetKey: NotFound on unknown slug
- [ ] GetKeyString on a deleted key: behavior undefined today —
  decide and pin (likely NotFound, mirroring GetKey)
- [ ] Permission gate: `apikeys.read` for both

## UpdateKey

- [x] Display-name update happy path
- [ ] Field-mask honors `display_name` and `annotations` independently
- [ ] No-mask updates all writable fields
- [ ] Update on a deleted key: NotFound (caller can't resurrect via
  Update)

## LookupKey

- [x] Happy path resolves key string → org + key name
- [ ] Unknown key string → NotFound
- [ ] Lookup of a deleted/expired key → NotFound (verify the SQL
  filters correctly)
- [ ] Constant-time compare of the key string (verify the
  comparison goes through `subtle.ConstantTimeCompare` — this is
  a security boundary, worth one explicit test)

## DeleteKey / UndeleteKey

- [x] DeleteKey + ListKeys with show_deleted
- [ ] **#71-known issue:** UndeleteKey uses `GetApiKeyByOrgAndKeyID`
  which filters deleted records, so it currently can't find a
  soft-deleted key. The integration test has a `t.Skip` for this.
  Either fix the SQL to read deleted rows, or document that
  UndeleteKey is permanently broken and remove the RPC.

## ListKeys

- [x] Default list, pagination, show_deleted

## Drop list

- ~~The `*_ErrorPaths` matrix~~ — every handler had a paired
  "everything errors" test. Mock theater. Real DB failure modes
  are exercised by the live path; we don't need a per-handler
  "DB returned an error" assertion.

## Shape of the rewrite

- `server_integration_test.go` — already covers most of the happy
  paths. Extend with: permission gates, field-mask matrix,
  LookupKey constant-time check, UndeleteKey decision (fix or
  remove).
