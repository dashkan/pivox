# Converting a static-sqlc `List` to a dynamic AIP-160/132 keyset list

Status: procedure (2026-07). Companion to `docs/aip-list-audit.md`, which
inventories every `List` RPC and its gaps. This doc is the **mechanical
recipe** a future agent follows to convert any static-sqlc list into a
dynamic **AIP-160 filtered + AIP-132 sorted + compound-cursor keyset**
list, built on `internal/filter`.

**Worked example (the pilot): `ListConnectors`.** Every step below cites the
connectors implementation. Read these four files alongside this doc:

- `internal/filter/keyset.go` — `PlanOrderBy`, `BuildListQuery`, `ListQuery`,
  `Predicate`, `KeysetCursor`, `OrderByPlan` (the reusable engine).
- `internal/filter/token.go` — `EncodeCursor` / `DecodeCursor` (the
  compound-cursor page-token codec).
- `internal/filter/declarations.go` — `ConnectorFilter()` (the field
  whitelist).
- `internal/service/connectors/server.go` — `ListConnectors` (the handler
  assembly) + `connectorSortValue` (the row→cursor extractor).
- `internal/service/connectors/connectors_list_e2e_test.go` +
  `internal/filter/{keyset_test.go,token_test.go}` (the tests).

The transpiler (`internal/filter/transpiler.go`) already supports AND/OR/NOT,
`=`/`!=`/`<`/`<=`/`>`/`>=`, `:` (substring / JSONB-has), `timestamp("…")`,
JSONB dot-traversal, bare-literal search, and a JSONB-key injection guard. You
almost never touch it — you only add a `ResourceFilter` declaration that names
the columns it may reach.

---

## 0. Does this pattern apply?

Use this recipe for any dynamic AIP-160/132 list. It fits both:

- **Leveled** resources (row lives at org level *or* under a space —
  `space_id` nullable; connectors, secrets, workflows) whose base scope is
  `org_id = … AND space_id IS NOT DISTINCT FROM …`.
- **Flat single-parent** resources (one `parent_id = $` predicate: spaces,
  api keys, tags, requests, assets) whose base scope is a single equality.

Both supply their base scope as handler-built `ListQuery.Base` predicates and
run through `filter.BuildListQuery` — the single compound-cursor engine. (The
older `filter.Query` engine, which took a single `ParentColumn` `=` predicate
baked into the `ResourceFilter`, has been **deleted**; every handler is on
`BuildListQuery` now.)

Rollup (`spaces/-` wildcard, AIP-159) is **out of scope** here — keep the
existing single-scope partition. Add rollup later per audit §G.

---

## 1. Declare the field whitelist (`internal/filter/declarations.go`)

Add a `XxxFilter()` returning a `*ResourceFilter`. This is the **only** place
column names are chosen; user input never names a column. Model on
`ConnectorFilter()`.

```go
func ConnectorFilter() *ResourceFilter {
    return &ResourceFilter{
        Filterable: map[string]FilterableField{
            "displayName": {Column: "display_name", Type: filtering.TypeString, AllowPartial: true},
            "createTime":  {Column: "create_time", Type: filtering.TypeTimestamp},
            "updateTime":  {Column: "update_time", Type: filtering.TypeTimestamp},
            // JSONB label map: {Column: "annotations", Type: filtering.TypeMap(...), JSONB: true}
        },
        Sortable: map[string]SortableField{
            "displayName": {Column: "display_name", Type: filtering.TypeString},
            "createTime":  {Column: "create_time", Type: filtering.TypeTimestamp}, // Type REQUIRED on timestamps
            "updateTime":  {Column: "update_time", Type: filtering.TypeTimestamp},
        },
        Table:         "connectors",
        SoftDelete:    false,   // true adds `delete_time IS NULL` (only if the table has it)
        DefaultFields: []string{"displayName"}, // fields a bare `foo` term searches
        // Base scope (org_id / space_id / parent_id) is handler-supplied via
        // ListQuery.Base — never baked into the ResourceFilter.
    }
}
```

Rules:

- `AllowPartial: true` on a text field makes `field = "foo*"` lower to `ILIKE`
  (and `field : "foo"` is always substring `ILIKE %foo%`).
- **`SortableField.Type` MUST be set to `filtering.TypeTimestamp` on any
  timestamp sort column.** It is what tells `DecodeCursor` to parse the page
  token's encoded sort value back into a `time.Time`. Omitting it silently
  degrades a timestamp cursor to string comparison. Non-timestamp columns
  leave `Type` nil (treated as string).
- Only declare columns you intend to expose. Anything not in `Filterable`
  errors as an unknown field; anything not in `Sortable` errors as an unknown
  order_by field.
- **Every column registered in `Sortable` MUST be `NOT NULL`.** The
  compound-cursor keyset compares `(col, id)` tuples; a NULL makes the row
  comparison evaluate to UNKNOWN and silently drops/duplicates rows across page
  boundaries. See "The compound-cursor keyset" below for the mechanism and the
  nullable-column escape hatch.

### The engine's per-resource defaults (`DefaultOrder` / `DefaultPageSize` / `DefaultConditions`)

The `ResourceFilter` carries three optional "defaults" knobs, all consumed by
the compound-cursor path and **all inert when unset** — a resource that declares
none behaves exactly as it did before the knobs existed (id-ASC order, 100/1000
page-size policy, no extra predicate). They are server-controlled declarations,
never request input.

- **`DefaultOrder string`** — the AIP-132 order applied when the client sends no
  `order_by`. `PlanOrderBy` parses it exactly like a client order_by, with one
  addition: the token `"id"` (or an empty/unset value) means the compact id-only
  keyset (`Field == ""`) with the declared direction — so `DefaultOrder: "id
  desc"` gives a newest-first list whose token is still the 16-byte id cursor
  (`ORDER BY id DESC`, resume `id < $cursor`). Any other token must be a
  registered `Sortable` field, in which case the default uses the compound
  `(col, id)` cursor. Unset → historical id-ASC default. (aichat is the first
  DESC default: `DefaultOrder: "id desc"` keeps the four AiChat lists
  newest-first.)
- **`DefaultPageSize int32` / `MaxPageSize int32`** — applied by
  `filter.ClampPageSize(rf, req.GetPageSize())`, the single shared helper that
  replaced the per-handler `clampPageSize` copies. A `page_size <= 0` becomes
  `DefaultPageSize` (unset → 100); anything above `MaxPageSize` (unset → 1000) is
  capped. Call `filter.ClampPageSize(rf, req.GetPageSize())` in the handler
  instead of hand-rolling the clamp.
- **`DefaultConditions []Predicate`** — server-declared predicates ALWAYS ANDed
  into the query, even with no client filter (e.g. hide archived rows by
  default). They reuse the exact `Predicate` machinery as `ListQuery.Base` — each
  must carry exactly one `%s` / one bound `Arg` — and `BuildListQuery` numbers
  them alongside the base scope, so `$N` alignment holds with base + filter +
  cursor all present. Nil → none.

## 2. Add a `Scan` helper (`internal/filter/scan.go`)

`BuildListQuery` emits `SELECT *`, so you need a `ScanXxx(rows pgx.Rows)
([]db.Xxx, error)` whose destination order **exactly matches the table's
column order in the init migration**. Model on `ScanConnectors`. A missing or
reordered field fails at runtime with a pgx column-count mismatch that aborts
the RPC — copy the column order from `000001_init.up.sql`, not from memory.

## 3. Rewrite the handler

Replace the static-sqlc list body with the assembly below (from
`ListConnectors`). The base scope comes from the interceptor-resolved context
(`server.MustResolvedOrgFromContext` / `ResolvedSpaceFromContext`), applied by
YOU — a filter can only narrow within it, never widen it.

```go
func (s *XxxServer) ListXxx(ctx, req) (*pb.ListXxxResponse, error) {
    orgID, spaceID, prefix := s.scope(ctx)
    rf := filter.XxxFilter()
    pageSize := clampPageSize(req.GetPageSize())          // default 100, cap 1000

    plan, err := filter.PlanOrderBy(rf, req.GetOrderBy()) // "" = default id order
    if err != nil {
        return nil, apierr.InvalidArgument(apierr.FieldViolation("order_by", err.Error()))
    }
    cursor, err := filter.DecodeCursor(s.codec, plan, req.GetPageToken())
    if err != nil {
        return nil, apierr.InvalidArgument(apierr.FieldViolation("page_token", "invalid or malformed"))
    }

    sql, args, err := filter.BuildListQuery(filter.ListQuery{
        Resource: rf,
        Base: []filter.Predicate{                          // the NON-NEGOTIABLE base scope
            {SQL: "org_id = %s", Arg: orgID},
            {SQL: "space_id IS NOT DISTINCT FROM %s", Arg: spaceID},
        },
        Filter:   req.GetFilter(),
        Order:    plan,
        PageSize: pageSize,
        Cursor:   cursor,
    })
    if err != nil { // only source is the filter transpiler (bad user filter)
        return nil, apierr.InvalidArgument(apierr.FieldViolation("filter", err.Error()))
    }

    pgxRows, err := s.pool.Query(ctx, sql, args...)
    if err != nil { return nil, apierr.Internal(err, "list xxx") }
    rows, err := filter.ScanXxx(pgxRows)
    if err != nil { return nil, apierr.Internal(err, "list xxx") }

    var nextPageToken string
    if int32(len(rows)) > pageSize {
        rows = rows[:pageSize]
        last := rows[pageSize-1]                            // LAST returned row, never rows[pageSize]
        nextPageToken, err = filter.EncodeCursor(s.codec, plan, xxxSortValue(plan, last), last.ID)
        if err != nil { return nil, apierr.Internal(err, "encode page token") }
    }
    // …resolve actors, convert each row to proto…
}
```

`Predicate.SQL` is a fixed template with **one** `%s` that `BuildListQuery`
replaces with the bound `$N`; the value travels in `Predicate.Arg` and is
bound, never interpolated.

## 4. Add the row→cursor extractor

`EncodeCursor` needs the last row's value for the sort column, as a string.
Write a tiny resource-specific function (model on `connectorSortValue`):

```go
func xxxSortValue(plan filter.OrderByPlan, r db.Xxx) string {
    switch plan.Field {
    case "displayName": return r.DisplayName
    case "createTime":  return r.CreateTime.UTC().Format(time.RFC3339Nano) // micro precision, exact round-trip
    case "updateTime":  return r.UpdateTime.UTC().Format(time.RFC3339Nano)
    default:            return "" // id-only ordering — value unused
    }
}
```

Timestamps MUST use `time.RFC3339Nano` so `DecodeCursor` reparses them to the
exact `time.Time` (matching the `Type: filtering.TypeTimestamp` in step 1).

## 5. Delete the dead static sqlc query + regenerate

Remove the `-- name: ListXxxByParent :many` block from
`internal/db/queries/xxx.sql` (leave a comment pointing here), then regenerate:

```sh
cd internal/db && sqlc generate     # v1.31.1; or `make proto-generate`
```

Confirm the generated method is gone and the module builds
(`go build ./...`). Other RPCs (Get/Create/Update/Delete) stay on sqlc,
untouched.

---

## Historical: migrating the legacy `filter.Query` handlers (complete)

> This migration is **done** and the legacy `filter.Query` engine has been
> **deleted**. The section is retained for the design rationale (why the
> compound cursor exists) and because its per-step notes still describe how a
> flat single-parent list is assembled on `BuildListQuery`. There are no more
> legacy handlers to convert.

A class of handlers was already **dynamic** but ran on the legacy `filter.Query`
engine: they paired an **id-only cursor** (`EncodeNextPageToken` /
`DecodeCursor`) with a `Sortable` set that exposed **non-id** columns. That was a
latent correctness bug: a client passing `order_by=displayName` got
`ORDER BY display_name` but the keyset resumed on `id > $cursor` — sort and
keyset disagreed, so rows **dropped and duplicated across page boundaries**
whenever display_name order differed from id order. (Pinned red→green in
`TestE2E_ListSpaces_OrderByDisplayNameKeysetBoundary`: the legacy path returned
`[aa,bb,cc,bb,dd,ee]` — `bb` duplicated, `ff`/`gg` dropped — for 7 rows at
page_size 3.) The compound `(col, id)` cursor below is the fix.

**Worked example: `ListSpaces`** (`internal/service/spaces/server.go`) — the
template the fan-out (organizations, apikeys, tags, aichat, mcp) followed. The
moving parts of a flat single-parent list differ from §1–5 as follows:

1. **Make the declaration compound-capable.** In `SpaceFilter()`, set
   `SortableField.Type` on every sortable column — **`filtering.TypeTimestamp`
   on timestamp columns is mandatory** (it is what tells `DecodeCursor` to
   reparse the token's sort value into a `time.Time`); non-timestamp columns get
   `filtering.TypeString`.

2. **Soft-delete is handled by the engine, not the base scope.** `spaces` is
   soft-deleting (`SoftDelete: true`, request has `show_deleted`).
   `BuildListQuery` honors `q.Resource.SoftDelete && !q.ShowDeleted` by emitting
   a **no-arg** `delete_time IS NULL` predicate. So you do **not** put soft-delete
   in `Base` (a `Predicate` must carry exactly one `%s`/arg, and
   `delete_time IS NULL` has none) — you plumb `ShowDeleted: req.GetShowDeleted()`
   into `ListQuery` and let the engine add it. Hard-delete resources (no
   `delete_time` column) leave `SoftDelete: false` and nothing is emitted.

3. **Base scope is the flat parent predicate.** Unlike the leveled connectors
   (`org_id = … AND space_id IS NOT DISTINCT FROM …`), a flat single-parent
   resource supplies **one** base predicate:
   `Base: []filter.Predicate{{SQL: "org_id = %s", Arg: resolvedOrg.ID}}`.

4. **`sortValue` helper** — same shape as `connectorSortValue`
   (`spaceSortValue(plan, r db.Space) string`): a `switch plan.Field` returning
   the row's value for the active sort column, timestamps as
   `.UTC().Format(time.RFC3339Nano)`, `default: ""` for the id-only path.

5. **Encode via `filter.Paginate` + `filter.EncodeCursor`.** Inside the
   `Paginate` callback, encode the compound
   `EncodeCursor(codec, plan, spaceSortValue(plan, last), last.ID)` and build the
   query with `BuildListQuery`. `Paginate` keeps the trim + last-returned-row
   discipline.

6. **Nullable-column rule (per Phase C).** Every column you register as
   `Sortable` MUST be `NOT NULL` (see the keyset section below for the
   mechanism). Spaces' three sortables (`name`, `display_name`, `create_time`)
   are all `NOT NULL`, so none are demoted. If a resource has a nullable column
   a client might want to sort on, register it **filterable-only** — a client
   sorting on it then gets `InvalidArgument` from `PlanOrderBy` (not a silently
   broken keyset).

7. **Orphaned static query? Usually none — but grep before you delete.** §5
   deletes the static sqlc list query that a static→dynamic migration orphans. A
   legacy-`filter.Query` handler was **already dynamic**, so it typically has
   **no** static list query to delete and **no** sqlc regen step. Confirm with
   `grep -rn 'name: List<Resource>' internal/db/queries/` and grep the generated
   caller set. **Do not delete a shared list query**: `spaces.sql` has
   `ListSpacesPastPurgeTime`, but that is the **purge worker's** query
   (`internal/workers/purge_spaces.go`), not the RPC's — it stays. Only delete a
   query with **zero** remaining callers after your migration; when you do
   regenerate sqlc, verify the diff contains **only** the removed query (no
   schema churn).

Everything else (the handler assembly in §3, the security contract, the test
matrix) is identical to the static-sqlc recipe.

---

## The compound-cursor keyset (why it is correct)

A keyset list resumes with a `WHERE` predicate on the sort column, not
`LIMIT/OFFSET` (which the audit flags as a bug — it skips/repeats rows under
concurrent writes and re-returns page 1 when the token is never consumed).

- **Default (no `order_by`)** — order by `id`; cursor is the last row's id;
  predicate `id > $cursor`. Token is the simple encrypted 16-byte id
  (`EncodeCursor` with an empty `plan.Field`).
- **Custom `order_by`** — order by `<col> <dir>, id <dir>`. Because `<col>` is
  not unique (e.g. many rows share a `display_name`, all `''` by default), the
  **id tiebreaker is mandatory** and the cursor must encode BOTH values. The
  predicate is a Postgres row-value comparison:

  ```sql
  -- ASC:  (display_name, id) > ($sortVal, $cursorId)
  -- DESC: (display_name, id) < ($sortVal, $cursorId)
  ```

  Postgres evaluates the tuple lexicographically, which is exactly "the next
  page after (col, id)" when the operator and the `ORDER BY` direction agree.
  The id tiebreaker takes the **same** direction as the sort column so the row
  comparison stays a valid boundary. This is why duplicate sort keys neither
  drop nor repeat rows across a page boundary (see
  `TestE2E_ListConnectors_KeysetCoverage_DuplicateSortKeys`).

- **Off-by-one (CLEANUP-1)** — encode the **last returned** row
  (`rows[pageSize-1]`), never the first un-returned row (`rows[pageSize]`). The
  resume predicate is strict `>`/`<`, so encoding `rows[pageSize]` silently
  drops it on the next page.

**Limitation (deliberate):** `PlanOrderBy` accepts **at most one** order_by
field (plus the implicit id tiebreaker). A multi-field `order_by` is rejected
with `InvalidArgument` — supporting N sort columns would need an N-tuple
cursor. If a resource genuinely needs multi-column sort, extend `OrderByPlan` +
`BuildListQuery` + the token payload together; don't fake it.

**Sortable columns registered in the whitelist MUST be `NOT NULL`.** The
compound-cursor path resumes with the row-value predicate `(col, id) <op>
($sortVal, $cursorId)` under `ORDER BY col, id`. If `col` is nullable, a NULL on
either side of the comparison makes the tuple comparison evaluate to UNKNOWN
(SQL three-valued logic) rather than true/false — the boundary predicate neither
includes nor excludes those rows deterministically, so rows silently drop or
duplicate across page boundaries (the classic keyset-with-NULLs gap). The pilot's
columns are all `NOT NULL` (`display_name`, `create_time`, `update_time`, `id`),
so it is safe. For the sweep: register **only** `NOT NULL` columns as `Sortable`.
If a resource genuinely needs to sort on a nullable column, do not register it
as-is — add an explicit `NULLS FIRST/LAST` ordering plus a coalesced-cursor
scheme (so the cursor encodes a non-NULL sentinel) before shipping. Do not assume
the row comparison "just works" on a nullable column.

---

## Security (dynamic SQL — the non-negotiable contract)

- **Every value is a bound `$N` parameter.** Filter operands, cursor values,
  page size, and the base org/space scope ids all travel in the args slice.
  Only the table name, whitelisted column names, and the ASC/DESC direction are
  assembled into the SQL text — all from server-controlled declarations, never
  from the request. If you ever `fmt.Sprintf` a *value* into SQL, that's the
  bug. (The `%s` in `Predicate.SQL` and inside `BuildListQuery` is only ever
  filled with a `$N` placeholder string, never a value.)
- **Column/identifier names come only from the whitelist** (`Filterable` /
  `Sortable`). An unknown filter field or order_by field is rejected with
  `InvalidArgument`, not silently ignored.
- **The base scope is applied by the handler from context**, not derivable from
  the filter. A filter can only add ANDed predicates *within* the scope.
- **Prove it with a test.** A payload like `x' OR '1'='1` in a filter value
  must be an inert literal operand that matches nothing and errors nothing —
  see `TestE2E_ListConnectors_InjectionInert` and
  `TestBuildListQuery_InjectionOperandIsInert`. The JSONB-key injection guard
  (`TestTranspile_JSONBKey_RejectsSQLInjection`) covers dot-traversal keys.

---

## Tests to add (TDD — write these first)

Mirror the connectors suite. Unit tests in `internal/filter` for any
declaration/extension; e2e tests through `grpcharness` (NOT `MockQuerier`) for
the handler:

- **Filter:** `displayName = "x"` (exact), `displayName : "x"` (substring),
  `displayName = "x*"` (wildcard→ILIKE).
- **order_by:** each whitelisted field, asc and desc; assert the row order.
- **Keyset full coverage under a custom sort:** insert `> pageSize` rows with a
  small `page_size`, drain all pages following tokens, assert every row appears
  exactly once (no dupes, no skips) — include a **duplicate-sort-key** variant
  to exercise the id tiebreaker, and the **default id** path.
- **Rejections:** unknown filter field → `InvalidArgument`; unknown order_by
  field → `InvalidArgument`; garbage/tampered `page_token` → `InvalidArgument`.
- **Injection inert:** payload value matches nothing, errors nothing, leaves
  the data intact.
- **Scope isolation:** a second org's rows never appear; a filter cannot widen
  past the org scope.
- **Empty filter:** returns all in-scope rows.

Run `make test` (bring the stack up with `make test-up` first for a targeted
`go test ./internal/service/xxx/...`), `make lint`, and `go build ./...`.

---

## Sweep checklist (per resource, from `docs/aip-list-audit.md`)

Leveled (`spaces/-` partition, bespoke query): **ListSecrets**,
**ListWorkflows** — follow this doc verbatim. Also carry the CLEANUP-1
off-by-one fix. Update the audit's summary table row from `filter: no` /
`order_by: no` / `keyset✗` to done as each lands, in the same commit.
