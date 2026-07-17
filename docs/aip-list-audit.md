# AIP `List` RPC audit + implementation guidance

Status: audit only (2026-07). No code was changed in producing this
document. It is a work-order for a future session to bring every List
RPC in the Cloud Controller up to a consistent AIP-132/159/160 standard.

## Why this exists

The workflow-resources work standardized four things that should
propagate across the whole List surface:

1. **Rollup via the `-` wildcard (AIP-159).** A parent-level list can
   roll up items from nested collections, returning each row with its
   true (deeper) resource name. Shipped for `ListWorkflowRuns`:
   `organizations/{org}/workflows/-/runs` returns runs across every
   workflow, including space-scoped ones. The same option is meaningful
   for other *leveled* resources (e.g.
   `organizations/{org}/spaces/-/connectors`).
2. **Server-side AIP-160 `filter`.** Many list queries ignore the
   `filter` field entirely (the SQL literally says "AIP-160 filter …
   not yet wired").
3. **Server-side AIP-132 `order_by`.** Same — accepted and ignored.
4. **Keyset pagination off-by-one (CLEANUP-1).** ~14 handlers encode the
   next-page token as `results[pageSize].ID` (the first *un*-returned
   row) against an `id > cursor` predicate, silently dropping one row
   per page boundary. The correct form is `results[pageSize-1].ID` (the
   last *returned* row).

## Reference implementation — WorkflowRuns

`internal/service/workflows/workflow_runs.go` is the fully-modernized
example. Copy its shapes.

- **Correct keyset pagination** —
  `renderRunPage` (`workflow_runs.go:421-450`). The token is the LAST
  returned row: `filter.EncodeNextPageToken(s.codec, rows[pageSize-1].ID)`
  at `workflow_runs.go:432`, and the trim `rows = rows[:pageSize]` happens
  *after*. The inline comment at `:430-431` explains why `rows[pageSize]`
  is wrong.
- **`-` wildcard rollup** — `ListWorkflowRuns` (`workflow_runs.go:224-321`).
  `isWorkflowWildcard` (`:326`) detects the `/workflows/-` suffix; the
  handler then dispatches to `ListWorkflowRunsBySpace` or
  `ListWorkflowRunsByOrg` (scope-wide queries that drop the single-parent
  predicate), and rebuilds each row's true resource name via batched slug
  lookups (`orgRunNames` `:339`, `resolveWorkflowSlugs` `:393`) — no N+1.
  The rollup queries live in `internal/db/queries/workflows.sql:189`
  (`ListWorkflowRunsByOrg`) and `:201` (`ListWorkflowRunsBySpace`), keyed
  on `org_id` / `space_id` respectively instead of `workflow_id`.
- **Server-side filter** — `parseRunStateFilter` (`workflow_runs.go:457`).
  This is a *narrow, hand-rolled* AIP-160 parse (only `state = "X"`), not
  the generic `internal/filter` engine, because runs use a bespoke sqlc
  query rather than the `filter.Query` path. It shows how to reject
  unsupported filter shapes with `apierr.InvalidArgument` rather than
  silently ignoring them.

Note: workflow runs deliberately still **ignores `order_by`** (runs are
always id/creation order — the keyset column). That is an acceptable
documented deviation, not a template to copy for resources where sort is
meaningful.

## The helper toolbox

### `internal/filter` (the generic AIP-160/132 engine)

Files: `query.go`, `transpiler.go`, `orderby.go`, `declarations.go`,
`scan.go`, `token.go`.

- **`filter.Query(ctx, dbtx, rf, params)`** (`query.go:27`) — builds and
  runs `SELECT * FROM <rf.Table> WHERE … ORDER BY … LIMIT pageSize+1`.
  `QueryParams` (`query.go:14`) carries `Filter`, `OrderBy`, `ParentID`,
  `UserID`, `PageSize`, `Cursor` (opaque page_token), `ShowDeleted`,
  `Codec`. It wires filter + order_by + parent + soft-delete + cursor in
  one call. It over-fetches by one (`LIMIT pageSize+1`) so the caller can
  detect a further page.
- **`ResourceFilter`** (`declarations.go:27`) — per-resource metadata:
  `Filterable` map (field → SQL column + CEL type + `AllowPartial` for
  ILIKE + `JSONB`), `Sortable` map (field → column), `Table`,
  `SoftDelete`, default `OrderBy`, `CursorColumn`/`CursorDirection`,
  `ParentColumn`, `UserColumn` (implicit access-control predicate).
  Existing declarations: `SpaceFilter`, `OrganizationFilter`,
  `TagKeyFilter`, `TagValueFilter`, `TagBindingFilter`,
  `ConversationFilter`, `MessageFilter`, `ArtifactFilter`,
  `ArtifactVersionFilter`, `ApiKeyFilter`.
- **`ParseOrderBy(rf, orderBy)`** (`orderby.go:17`) — AIP-132 string →
  SQL `ORDER BY`. `"name"` always allowed; other fields must be in
  `rf.Sortable`. Unknown field/direction → error.
- **`Transpile(rf, filter, paramIdx)`** (`transpiler.go`) — AIP-160
  expression → parameterized SQL `WHERE` fragment, using `rf.Filterable`.
- **`Scan*` helpers** (`scan.go`) — `filter.Query` returns raw
  `pgx.Rows`; each resource has a `ScanXxx(rows)` that maps to
  `[]db.Xxx`. A resource newly adopting `filter.Query` needs a matching
  Scan helper.

**Caveat for leveled resources.** `filter.Query` supports exactly one
`ParentColumn` with an `=` predicate. It does **not** express the
`space_id IS NOT DISTINCT FROM narg('space_id')` partition that the
org-or-space resources use (connectors/secrets/workflows), nor the `-`
rollup. For those, the cleaner path is to keep the bespoke sqlc query and
add filter/order_by fragments to it (or add a `…ByOrg` rollup variant),
rather than force-fit `filter.Query`. Flat single-parent resources
(gateways, requests, assets, members, roles) can migrate to
`filter.Query` directly.

### `internal/filter/token.go` — pagination tokens

- `filter.EncodeNextPageToken(codec, id)` (`token.go:14`) — encrypts a
  UUID into an opaque token.
- Decode happens inside `filter.Query` via the `Cursor` param; bespoke
  handlers decode manually (`s.codec.Decrypt`, e.g.
  `secrets/server.go:159`).

### The pagination one-liner fix

Every buggy handler has the same shape:

```go
// BUGGY: encodes the first UN-returned row, then trims — skips it next page
if int32(len(results)) > pageSize {
    nextPageToken, err = filter.EncodeNextPageToken(s.codec, results[pageSize].ID)
    ...
    results = results[:pageSize]
}
```

Fix (match `storage/gateways.go:303-308` / `workflow_runs.go:430-437`):

```go
// CORRECT: trim first, then encode the LAST RETURNED row
if int32(len(results)) > pageSize {
    results = results[:pageSize]
    nextPageToken, err = filter.EncodeNextPageToken(s.codec, results[pageSize-1].ID)
    ...
}
```

Both orders of the two statements are fine as long as the encoded index
is `pageSize-1` (the last returned row), not `pageSize`.

### `internal/apierr`

Use `apierr.InvalidArgument(apierr.FieldViolation("filter", …))` /
`"order_by"` to reject unsupported filter/sort shapes (see
`dashboards/dashboards.go:160-167` for the explicit-rejection pattern,
preferable to silent ignore while wiring is pending).

## Summary table

Scope legend: **org** = org-only; **org+space** = leveled (row lives at
org level *or* under a space, `space_id` nullable); **nested** = child of
a non-scope resource (workflow/conversation/gateway/tagKey); **caller** =
scoped to the authenticated principal; **global** = static catalog.

Pagination legend: **keyset✓** = correct; **keyset✗** = off-by-one
CLEANUP-1 bug; **offset** = offset/limit token (no off-by-one, but not
keyset); **broken** = token emitted but never consumed (offset hardcoded
0); **none** = returns full set.

| RPC | Handler (file:line) | Scope | Rollup applicable? | filter wired? | order_by wired? | Pagination |
|---|---|---|---|---|---|---|
| ListWorkflowRuns | workflows/workflow_runs.go:224 | nested (workflow) | **DONE** (`workflows/-`) | partial (`state` only) | ignored (by design) | **keyset✓** :432 |
| ListWorkflows | workflows/server.go:317 | org+space | yes (`spaces/-`) | no (proto has) | no (proto has) | keyset✗ :338 |
| ListWorkflowVersions | workflows/server.go:714 | nested (workflow) | low value | n/a (no proto field) | n/a (no proto field) | keyset✗ :742 |
| ListConnectors | connectors/server.go:149 | org+space | yes (`spaces/-`) | **DONE** (AIP-160) | **DONE** (AIP-132) | **keyset✓** (compound cursor) |
| ListSecrets | secrets/server.go:146 | org+space | yes (`spaces/-`) | no (proto has) | no (proto has) | keyset✗ :180 |
| ListStorageGateways | storage/gateways.go:264 | org | n/a | no (proto has) | no (proto has) | **keyset✓** :305 |
| ListEndpoints | storage/endpoints.go:253 | nested (gateway) | low value | no (proto has) | no (proto has) | none |
| ListAgents | storage/agents.go:93 | nested (gateway) | low value | no (proto has) | no (proto has) | none |
| ListKeys | apikeys/server.go:157 | org | n/a | **yes** | **yes** | keyset✗ :195 |
| ListSpaces (api) | spaces/server.go:162 | org | n/a | **yes** | **yes** | keyset✗ :201 |
| ListMembers (org) | organizations/members.go:105 | org | n/a | no (proto has) | no (proto has) | offset :138 |
| ListMembers (space) | spaces/members.go:117 | space | n/a | no (proto has) | no (proto has) | offset :151 |
| ListOrganizations | organizations/server.go:152 | caller | n/a | **ignored** (`_ = req`) | **ignored** | none |
| ListDomains | organizations/domains.go:188 | org | n/a | n/a (no proto field) | n/a | none (proto has page_size/token, unwired) |
| ListDashboards | dashboards/dashboards.go:113 | org / space | n/a | rejected (space branch) | rejected (space branch) | offset (space) / none (org) |
| ListTagKeys | tags/keys.go:92 | org | n/a | **yes** | **yes** | keyset✗ :125 |
| ListTagValues | tags/values.go:112 | nested (tagKey) | low value | **yes** | **yes** | keyset✗ :149 |
| ListTagBindings | tags/bindings.go:87 | resource-parent | n/a | **yes** | **yes** | keyset✗ :115 |
| ListEffectiveTags | tags/bindings.go:229 | resource-parent | n/a | n/a (no proto field) | n/a | none |
| ListConversations | aichat/conversations.go:43 | nested (org+user) | maybe (`users/-`, gated) | **yes** | **yes** | keyset✗ :90 |
| ListMessages | aichat/messages.go:41 | nested (conversation) | low value | **yes** | **yes** | keyset✗ :79 |
| ListArtifacts | aichat/artifacts.go:44 | nested (conversation) | low value | **yes** | **yes** | keyset✗ :82 |
| ListArtifactVersions | aichat/artifact_versions.go:44 | nested (artifact) | low value | **yes** | **yes** | keyset✗ :82 |
| ListRequests | requests/server.go | space | (space→org possible) | **yes** | **yes** | **keyset✓** |
| ListAssets | assets/server.go | space | (space→org possible) | **yes** | **yes** | **keyset✓** |
| ListRoles | iam/server.go:168 | org | n/a | no (proto has) | no (proto has) | none (+ N+1) |
| ListPermissions | iam/server.go:126 | global | n/a | n/a | n/a | none (by design) |
| ListAccountOrganizations | iam/server.go:102 | caller (`accounts/me`) | n/a | n/a | n/a | none (AIP-158 disabled) |
| ListOperations | operations/server.go:132 | caller | n/a | **ignored** (proto has) | n/a | page_size only (no next token/cursor) |
| ListOrgs (mcp) | mcp/orgs.go:38 | caller | n/a | n/a (`name_prefix`) | n/a | none (single page) |
| ListSpaces (mcp) | mcp/spaces.go:60 | org | n/a | via `name_prefix` | n/a | **keyset✓** :114 |
| ListUsers | *unimplemented* | org | n/a | — | — | — |
| ListGroups | *unimplemented* | org | n/a | — | — | — |
| ListGroupMembers | *unimplemented* | nested (group) | n/a | — | — | — |
| ListInvitations | *unimplemented* | org | n/a | — | — | — |
| ListAssetVersions | *unimplemented* | nested (asset) | low value | — | — | — |
| ListLineItems | *unimplemented* | nested (request) | n/a | — | — | — |

**Totals:** 37 List RPCs declared (36 Pivox + `google.longrunning.ListOperations`).
31 implemented, 6 unimplemented (fall through to the generated
`Unimplemented*Server` returning `codes.Unimplemented`).

Gap counts across the 31 implemented:
- **Pagination off-by-one (keyset✗):** 11 —
  ListWorkflows, ListWorkflowVersions, ListSecrets,
  ListKeys, ListSpaces(api), ListTagKeys, ListTagValues, ListTagBindings,
  ListConversations, ListMessages, ListArtifacts, ListArtifactVersions.
  (12 handler sites; ListArtifacts + ListArtifactVersions counted
  separately gives 12.) ListConnectors is DONE — the compound-cursor
  keyset pilot (see section D).
- **Pagination broken (token never consumed):** 0 — ListRequests and
  ListAssets were the last two; both are now compound-cursor keyset lists
  (RequestFilter / AssetFilter, see section C).
- **Filter present in proto but not wired (or ignored):** 11 —
  ListWorkflows, ListConnectors, ListSecrets, ListStorageGateways,
  ListEndpoints, ListAgents, ListMembers(org), ListMembers(space),
  ListOrganizations, ListRequests, ListAssets, ListRoles, ListOperations.
- **order_by present in proto but not wired:** 11 (same set minus
  ListOperations/ListDomains which lack the field; plus ListWorkflows
  etc.).
- **Rollup applicable, not done:** 4 clearly useful — ListWorkflows,
  ListConnectors, ListSecrets (`spaces/-`), plus ListConversations
  (`users/-`, permission-gated). Several "low value" nested cases exist
  but aren't worth the query.

## Per-resource guidance

Grouped by the change they need. Each entry names the exact query +
handler + line.

### A. Already correct — reference these, don't touch

- **ListWorkflowRuns** (`workflows/workflow_runs.go`) — full reference:
  correct keyset, `-` rollup, server-side state filter.
- **ListStorageGateways** (`storage/gateways.go:305`) — correct keyset
  (still needs filter/order_by; see section C).
- **ListSpaces (mcp)** (`mcp/spaces.go:114`) — correct keyset via
  `filter.Query`.

### B. Pagination off-by-one — one-liner fix (do first)

All use the buggy `results[pageSize].ID` / `rows[pageSize].ID` form.
Change the index to `pageSize-1` (see "The pagination one-liner fix").
Cite each:

| Handler | Line to change | Current | Fix |
|---|---|---|---|
| ListWorkflows | workflows/server.go:338 | `rows[pageSize].ID` | `rows[pageSize-1].ID` (trim first) |
| ListWorkflowVersions | workflows/server.go:742 | `rows[pageSize].ID` | `rows[pageSize-1].ID` |
| ListSecrets | secrets/server.go:180 | `rows[pageSize].ID` | `rows[pageSize-1].ID` |
| ListKeys | apikeys/server.go:195 | `results[pageSize].ID` | `results[pageSize-1].ID` |
| ListSpaces (api) | spaces/server.go:201 | `results[pageSize].ID` | `results[pageSize-1].ID` |
| ListTagKeys | tags/keys.go:125 | `results[pageSize].ID` | `results[pageSize-1].ID` |
| ListTagValues | tags/values.go:149 | `results[pageSize].ID` | `results[pageSize-1].ID` |
| ListTagBindings | tags/bindings.go:115 | `results[pageSize].ID` | `results[pageSize-1].ID` |
| ListConversations | aichat/conversations.go:90 | `results[pageSize].ID` | `results[pageSize-1].ID` |
| ListMessages | aichat/messages.go:79 | `results[pageSize].ID` | `results[pageSize-1].ID` |
| ListArtifacts | aichat/artifacts.go:82 | `results[pageSize].ID` | `results[pageSize-1].ID` |
| ListArtifactVersions | aichat/artifact_versions.go:82 | `results[pageSize].ID` | `results[pageSize-1].ID` |

TDD: for each, a two-page integration test through `grpcharness` that
inserts `pageSize+1` rows, reads page 1, follows the token, and asserts
no row is dropped at the boundary. The bug is invisible unless the test
crosses a page boundary with an exact multiple.

### C. Pagination broken (rewrite to keyset) — ListRequests, ListAssets — **DONE**

These were worse than an off-by-one: the query was
`LIMIT pageSize+1 OFFSET 0` with the offset **hardcoded to 0**, and the
emitted `nextPageToken` (`rows[pageSize].ID.String()`) was **never
consumed** — no `page_token` decode existed. Every "next page" re-returned
page 1.

Both are now fully dynamic AIP-160 filtered + AIP-132 sorted +
**compound-cursor** keyset lists built on `internal/filter.BuildListQuery`
(base scope `space_id = $`), matching the connectors/spaces pilots. See
`filter.RequestFilter` / `filter.AssetFilter`, `filter.ScanRequests` /
`filter.ScanAssets`, and the handler assembly in `requests/server.go`
(ListRequests) and `assets/server.go` (ListAssets).

- The static `ListRequestsBySpace` and `ListAssetsBySpaceWithDeleted`
  queries were deleted; `show_deleted` on assets now flows through
  `ListQuery.ShowDeleted` (asset_requests has no `delete_time`, so its
  flag is inert). The JOINED `ListAssetsBySpace` / `ListAssetsByOrg`
  stay — the dashboards synthesizer still uses them; ListAssets was
  repointed onto the PLAIN `assets` table.
- `AssetFilter.sizeBytes` is the first BIGINT sortable, which added a
  `filtering.TypeInt` cursor branch to `filter.DecodeCursor` (a Go string
  bound against a bigint column fails pgx encoding). Nullable columns the
  protos advertise for order_by (`dueTime`, `expireTime`) are registered
  filterable-only, per the compound-cursor NOT-NULL rule.

### D. Filter + order_by wiring, keyset-partitioned (leveled) resources

**ListConnectors is DONE — it is the worked pilot.** It is now a fully
dynamic AIP-160 filtered + AIP-132 sorted + **compound-cursor** keyset
list built on `internal/filter.BuildListQuery` (base scope `org_id` +
`space_id IS NOT DISTINCT FROM`, layered filter/order_by/keyset, all
values bound as `$N`). The step-by-step conversion recipe extracted from
it lives in **`docs/aip-list-transpiler-procedure.md`** — follow it
verbatim for the remaining two.

**ListWorkflows, ListSecrets** — org+space leveled, bespoke sqlc queries
(`workflows.sql:102`, `secrets.sql:46`), each `WHERE org_id = @org_id AND
space_id IS NOT DISTINCT FROM narg('space_id')`. Convert per the procedure
doc:
1. Declare a `ResourceFilter` in `filter/declarations.go` (whitelist +
   timestamp `Sortable.Type`), add a `Scan` helper in `filter/scan.go`.
2. Rewrite the handler to `PlanOrderBy` → `DecodeCursor` → `BuildListQuery`
   (base scope from context) → `ScanXxx` → `EncodeCursor(rows[pageSize-1])`.
   The compound cursor (`(sort_col, id)` row comparison) keeps the keyset
   stable under a custom `order_by` and duplicate sort keys.
3. Reject unknown filter/sort fields + tampered tokens with
   `apierr.InvalidArgument`.
4. Delete the dead static `ListXxxByParent` sqlc query + regenerate.

`ListWorkflowVersions` has **no** `filter`/`order_by` in the proto
(`workflow.proto:698` — only parent/page_size/page_token), so C-fix only
(pagination). If sort/filter is later wanted, add the proto fields first.

### E. Filter + order_by wiring, flat single-parent resources

These can migrate to `filter.Query` (add a `ResourceFilter` +
`Scan` helper) for consistency, or extend the bespoke query as in D.

- **ListStorageGateways** (org-only, `gateways.go`) — add a
  `StorageGatewayFilter` to `declarations.go` (Table `storage_gateways`,
  ParentColumn `org_id`, sortable `displayName`/`createTime`), a
  `ScanStorageGateways`, and swap the body to `filter.Query`. Pagination
  is already correct; keep `rows[pageSize-1]`.
- **ListEndpoints** (`endpoints.go:253`) and **ListAgents**
  (`agents.go:93`) — currently return the **entire** child set with no
  paging at all (`ListStorageEndpointsByGateway`,
  `ListStorageAgentsByGateway`). Add keyset pagination first (proto has
  page_size/page_token), then filter/order_by. Parent is the gateway id.
- **ListMembers (org/space)** (`organizations/members.go:105`,
  `spaces/members.go:117`) — offset-based today (opaque base10 offset
  token, `parseMembersPaging`). No off-by-one, but filter/order_by
  (`members.proto:139/143`) are unwired and actors are passed `nil`.
  Wiring filter/sort over the `ListOrgMembers`/`ListSpaceMembers` join is
  the work; offset paging can stay or convert to keyset.
- **ListRoles** (`iam/server.go:168`) — 4 system roles, no paging needed
  now, but note the **N+1**: `RolePermissionIDs` is called per role in
  the loop (`:184`). If custom roles land, batch it and add
  filter/order_by then.

### F. Caller-scoped / special lists

- **ListOrganizations** (`organizations/server.go:152`) — returns all of
  the caller's orgs via `ListOrganizationsForIdentity`; `filter` /
  `order_by` / `page_size` / `page_token` are all **ignored**
  (`_ = req`). The generic `OrganizationFilter` (`declarations.go:65`)
  can't be used directly because it has no membership predicate (it would
  return every org). To wire: filter/sort in-process over the returned
  membership set, or add a membership-scoped filtered query. Low urgency
  (caller org counts are small) but the ignored fields are a
  correctness/expectations gap — at minimum reject non-empty
  filter/order_by rather than silently dropping them.
- **ListOperations** (`operations/server.go:132`) — `filter` (AIP
  standard on operations) is **ignored**, and there is **no real
  pagination**: only `page_size` is honored, `next_page_token` is never
  set and no cursor is consumed (`ListAuthorizedOperations` takes only
  caller + page_size). Add a keyset cursor over the operations table and
  wire at least a `done`/state filter.
- **ListOrgs (mcp)** / **ListAccountOrganizations** / **ListPermissions**
  — deliberately single-page / catalog lists with documented AIP-158
  disables. No change; leave as-is.

### G. Rollup (`-` wildcard) — after B–F land

Meaningful for the leveled resources. Model exactly on
`ListWorkflowRuns`:
- **ListConnectors**, **ListSecrets**, **ListWorkflows** — support
  `organizations/{org}/spaces/-/{collection}`. Add a `…ByOrg` sqlc
  variant that drops the `space_id` predicate (`WHERE org_id = @org_id`,
  keyset on id) — mirror `workflows.sql:189` `ListWorkflowRunsByOrg`.
  Detect the wildcard with a `HasSuffix(parent, "/-")`-style check
  (`isWorkflowWildcard`, `workflow_runs.go:326`), and rebuild each row's
  true name with a batched space-slug lookup (`orgRunNames`,
  `workflow_runs.go:339`; `SpaceSlugsByIDs`) so a space-scoped row nests
  under its space and an org-direct row nests under the org. Permission
  is already gated at the org/space scope by the interceptor, so there is
  no single parent to re-check.
- **ListConversations** — a `users/-` rollup would let an admin holding
  `ai.conversations.readAll` list every user's conversations in the org.
  The `readAll` check already exists (`conversations.go:57`); the
  `ConversationFilter.UserColumn` predicate (`declarations.go:176`) would
  need to become optional for the wildcard path. Only build if a product
  need appears.

### H. Unimplemented — wire to the standard when built

`ListUsers`, `ListGroups`, `ListGroupMembers` (iam), `ListInvitations`
(organizations), `ListAssetVersions` (assets), `ListLineItems`
(requests) are declared in proto and registered in
`internal/server/permission_registry_gen.go` but have **no service
handler** — they return `codes.Unimplemented`. When implemented, follow
the reference: keyset pagination (`rows[pageSize-1]`), `filter.Query` (or
bespoke filter/sort), and `-` rollup where the resource is leveled. Note
`ListGroups`/`ListUsers` protos already carry `filter`/`order_by`
(`groups.proto:135/147`, `users.proto:114/127`); `ListGroupMembers`,
`ListInvitations`, `ListAssetVersions`, `ListLineItems` do not.

## Recommended order of work

1. **Pagination first — it's a data-correctness bug.**
   1a. Section C (ListRequests, ListAssets) — **DONE**: both rewritten to
   the compound-cursor keyset engine (see section C).
   1b. Section B — the 13 off-by-one one-liners. Mechanical; one commit
   per service package with a boundary-crossing integration test each.
2. **Filter + order_by** (sections D, E, F) — wire per resource, TDD a
   filter test + a sort test through `grpcharness`. Prefer `filter.Query`
   for flat resources, bespoke-query extension for leveled ones. Reject
   unsupported shapes with `apierr` rather than ignoring.
3. **Rollup** (section G) — last, only for the leveled resources where it
   adds real value (connectors/secrets/workflows via `spaces/-`).
4. **Unimplemented handlers** (section H) — as product needs dictate,
   built to the standard from line one.

Each phase boundary: run `make test`, then auto-spawn a code-reviewer
over the diff (auth/scope/data-integrity surface). Keep this doc's table
in sync as rows flip to ✓.
