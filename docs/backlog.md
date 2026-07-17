# Engineering backlog / tech-debt tracker

Status: **LIVE** (opened 2026-07). This is the durable home for
**cross-cutting** follow-up items that would otherwise live only in a
session's scratch notes and evaporate on the next context compaction.
It is a backlog, not a design doc — one line per item, an anchor
(file/path/function or task-id), and why it's deferred or what decision
is open. Keep it skimmable.

## Why this exists

Several sweeps (the keyset/List migration, the workflow UI build, the
Firebase→Keycloak auth cut-over) each surfaced follow-ups that outlive
the change that found them. When a session compacts, those items are
lost unless they're written down somewhere discoverable. This file is
that somewhere. Add to it when you finish a change and leave a loose
end; check items off (or delete resolved ones) as they land.

## Status markers

- **PENDING** — agreed work, not started.
- **IN PROGRESS** — an agent/PR is actively on it now.
- **RESOLVED (this session)** — landed; kept for a short while as a
  breadcrumb + lesson, then prunable.
- **DECISION NEEDED** — blocked on a human call; the decision is stated
  inline.

## Scope

This file owns everything **except** the Workflow UI backlog, which has
its own home (see [§9](#9-workflows-pointer-only)). Don't duplicate
workflow items here. The AIP List-RPC audit also has its own detailed
home (`docs/aip-list-audit.md`); §2 below points at it rather than
restating the per-RPC table.

---

## 1. Backend — List / keyset / pagination

The definitive per-RPC state lives in **`docs/aip-list-audit.md`**
(37 List RPCs, pagination/filter/order_by status each) and the
conversion recipe in **`docs/aip-list-transpiler-procedure.md`**. This
section only tracks the cross-cutting items not pinned to a single RPC
row.

- **CLEANUP-1 — pagination off-by-one, codebase-wide.** `PENDING`.
  Next-page token = the first *un-returned* row (`results[pageSize].ID`)
  while the resume predicate is strict `id > cursor` → one row silently
  skipped at every page boundary. Affects **~11 handlers** (was ~13;
  `ListConnectors` migrated to a compound cursor, so its old "Section B"
  entry is stale). Fixed **only** in the BE-1 run-list code
  (`renderRunPage`, `internal/service/workflows/workflow_runs.go`).
  Needs a codebase-wide sweep **plus a shared full-coverage pagination
  test helper** (existing tests only assert `page2[0] != page1[0]`,
  which never crosses the boundary that exposes the bug). See the
  per-handler fix table in `docs/aip-list-audit.md` §B.
- **`ListRequests` / `ListAssets` — genuinely broken pagination.**
  `RESOLVED (this session)`. These were worse than off-by-one:
  `LIMIT pageSize+1 OFFSET 0` with offset hardcoded 0 and the emitted
  token never consumed → every "next page" re-returned page 1. Both
  rewritten to the compound-cursor keyset engine (`requests/server.go`,
  `assets/server.go`; `filter.RequestFilter` / `filter.AssetFilter`).
  See `docs/aip-list-audit.md` §C.
  - **Open decision (DECISION NEEDED, now moot for these two):** whether
    the two broken handlers got the sqlc keyset rewrite in the same pass
    as the CLEANUP-1 off-by-ones or separately. Landed separately (the
    leaning at the time). Recorded so the CLEANUP-1 sweep doesn't
    re-touch them.
- **KEYSET-SWEEP — list handlers on the shared engine.** `RESOLVED
  (this session)`. Migrated onto `filter.BuildListQuery` + compound
  `(col,id)` row-value cursor (`filter.PlanOrderBy` / `EncodeCursor` /
  `DecodeCursor`, `internal/filter/`).
- **`filter` / `order_by` not wired server-side.** `PENDING` (partial).
  Many list queries still ignore AIP-160 `filter` + AIP-132 `order_by`
  in SQL ("not yet wired"). Architecturally resolved toward the shared
  `filter.BuildListQuery` engine; remaining per-RPC wiring tracked in
  `docs/aip-list-audit.md` (§D–F): ~11 filter-unwired, ~11
  order_by-unwired.
- **LOW-1 — sortable columns must be NOT NULL.** `PENDING` (guard-note).
  The keyset path silently skips/dupes on nullable sort columns. Add a
  guard note to the transpiler procedure
  (`docs/aip-list-transpiler-procedure.md`). `dueTime` / `expireTime`
  (nullable) are demoted to filterable-only for this reason.
- **Old id-only cursor paths.** `PENDING`. Migrate the remaining id-only
  cursor handlers to compound cursors, **or** reject a non-`id`
  `order_by` when the cursor column is id-only (don't silently return a
  wrong page).
- **aichat → shared list func.** `PENDING`. Put the aichat lists on the
  shared func (`DefaultOrder` newest-first) for free filter+pagination.
  The inert legacy config (`OrderBy` / `CursorColumn` / …) on the four
  aichat filters can't be dropped until mcp/spaces leaves `filter.Query`.
  Tracked as issue **#42**.
- **`filter.Query` legacy engine deletion.** `PENDING`. Fires after
  aichat (#42) **and** MCP-STATIC land. (Note: `docs/aip-list-audit.md`
  header already declares the legacy `query.go` path retired for the
  handlers that moved — confirm the last consumers before deleting.)
- **`creator` filter omitted.** `PENDING`. Maps to a raw `created_by`
  UUID; a proper `creator` filter needs actor-resolution wiring. Deferred.
- **Deferred pure nits.** `PENDING`. Parallelize the loader prefetch;
  stable KVE key; dedup a shared `DecodePageToken`
  (`internal/filter/token.go`).
- **Transpiler wildcard-path `\` escaping.** `RESOLVED (this session)`.
  The `*`-wildcard operand path now escapes `\` and emits `ESCAPE '\'`
  while preserving `*`→`%` (`internal/filter/transpiler.go`). (Distinct
  from the `contains` / bare-literal paths below, which remain open.)
- **Transpiler `contains` / bare-literal ILIKE paths escape nothing.**
  `DECISION NEEDED`. `transpileHas` (`%value%`), `expandBareLiteral`, and
  `transpileHasSelect` in `internal/filter/transpiler.go` build ILIKE
  patterns with **no** LIKE-metacharacter escaping and no explicit
  `ESCAPE`, so a literal `%` / `_` / `\` in a `:` (has/contains) or
  bare-literal filter value is treated as a wildcard/escape. Same bug
  class as the wildcard `*` path (now fixed above); different intended
  semantics, so it needs a decision before fixing.

## 2. MCP

- **`name_prefix` literal-prefix handling.** `RESOLVED (this session)`.
  Now escapes `\ % _` and uses explicit `ESCAPE '\'`
  (`internal/service/mcp/spaces.go` + `internal/db/queries/spaces.sql`),
  so `*` / `\` / `%` / `_` all match literally. TDD unit + grpcharness
  e2e.
- **MCP-STATIC — mcp/spaces on a static query.** `RESOLVED (this
  session)`. `mcp/spaces` was deliberately converted to a static query,
  kept off the dynamic filter engine on purpose (documented so nobody
  "fixes" it back onto the engine).
- **`pivoxApiV1*` schema-rename MCP collision.** `PENDING` (accepted
  as-is). Accepted for now; **file an issue to track** so it isn't lost.

## 3. Auth / BFF / migration

- **BFF idle sign-out.** `RESOLVED (this session)`. Keycloak
  `ssoSessionIdleTimeout=1800` was expiring the online session; fix was
  `offline_access`. Merged to `main`.
- **Next.js / SSR migration.** `PENDING` (architectural, deferred).
  Technical write-up (no recommendation) at
  `docs/tanstack-start-nextjs-migration.md`, incl. an Auth.js section.
  Open questions: JWT vs DB session; refresh concurrency. Real SSR is an
  architectural change deferred pending a look at server-side data + auth
  wiring.
- **TanStack Router parallel routes.** `PENDING` (blocked upstream). Not
  in any released version (GitHub discussion **#605**); needed for
  Next-style modal-permalink slots. Open-ended — gates the "true modal
  slots" endpoint of the permalink work in §8.

## 4. Tags

- **TAG-RPC-DEAD / tag-IDOR.** `RESOLVED (this session)`.
  `CreateTagValue` / `ListTagValues` + tag-binding create were
  **unreachable** through the real auth interceptor chain (resource-name
  vs scope-extractor mismatch) — effectively dead in prod. Also closed an
  IDOR via an org-scoped query. Landed this session.
  - **Lesson to watch (class of bug):** a naming/scope-extractor mismatch
    silently produces a *dead endpoint* that still compiles and passes
    unit tests. Any test that bypasses the interceptor chain won't catch
    it — this is exactly why service-layer tests must go through
    `grpcharness`, not a hand-built server.

## 5. Frontend / build / cleanup

- **`@pivox/ui` `./auth` broken export.** `PENDING` (repeatedly flagged
  out-of-scope). `web/packages/ui/package.json` `./auth` points at
  `dist/esm/shared/auth-provider.js`, but `src/shared/auth-provider.ts`
  isn't a vite build `entry`, so only the `.d.ts` emits and the `.js`
  never builds → `publint --strict` fails. Fix: add the source to the ui
  vite `entry[]`, or add an `auth.ts` barrel. Pre-existing on HEAD.
- **Orphaned resource-admin dialog exports.** `PENDING`. Moving secrets
  from the dialog-based UI to routed Grid + FormPage left `FormDialog` /
  `FormActions` (`web/packages/ui/src/resource-admin/form-dialog.tsx`)
  and the `DialogState` / `DialogMode` types with zero remaining
  consumers, still exported from the barrel. Remove them — and pair the
  removal with a doc note (per the cleanup-with-prevention rule) so the
  dead exports don't silently reappear.
- **Delete-button red emphasis.** `DECISION NEEDED`. Reverted to stock
  shadcn `destructive` (tint) this session; user wants a stronger red
  later. NOTE: the earlier *blue* delete-button bug's real cause is
  still **UNKNOWN** — the "class not emitted" theory was disproven
  (`@pivox/ui` **is** Tailwind-scanned and `--color-destructive` **is**
  registered). Don't re-assert the disproven theory when this is picked up.
- **SEED-BUG — psql meta-command in a pgx-run seed.** `RESOLVED (this
  session)`. `scripts/seeds/10_storage_gateways.sql` used a psql-only
  `\set` meta-command the pgx seed-runner can't parse
  (`SQLSTATE 42601`), breaking `TestSeed_AssetVersionsMatchAssetContentType`.
  - **Lesson:** seed files run through **pgx**, not psql — no `\set`, no
    backslash meta-commands.
- **`ListStorageGateways` implemented.** `RESOLVED (this session)`.
  Scoped keyset list added so the connector agent dropdown can enumerate
  agents (`ListAgents` was already implemented).
- **FEAT-ROUTER-1 doc drift.** `PENDING` (doc-only). `@pivox/features`
  router decoupling shipped, but `docs/workflow-ui.md` §5/§6/§8/§10 still
  read "pending" — quick accuracy pass to mark done.
- **Decision-comment house-style sweep.** `PENDING` (owner-confirmed).
  Verbose "why we chose X" prose in ~50% of files (e.g.
  `web/apps/start/src/lib/api-client.ts`,
  `web/apps/start/src/server/prefetch.ts`). Sweep planned. (Source notes
  cited these under `apps/start/…`; verified path prefix is
  `web/apps/start/…`.)
- **`chat.tsx:256` `forwardRef` under React 19.** `PENDING`. Lone
  holdout (`web/packages/ui/src/chat/chat.tsx:256`); ref is a prop under
  React 19. shadcn primitives are already clean.
- **Dual React in the tree.** `PENDING`. `react@19.2.7` (apps) +
  transitive `react@18.3.1` via `react-jsx-parser` ← `@pivox/primitives`.
  A past "two-React null dispatcher" crash is on record. Resolve to a
  single React when `react-jsx-parser` is replaced/updated. Also why View
  Transitions are deferred (`<ViewTransition>` is `react@canary`-only,
  not in the pinned 19.2.7). Treat any hook-dispatcher crash as a
  resolution problem, not app logic.

## 6. Grid / FormPage / resource-admin

- **Roll Grid + FormPage onto secrets.** `IN PROGRESS`. A dedicated
  agent is doing this now, coupled with the permalink generalization
  (§8).
- **`ScopedGrid` / `ScopedResourceGrid` extraction.** `PENDING`
  (deferred until warranted). Thin wrapper adding a scope selector +
  scope↔path mapping; don't extract speculatively.
- **Backfill render tests for `resource-admin` components.** `PENDING`.
  Currently validated via types + builds + pure-logic only; add
  `@testing-library/react` render tests (jsdom is already the repo
  standard).
- **Scoped form tier — dropped.** `RESOLVED (this session)` (decision).
  Settled on **2 tiers, not 3** — no scoped form tier. Recorded so it
  isn't re-proposed.

## 7. Pagination (UI)

- **`NumberedPagination`.** `PENDING`. The cursor pager was renamed
  `CursorPagination` (`Grid.CursorPagination`) so a numbered
  (skip/take/pageNum) sibling can land later. Build it when that UX
  actually arrives — don't pre-build.
- **Prev-pagination limitation.** `PENDING` (accepted for now). A single
  URL cursor means `prevPage()` jumps to the first page at 3+ pages (no
  back-stack).
- **Nav `replace` vs `push`.** `DECISION NEEDED` (user's call). Currently
  `replace` for typing, `push` for scope/page/sort. Left as the user's
  decision.

## 8. Routed-pages permalinks

- **Generalize `?from=` return-to.** `IN PROGRESS` (same agent as
  secrets in §6). Generalize the connectors `?from=` return-to across
  routed pages. Interim is search-param based (`?edit=<slug>` / `?new`);
  **true modal slots are blocked** on TanStack Router parallel routes
  (discussion #605, see §3).

## 9. Workflows (pointer only)

The Workflow UI backlog is **fully owned by `docs/workflow-ui.md`** — do
not duplicate it here. Spot-checked 2026-07: the four items that might
have looked "uncaptured" are all already in that doc. Pointers:

- **§7 / §8** — Phase 2 (run monitor + history + trigger/cancel) and the
  Phase-2/3 task breakdown (T7–T13).
- **§12 — Risks & open questions**, including:
  - MANAGED-workflow `config` vs `parameters` — config editor priority
    between Phase 2 and 3 ("Confirm priority"). *(In doc — do not restate.)*
  - CEL authoring depth — autocomplete over run context is a follow-up.
    *(In doc.)*
  - Run polling vs streaming; trigger config UI; secret-rotation mask
    (verify live); View Transitions deferred.
- **§13 — Task breakdown for subagents** (T1–T13 + BE-1).
- **§14 — Convention-violation ledger**, including:
  - `workflow_runs.org_id/space_id` missing DB `CHECK`/composite-FK
    scope-integrity constraint (`000001_init.up.sql`). *(In doc — do not
    restate.)*
  - HTTP-connector **reachability validation on save** — see §6.3 (line
    ~406), documented-not-built. *(In doc.)*
  - The keyset off-by-one ledger row (mirrors §1 CLEANUP-1 here).

If a genuinely new workflow item appears that isn't in `workflow-ui.md`,
add it **there**, not here.

## 10. Process / housekeeping

- **Prune stale agent worktrees.** `PENDING`. Source notes flagged ~20
  merged worktrees cluttering `git worktree list`; the tree currently
  lists **3** (already largely pruned) — verify and clean the remainder.
- **Process lesson — worktree resume lands in main.** `PENDING`
  (guardrail). Resuming a worktree-isolated agent can drop it back into
  the **main** repo. Always verify you're on the intended branch/worktree
  after any such resume; prefer spawning a fresh worktree over resuming.
