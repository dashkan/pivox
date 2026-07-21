# Resource-admin DRY — design

**Status:** design → building now.

**Purpose.** Every admin resource today hand-writes a near-identical feature
(`use-X.ts`, `X-feature.tsx`, `use-X-form.ts`, `build-X-request.ts`, form fields,
save logic) plus a per-app route. We're going to build many more admin pages.
Replace the copy-paste with a **descriptor-driven** abstraction: a new admin
resource = one descriptor + a thin per-app route. Shared across `apps/start`
(SSR/TanStack) **and** `apps/electron`.

---

## 1. Layering (what's shared vs per-app)

- **`@pivox/features`** (router- and react-query-agnostic, DI'd `$api`/`apiClient`)
  — the descriptors, generic hooks (`useResourceList`, `useResourceForm`),
  request builders, validators.
- **`@pivox/ui`** (React, router-agnostic, nav injected as callbacks) — `Grid`,
  `FormPage`, and the `ResourceList` / `ResourceForm` composites.
- **`apps/start` / `apps/electron`** — thin route shells only: read URL/nav
  state, inject `navigate` / `onCreate` / `onEdit`, and (start only) do the SSR
  prefetch + queryClient prime. **This is the only per-app code.**

Electron sharing works because none of the shared layers import a router or SSR.
Each app injects its own navigation; `start` additionally does SSR (electron has
none). The descriptor's `queryKey`/`buildListRequest` are shared, so `start`'s
prefetch primes the byte-identical key the shared hook reads.

---

## 2. Model — List always shared; Create/Edit **form by default, custom override**

- **List:** the shared `Grid`, always — every resource, including workflows.
- **Create/Edit:** **form-based by default.** Provide a form spec and you get
  `FormPage` create/edit + wired routes with zero extra work (connectors, secrets,
  and everything else).
- **Override** create/edit with a **custom view** when a resource needs it —
  workflows supplies its React Flow canvas instead of a form spec, replacing the
  default form flow. It keeps the shared List.
- The `Grid`'s row action is **injected**; it defaults to the form edit route and
  an override just repoints it (workflows → `/workflows/$id`). No per-resource
  wiring for the common case.

---

## 2b. Affordances via composition (not flags)

The descriptor holds **data**; UI **affordances are composed** — presence-of-child is
the config, so there are **no `newLabel?`/`deleteConfirm?` flags** and no "List
presumes create/delete" coupling.

- **Data (descriptor):** `useList`, **columns-as-data**, `rowId`, the `remove`
  mutation. (columns-as-data is the skill's sanctioned carve-out.)
- **Row-level affordances (Edit/Delete) → a column.** A predefined
  `actionsColumn(ctx, opts)` factory renders Edit + Delete using `ctx.onEdit(row)` /
  `ctx.openRemove(row)` — the column render fn already gets the row, so no new Grid
  machinery. A resource adds it (default edit+delete), customizes it (edit-only), or
  omits it (workflows). The **delete-confirm copy is a param** to the delete opt, so
  it exists only when the resource has delete. The confirm dialog + `remove` mutation
  stay a composite/descriptor concern; the button just calls `openRemove(row)`.
- **Toolbar-level affordances (New, filter, scope) → composed children:**
  `<ResourceList.Toolbar><ResourceList.NewButton/>…</ResourceList.Toolbar>`.
- **`ResourceList.Default` preset** composes the common toolbar-New + the edit+delete
  actions column, so the 90% case stays one line; resources compose explicitly only
  to deviate.

Both couplings dissolve: no `<NewButton/>` → no create; no actions-column-delete → no
delete. Workflows omits both today (nav on the Name cell) and **grows** edit/delete
later by composing them in (edit → the canvas via `onEdit`; delete → the already-wired
`removeWorkflow`) — zero abstraction change. That forward-flexibility is the point,
and it's why this beats the optional-flags patch.

## 3. Descriptor shapes (illustrative — refine in code)

```ts
// @pivox/features — router/query-agnostic
interface ListDescriptor<Row> {
  key: string;                                   // resource id → query keys + routes
  columns: ColumnDef<Row>[];                     // columns-as-data (Grid)
  scope: ScopeConfig;                            // org / space / rollup
  buildListRequest(scope, state: ListState): ListRequest;   // filter/sort/page → openapi
  queryKey(scope, state: ListState): QueryKey;   // react-query + SSR-prime parity
  facetable?: string[];                          // later: terms-agg fields (§ aggs)
  rowId(row: Row): string;
}

interface FormDescriptor<Row, Values> {
  fields: FieldDef[];                            // fields-as-data (FormPage)
  fetchRecord(scope, id): Promise<Row>;          // detail query for edit
  toValues(row: Row): Values;                    // record → form values (+ seed for create)
  buildCreate(scope, v: Values): CreateRequest;
  buildUpdate(scope, id, v: Values): UpdateRequest;
  validate(v: Values): Errors;
  remove(scope, id): DeleteRequest;
}

interface ResourceAdmin<Row, Values> {
  list: ListDescriptor<Row>;
  form?: FormDescriptor<Row, Values>;            // default create/edit
  createView?: ComponentType<ResourceViewProps>; // override: replaces form create
  editView?:   ComponentType<ResourceViewProps>; // override: replaces form edit
}
```

## 4. Generic hooks + composites

```ts
// @pivox/features
useResourceList(list, { parent, state, $api, apiClient })
  → { rows, totalCount, facets?, isLoading, error }
useResourceForm(form, { parent, id?, $api, apiClient })
  → { values, pending, error, submit, remove }

// @pivox/ui — compound, DI'd nav, columns/fields/facets as data (composition skills)
<ResourceList descriptor={list} state onStateChange onCreate onEdit />   // → Grid
<ResourceForm descriptor={form} onDone back onDirtyChange />             // → FormPage
```

## 5. Per-app route shells

- **start:** TanStack file route + `createServerFn` SSR prefetch → `setQueryData`
  under `descriptor.queryKey(...)` + inject `useNavigate`-based `onCreate`/`onEdit`.
- **electron:** electron-router route + inject its navigate; **no SSR**. Same
  `ResourceList`/`ResourceForm`/custom-view components.

---

## 6. Migration order

1. Build the abstraction (types + hooks + composites).
2. **Connectors** → List + Form. Proof: full CRUD, byte-identical behavior + SSR
   prime + return-to nav preserved.
3. **Secrets** → List + Form (incl. the org-rollup + write-only value).
4. **Workflows** → **List only**; custom canvas create/edit/view untouched, row
   action → `/workflows/$id`. (Swaps its bespoke `<Table>` for the Grid → gains
   filter/sort/pagination + future terms-facets.)
5. **Electron** — wire one resource (or a smoke mount) in `apps/electron` to prove
   the shared components run without SSR/TanStack. **Load-bearing assumption;
   validate early** (features are already router-agnostic post FEAT-ROUTER-1, so
   it should hold — confirm live).

---

## 7. Constraints

- **`vercel-composition-patterns`** — compound components, DI context, **no
  boolean-mode props**; columns/fields/facets-as-data is the children-over-render
  carve-out.
- `@pivox/features` stays router/react-query-agnostic (injected `$api`).
- Keep `@pivox/primitives` stock; `@/` alias in apps; **TDD**.

## 8. Forward-compat with aggs

Terms facets + `total_count` (the List-tier agg work) layer onto this additively:
`ListDescriptor.facetable` names the fields, `useResourceList` returns `facets`,
`ResourceList` renders the facet sidebar from the generic `map<string,FacetResult>`
(see `search-faceting-design.md`). Nothing here blocks it; it's the next step
once the abstraction lands.
