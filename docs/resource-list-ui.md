# Resource list UI — reusable grid layering

Status: design / next-phase spec. Not yet built.

## Goal

A reusable list surface so we don't rewrite it per resource. It's used for
AIP resources today (connectors, secrets, workflows, versions, runs), but the
base component is domain-agnostic — nothing forces the rows to be AIP
resources. It must work unchanged in:

- **`start`** (TanStack Start): SSR + CSR, list state in URL search params.
- **`electron`**: CSR only, list state in local/component state, bearer-token
  client (no BFF, no SSR).

## Principle: dumb, controlled, injected

Every tier is presentational and fully controlled. It **fetches nothing, owns
no state, and knows no router or SSR.** Consumers inject `rows` + controlled
state + change handlers, and wire that state to their environment (URL search
params in start; local state in electron). This is what makes it portable
across start and electron, and SSR-safe (no data effects to break server
render — prefetch/hydration live in the route, outside the grid).

## Three tiers — each a coherent concept, each earns its keep

```
Grid<T>  →  ResourceGrid<T>  →  ScopedResourceGrid<T>
generic     AIP resource        org+space-scoped AIP resource
```

Each tier is a *concept*, not just a delta over the one below. Crucially,
`ScopedResourceGrid` extends **`ResourceGrid`**, not `Grid` directly — which is
what makes `ResourceGrid` a required tier regardless of how small its own
customization is. It carries real content anyway (standard columns, CRUD
orchestration).

### 1. `Grid<T>` — generic data grid

Home: `@pivox/ui` (e.g. `@pivox/ui/grid`) or `@pivox/primitives`. **No** domain
concepts — no `name` parsing, no scope, no AIP.

- **Config:** `columns: { key, header, sortable?, filter?, cell: (row: T) => node, className? }[]`, `rowKey: (row: T) => string`, `rowActions?: (row: T) => node`, `emptyLabel`/`loadingLabel`.
- **Data + controlled state:** `rows: T[]`, `isLoading`, `loadError`; `state: { filters: Record<string,string>, sort: {field,direction}|null, pageSize, pageToken }`; `onChange: (next, { history }) => void`; `pagination: { hasPrev, hasNext }` (opaque — cursor or offset, the grid can't tell).
- **Renders:** always-mounted header + column-aligned filter row + pagination; body reflects loading/error/empty/data; sortable headers.
- **Value:** rendering + controlled interaction. Reusable for anything.

### 2. `ResourceGrid<T>` — AIP-resource admin shell

Home: `@pivox/ui/resource-admin` (consumes `Grid`). Adds the conventions every
AIP resource repeats:

- **Standard column factories:** `nameColumn` (`displayName || name-leaf`), `timestampColumn(createTime|updateTime)` with actor.
- **`AdminFrame`** (title + New) + **create/edit/delete dialog orchestration** (open/close/pending/error, delete-confirm), with the resource's form as a **slot**.
- **Row actions:** edit / delete / new.
- **Pluggable edit/create action:** dialog (connectors, secrets) **or** navigate-to-editor (workflows, whose "edit" is the canvas, not a form). If this hard-coded "open a dialog" it would exclude editor-resources and under-deliver.
- **Value:** the CRUD-admin shell every resource reuses; and the semantic base `ScopedResourceGrid` extends.

### 3. `ScopedResourceGrid<T>` — org+space-scoped resource

Home: `@pivox/ui/resource-admin` (consumes `ResourceGrid`). Adds the org+space
dimension every scoped resource repeats:

- **Scope selector** control + **scope state** + **request-path switch** (org rollup vs a specific space).
- **Standard Space column** — at this tier it's universal, so the layer owns it (not consumer config).
- **Value:** the scope dimension for connectors, secrets, workflows, runs.

## Which tier per resource

| Data | Tier |
|---|---|
| Generic / custom, non-resource | `Grid<T>` |
| Flat CRUD AIP resource (tags, api-keys, storage-gateways) | `ResourceGrid` |
| org+space-scoped AIP resource (connectors, secrets, workflows, runs) | `ScopedResourceGrid` |

## Controlled-state contract

- `Grid` tier: `{ filters, sort, pageSize, pageToken }`.
- `ScopedResourceGrid` tier: adds `scope`.

`start` owns this in URL search params (`validateSearch`) and feeds it
controlled; electron owns it in local state. Neither the grid nor
`@pivox/features` import the router — see the connectors implementation
(`web/apps/start/src/routes/_app/connectors/`, `use-list-controls`).

Note: today's `ListControlsValue` bundles `scope`; the extraction splits it —
`scope` moves up to the `ScopedResourceGrid` tier.

## Forms

Create/edit forms are resource-specific and **not** part of any grid tier —
they're slotted into `ResourceGrid`'s dialog, or replaced by navigate for
editor-resources. Delete-confirm is standard (`ResourceGrid`).

## Extraction plan

1. Build `Grid<T>` by lifting connectors' list assembly **minus scope**
   (columns/filters/sort/pagination → generic config).
2. Convert **Secrets** onto it (backend transpiler + a `ResourceGrid` usage) —
   validates the config API against a 2nd resource before locking it.
3. Extract `ScopedResourceGrid` from connectors + secrets (both scoped); flat
   resources use `ResourceGrid` directly.

**Backend prerequisite:** sweep the AIP-160 filter transpiler + compound-cursor
keyset + org rollup to secrets/workflows/versions/runs — see
`docs/aip-list-transpiler-procedure.md` and `docs/aip-list-audit.md`. Sortable
columns must be `NOT NULL` (keyset-with-NULLs gap). Settle the org/space rollup
decision (option 1 clean `spaces/-` vs option 2 mixed-depth) so scoped
resources are consistent before converting versions/runs.

## Open decisions

- The pluggable edit-action shape (dialog vs navigate) in `ResourceGrid` — the
  one boundary to keep honest: `ResourceGrid` only earns its orchestration
  value if the same shell fits dialog-form resources (connectors, secrets)
  *and* editor-resources (workflows). If two of those don't fit, that tier
  collapses back into helpers — which is fine, the value test working.
- Rollup option 1 vs option 2 (parked).
