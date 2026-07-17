# Resource admin UI — reusable grid + form layering

Status: design / next-phase spec. Not yet built.

The resource-admin surface has two halves that share one philosophy — dumb,
controlled, router-free, dependency-injected via a `{ state, actions, meta }`
context read with `use()`: the **list** (`Grid`, below) and the **create/edit
form** (`FormPage`,
[§ Resource form UI](#resource-form-ui--reusable-formpage-layering)). This doc
owns both. Read the Grid section first — the FormPage section is the
form-side mirror of it and refers back to its rule tags.

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

Create/edit forms are resource-specific and **not** part of any grid tier. Per
task #32 (ROUTED-PAGES) they are no longer slotted into a `ResourceGrid`
dialog — they are full routed **pages** (`FormPage`,
[§ Resource form UI](#resource-form-ui--reusable-formpage-layering)). The grid's
New / Edit / row actions therefore all resolve to a **navigate** to the form
route; the "open a dialog" path is retired. This collapses `ResourceGrid`'s
pluggable edit-action (dialog *or* navigate) to a single uniform navigate — see
the resolved open decision below. Delete-confirm still lives on both surfaces
(the grid for list-row delete, the FormPage for edit-page delete).

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

- ~~The pluggable edit-action shape (dialog vs navigate) in `ResourceGrid`.~~
  **Resolved by task #32 (ROUTED-PAGES):** create/edit are routed `FormPage`
  pages, so the grid's edit action is *always* a navigate. `ResourceGrid` no
  longer orchestrates a form dialog — it orchestrates only the list, row
  actions, and the list-row delete-confirm. This also removes the
  editor-resource asymmetry (workflows' canvas was already navigate); every
  resource now edits via navigate.
- Rollup option 1 vs option 2 (parked).

---

# Resource form UI — reusable FormPage layering

Status: design / next-phase spec. Not yet built. This is the form-side mirror
of the `Grid` section above and is designed to the same two skills
(`vercel-composition-patterns`, `vercel-react-best-practices`); each decision is
tagged with the rule it satisfies, exactly as the Grid was.

## Goal

Task #32 makes create/edit **routed pages**, not dialogs. That kills the
modal-combobox bug class (portal/pointer-lock hacks — see the `popupMountRef`
and `dialogBoundary` gymnastics in today's `ConnectorForm`), gives forms real
estate and a sane mobile layout, and gives every resource one uniform,
navigate-based edit action. `FormPage` is the reusable shell for that page so we
don't rewrite the frame + action bar + submit/load orchestration per resource.

Like `Grid`, it must work unchanged in:

- **`start`** (TanStack Start): SSR-prefetched edit record + CSR, return target
  in a URL search param.
- **`electron`**: CSR only, bearer-token client, its own router/history; no BFF,
  no SSR.

## Principle: dumb, controlled, router-free

`FormPage` **fetches nothing, owns no form values, runs no mutation, and imports
no router.** It renders a page frame + action bar and calls injected handlers.
The route (start) or the shell (electron) owns: loading the edit record,
running the create/update mutation, and navigating on cancel / success. This is
the same `state-decouple-implementation` discipline the Grid uses — the provider
is the only place that knows how state is produced; every part reads the
`{ state, actions, meta }` interface (`state-context-interface`) via `use()`
(`react19-use`).

The form **values**, `canSubmit` validity, and the create-vs-update mutation
are **resource-specific** and live in the resource's own layer — they never
enter the generic `FormPage` contract, exactly as domain rows never enter
`Grid`'s. `FormPage` is a thin shell + orchestration contract, **not** an
abstraction over form fields: no field schema, no generic field renderer, no
form-state library baked in. Three similar hand-written field-sets beat a
premature form engine.

## Compound parts (parallel to Grid's Provider/Toolbar/Table/CursorPagination)

A compound component (`architecture-compound-components`); consumers compose
exactly the parts they want, no boolean toggles
(`architecture-avoid-boolean-props`). All parts read one DI context via `use()`;
refs are passed as plain props (`react19-ref-as-prop`) — no `forwardRef`.

| Part | Role |
|---|---|
| `FormPage.Provider` | DI seam. The one place mapping resource state/actions → `FormPageContextValue<T>`. |
| `FormPage.Frame` | The `<form onSubmit>` + page layout. Native form semantics give Enter-to-submit for free; `onSubmit` calls `actions.submit()`. Mirrors `Composer.Frame` in the composition skill. |
| `FormPage.Header` | Title + Back link. Back is a composed child (a link the route supplies), not a router import. |
| `FormPage.Body` | Slot for the resource form fields — **children**, not a render prop (see tension note). |
| `FormPage.Actions` | The action bar. Renders `state.error` inline; composes the buttons below. |
| `FormPage.Cancel` | Reads `actions.cancel`; when `state.dirty`, routes through the unsaved-changes confirm first. |
| `FormPage.Submit` | Reads `state.canSubmit` + `state.pending`; label is its **children** (`<FormPage.Submit>Create connector</FormPage.Submit>`). Absorbs today's `FormActions` submit button. |
| `FormPage.Delete` | Reads `actions.delete`; opens the delete-confirm. **Composed only by the edit variant** — its presence *is* the "delete on edit" affordance. There is no `showDelete` flag (`architecture-avoid-boolean-props`). |

`FormPage.Frame` is an addition beyond the four parts the brief named
(Provider/Header/Body/Actions); it earns its place by owning the native
`<form>` element so Enter-to-submit and `type="submit"` work without a manual
keydown handler. Header/Body/Actions render inside it.

### Delete-on-edit and Cancel by composition (worked example)

The create and edit pages are **explicit variants** (`patterns-explicit-variants`),
not one component switched by a `mode` prop. Each composes the parts it needs —
the delete button simply isn't in the create tree:

```tsx
// Create page — no Delete part composed.
<FormPage.Frame>
  <FormPage.Header>New connector</FormPage.Header>
  <FormPage.Body><ConnectorCreateFields /></FormPage.Body>
  <FormPage.Actions>
    <FormPage.Cancel>Cancel</FormPage.Cancel>
    <FormPage.Submit>Create connector</FormPage.Submit>
  </FormPage.Actions>
</FormPage.Frame>

// Edit page — same parts + Delete, no boolean toggled anything.
<FormPage.Frame>
  <FormPage.Header>Edit connector</FormPage.Header>
  <FormPage.Body><ConnectorEditFields /></FormPage.Body>
  <FormPage.Actions>
    <FormPage.Delete>Delete connector</FormPage.Delete>
    <FormPage.Cancel>Cancel</FormPage.Cancel>
    <FormPage.Submit>Save changes</FormPage.Submit>
  </FormPage.Actions>
</FormPage.Frame>
```

This is the composition skill's `ChannelComposer` / `EditComposer` pattern
applied verbatim: variants differ by what they render, sharing internals via
context, not by a monolithic parent taking `isEdit` / `showDelete` booleans.

## DI context contract — `{ state, actions, meta }`

```ts
type FormMode = 'create' | 'edit';

/** Read half. Fully presentational; no async, no router, no form-values. */
interface FormPageState<T> {
  /** Data, not a component prop. Picks default labels / whether an edit record loads. */
  mode: FormMode;
  /** A create/update write is in flight. Gates Cancel + drives the "Saving…" label. */
  pending: boolean;
  /** A failed-submit message to surface inline in Actions, or null. */
  error: string | null;
  /** The resource form is valid and may be submitted. Derived in the provider (see 5.1). */
  canSubmit: boolean;
  /** The form has unsaved edits. Derived in the provider; drives the navigate-away guard. */
  dirty: boolean;
  /**
   * Edit-mode record load. In create mode all three are inert
   * (record null, recordLoading false, loadError null). Injected by the route:
   * SSR-prefetched (start) or client-fetched (electron).
   */
  record: T | null;
  recordLoading: boolean;
  loadError: string | null;
}

/** Write half. Every FormPage mutation flows through here; all injected. */
interface FormPageActions {
  /** Commit the form. Takes no args — the provider closed over the resource values. */
  submit: () => void;
  /** Abandon; navigate to the launching route (sanitized `from`, else the list). */
  cancel: () => void;
  /**
   * Edit-only delete. `undefined` in create — and because create's variant never
   * composes `FormPage.Delete`, the button is absent, not merely disabled.
   */
  delete?: () => void;
}

/** Metadata the parts can't derive from state alone. */
interface FormPageMeta {
  /** Human resource label ("connector") for titles + confirm copy. */
  resourceLabel: string;
  /**
   * Optional dirty-signal sink so a router-specific navigation blocker (start's
   * `useBlocker`, electron's own) can live in the route while FormPage stays
   * router-free. See the dirty-guard section.
   */
  onDirtyChange?: (dirty: boolean) => void;
}

interface FormPageContextValue<T> {
  state: FormPageState<T>;
  actions: FormPageActions;
  meta: FormPageMeta;
}
```

Held `unknown`-rowed in one module-level context; a `useFormPage<T>()` narrows
at the call site and throws outside a `<FormPage.Provider>` — identical DI
boundary to `useGrid<T>()`.

### Where the form values live (the load-bearing decision)

The generic `FormPage` state above carries **no** `values` field. Form values,
`patch`, and `canSubmit` are resource-specific and live in a **second,
resource-owned context** — precisely how connectors already run *two* contexts
today (the generic `Grid` context **and** `ConnectorsAdminContext`). Layering:

- `ConnectorFormProvider` owns `useState<ConnectorFormValues>` (lazy-seeded from
  `state.record`), exposes `{ values, patch }` on its own context, **derives**
  `canSubmit` and `dirty` during render (`5.1 derive-during-render` — never an
  effect, never mirrored state), and builds `submit: () => mutate(values)`.
- It feeds `canSubmit` / `dirty` / `pending` / `error` / `submit` / `cancel` /
  `delete` into `FormPage.Provider`.
- The field components (`ConnectorCreateFields`, `ConnectorEditFields`) read the
  **resource** context for `values` / `patch`. `FormPage.Submit` reads the
  **generic** context for `canSubmit`. Neither knows the other's shape.

`actions.submit()` taking no args (vs today's `submit(values)`) is what keeps
the generic contract free of resource types: the button in `FormPage.Submit` is
a plain `<button type="submit">`; the provider that built `submit` already holds
the values.

## Tension with the skills — surfaced, not buried

1. **Body is children, not a render prop — and needs *no* carve-out.** Grid's
   `Grid.Table` had to accept `columns` as *data* (the sanctioned
   `patterns-children-over-render-props` carve-out for per-row `cell(row)`)
   because rows are dynamic per-item data. `FormPage.Body` has no per-item data:
   the fields are static composition that read the resource form context
   directly. So `FormPage.Body` takes **children** with zero render functions —
   strictly cleaner than Grid on this axis. We call this out because it's the
   obvious place a reader would expect a `renderForm(values)` prop; we
   deliberately don't have one.

2. **`mode` in state vs. no `mode` component prop.** `FormPage.State.mode`
   exists as injected *data* (like `Grid.State.isLoading`) so shared copy can
   read "create" vs "edit". It is **not** a component boolean/union prop, and
   nothing forks its render tree on it. Create/edit divergence
   (identifier-field on create only, rotate-checkbox on edit only, scope
   editable on create / read-only on edit) is handled by **explicit variant
   field-sets** (`ConnectorCreateFields` / `ConnectorEditFields`), not
   `{isCreate ? … : …}` ladders. This is a real change from today's
   `ConnectorForm`, which branches on `isCreate` inline — that inline branching
   is exactly the `architecture-avoid-boolean-props` smell the migration fixes.
   Shared fields (display name, headers/annotations editor) extract to a
   `ConnectorCommonFields` both variants compose.

3. **Scoped tier is thin — flagged, not inflated.** See the tiering section.

## Tiering (explicit variants, no `mode` union) — parallel to Grid

```
FormPage<T>  →  ResourceFormPage<T>  →  (ScopedResourceFormPage<T>?)
generic shell   AIP create/edit         scope-on-create
```

Each tier is an *explicit component set*, not a delta toggled by a prop
(`patterns-explicit-variants`).

### 1. `FormPage<T>` — generic form shell

Home: `@pivox/ui/form-page`. No domain concepts. The compound parts + DI context
above. Value: page frame, action bar, submit/pending/error wiring, edit-load
states, delete-confirm plumbing, dirty-guard signal. Reusable for any page-level
form (not only AIP resources).

### 2. `ResourceFormPage<T>` — AIP create/edit shell

Home: `@pivox/ui/resource-admin` (consumes `FormPage`). Adds the conventions
every AIP resource repeats, as **two explicit variant components**:

- `ResourceFormPage.Create` — standard "Create {label}" / "Cancel" labels,
  composes Cancel + Submit, wires `submit → onSubmitSuccess (navigate back)`.
- `ResourceFormPage.Edit` — "Save changes" / "Cancel" + `Delete {label}`, the
  record-load states (`recordLoading` spinner, `loadError` notice), and the
  standard delete-confirm (the existing `DeleteDialog`, unchanged — it's a
  confirm, not a form).

Value: the CRUD-page shell every resource reuses; the semantic base a scoped
tier would extend. This tier is required for the same reason `ResourceGrid` is:
it carries the standard labels + delete-confirm + load-state conventions
regardless of how small each is.

### 3. `ScopedResourceFormPage<T>` — scope-on-create (candidate tier)

**Honest assessment: this tier barely earns its keep, unlike `ScopedResourceGrid`.**
For the list, scope is a live, ongoing dimension (selector + scope state +
org-rollup-vs-space request-path switch + a Space column). For the **form**,
scope matters at exactly one moment — **create**, choosing which space to create
into — and is **immutable on edit** (a connector can't move scope; today's
`ConnectorForm` already renders scope read-only in edit).

So the entire tier reduces to: on the *create* variant, compose a `<ScopeField>`
child and default the mutation's parent path from it. That is composition, not a
tier. **Recommendation:** do **not** introduce a `ScopedResourceFormPage` tier;
instead let `ResourceFormPage.Create` accept a `<ScopeField>` in its Body
(composition), and the resource's create mutation reads scope from its own form
values (where `ConnectorFormValues.scope` already lives). If a second scoped
resource later shows real shared create-time scope logic (e.g. permission-gated
space lists), promote it then. This mirrors the Grid doc's own honesty test
("if it doesn't fit, that tier collapses back into helpers — which is fine").

| Resource | Tier |
|---|---|
| Generic / non-resource page form | `FormPage<T>` |
| Flat CRUD AIP resource (tags, api-keys, storage-gateways) | `ResourceFormPage.{Create,Edit}` |
| Scoped AIP resource (connectors, secrets, …) | `ResourceFormPage.{Create,Edit}` + a composed `<ScopeField>` on Create |

## Return to the launching route

After **submit-success OR cancel**, the form must return to the exact list view
that launched it — filters, scope, page, and scroll are already encoded in the
origin's URL, so the origin URL *is* the captured view.

**Recommended: an explicit `from` / `returnTo` search param.** The list's New /
Edit / row action sets it when navigating to the form:
`/connectors/new?from=<encoded origin URL>` and
`/connectors/{id}/edit?from=<encoded origin URL>`. On cancel / submit-success the
route reads `from`, sanitizes it, and navigates there; **fallback** to the
resource's list route when `from` is absent or invalid. This survives refresh,
deep-link, and direct navigation; it's SSR-safe (no history dependence) and
shareable.

**Rejected: `router.back()`.** Fragile: a refresh, deep-link, direct
navigation, or an external referrer has no in-app history entry, so `back()` can
land on the wrong page or **exit the app entirely** (worse in electron, where
there's no "browser back to a safe origin"). It's also not SSR-safe and not
shareable. The `from` param is deterministic where `back()` is ambient.

### Security — sanitize `from` to an internal same-app path (required)

`from` is attacker-controllable (it's in the URL). Before navigating, reduce it
to an **internal, same-origin absolute path**; anything else falls back to the
list route. This is an open-redirect defense.

```ts
/** Return a safe same-app path (pathname+search+hash), or null to force the fallback. */
function safeInternalPath(from: string | undefined, appOrigin: string): string | null {
  if (!from) return null;
  // Reject scheme-relative ("//evil.com"), backslash tricks ("/\evil.com"),
  // and anything not starting with a single "/".
  if (!from.startsWith('/') || from.startsWith('//') || from.startsWith('/\\')) {
    return null;
  }
  try {
    const url = new URL(from, appOrigin); // resolve against our own origin
    if (url.origin !== appOrigin) return null; // absolute URL to another host → reject
    return url.pathname + url.search + url.hash; // strip origin; keep only the path
  } catch {
    return null;
  }
}
```

Reject list: external URLs (`https://evil.com/…`), protocol-relative
(`//evil.com`), backslash-normalization tricks (`/\evil.com`), control-char
smuggling, and any non-`/`-leading value. Accept: a single-slash-leading path
that resolves same-origin. Electron uses its app origin (e.g. the custom
`pivox://` scheme host or the file origin) as `appOrigin`. Because we only ever
navigate to `pathname + search + hash`, an accepted value can't carry a foreign
origin even if `new URL` were lenient.

### Keeping FormPage router-free

`FormPage` never reads `from` and never navigates. The **route** does:

```ts
// start route (electron shell is analogous with its own navigate)
const returnTo = safeInternalPath(search.from, appOrigin) ?? listRouteFor(resource);
// …passed into the resource provider, which wires the generic contract:
//   actions.cancel        = () => navigate(returnTo)
//   onSubmitSuccess()     = () => navigate(returnTo)   // called in the mutation's onSuccess
```

The provider injects `actions.cancel` and calls `onSubmitSuccess` from the
mutation's `onSuccess` (interaction in a handler, `5.8 logic-in-handlers` — not
an effect watching a success flag). Electron supplies its own `navigate` and
`appOrigin`. The exact injected contract into the feature/provider:
`{ returnTo: string, navigate: (to: string) => void, onSubmitSuccess: () => void }`
— all router-shaped values computed outside the composite.

## Create vs edit routing + edit-record load

| Flow | Route | Record load |
|---|---|---|
| Create | `/connectors/new?from=…` (scoped: scope chosen in-form, or a `…/spaces/{space}/connectors/new` entry) | none |
| Edit | `/connectors/{id}/edit?from=…` | SSR-prefetch (start) / client-fetch (electron) |

**Edit load mirrors the list loader.** start's connectors list route already
SSR-prefetches rows in its `loader` and primes react-query under the
byte-identical key (see `web/apps/start/src/routes/_app/connectors/index.tsx`).
The edit route does the same for a single record: `loader` fetches the connector
as the user and `setQueryData` under the same key the client hook reads, so the
record is in the server-rendered HTML and no XHR fires on load. Client
navigations skip the loader; the client query fetches with `keepPreviousData` to
avoid a flash. The provider maps that query into `state.record` /
`state.recordLoading` / `state.loadError`. Running the record fetch and any
sibling fetch (e.g. the space list for a scope field) in parallel follows
`1.5 Promise.all` / `3.7 parallel-fetch` — no waterfall.

The form seeds from `state.record` via **lazy state init**
(`5.12 lazy-state-init`: `useState(() => seedFrom(record))`) and resets across
records via a **keyed remount** (`5.1 keyed-reset`:
`<ConnectorEditFields key={record?.name ?? 'new'} />`) — not an effect that
copies props into state. `patch` uses functional updates
(`5.11 functional-setState`: `setValues(v => ({ ...v, ...next }))`). All field
variant components are module-level (`5.4 no-component-in-component`), never
defined inside a render.

## Dirty-state guard (unsaved-changes prompt)

Three navigation paths, three mechanisms — split by which layer legitimately
owns them:

1. **Cancel button** — we own it. `FormPage.Cancel` reads `state.dirty`; when
   dirty it opens a small unsaved-changes **confirm** (same shape as
   `DeleteDialog` — a confirm, not a form) before calling `actions.cancel`.
   Router-free, cross-env.
2. **Hard unload (reload / close tab / quit)** — a `beforeunload` handler
   subscribed **only while `state.dirty`**, inside `FormPage.Provider`. This is
   a genuine external-system subscription (a window event), the sanctioned use
   of an effect; it registers/cleans up on the `dirty` transition and does no
   React state work. Works in electron's `BrowserWindow` too. Router-free.
3. **Soft in-app navigation (clicking another link)** — router-specific;
   **must** live in the route to keep `FormPage` router-free. The route wires
   its blocker (start: `useBlocker`; electron: its history guard) to the dirty
   signal surfaced via `meta.onDirtyChange`.

**Open detail (recommended approach):** how the route learns `dirty` without
`FormPage` importing the router. Recommended: `meta.onDirtyChange?(dirty)` — the
provider reports the derived `dirty` up so the route can drive its own blocker.
Because `dirty` is derived during render, reporting it up is the one place a
small `useEffect(() => onDirtyChange?.(dirty), [dirty])` is justified (syncing
an external router system to React-derived state, the inverse-subscription case
react.dev sanctions). Alternatives considered: reporting from the `patch`
handler (misses programmatic resets) and lifting values to the route (violates
resource-owns-values). Flagged as the one spot where an effect is defensible;
call it out in review.

## Connectors + secrets: before → after

**Before (dialog).** The list route renders `<ConnectorsAdmin.Root>`, which
mounts a `<FormDialog>` wrapping `<ConnectorForm>`; `openCreate` / `openEdit`
flip `dialog.open`; submit closes the dialog and stays on the list. The form
fights modal pointer-lock with `popupMountRef` + `dialogBoundary` portal hacks.

**After (routed page).**

- Grid `openCreate` / `openEdit` become `navigate('/connectors/new?from=…')` /
  `navigate('/connectors/{id}/edit?from=…')`.
- New route files (`/connectors/new`, `/connectors/{id}/edit`) render
  `ResourceFormPage.Create` / `.Edit` with the connector fields slotted into
  `FormPage.Body`. The edit route SSR-prefetches the connector.
- `ConnectorForm` splits into `ConnectorCommonFields` +
  `ConnectorCreateFields` (identifier + editable scope) +
  `ConnectorEditFields` (scope read-only, no identifier) — the `isCreate`
  ladder disappears. The combobox portal hacks disappear with the modal.
- `FormActions` is absorbed by `FormPage.Actions` + `FormPage.Submit` /
  `FormPage.Cancel`; `FormDialog` is deleted.
- Secrets follow identically: `SecretCreateFields` (identifier + required value)
  vs `SecretEditFields` (rotate checkbox gating the value field). `SecretForm`'s
  `isCreate` / `valueRequired` branching becomes the two variants.

The `Connector` / `Secret` mutation (create vs update RPC) stays entirely in the
resource layer — `FormPage` never sees it.

## Extraction plan

1. Build `FormPage<T>` (compound parts + `FormPageContextValue<T>` +
   `useFormPage`), lifting `FormActions` into `FormPage.Actions`/`.Submit`/
   `.Cancel` and the delete-confirm plumbing from `ResourceGrid`.
2. Add `ResourceFormPage.Create` / `.Edit` (standard labels, load states,
   delete-confirm). Convert **connectors** onto routed pages — validates the
   contract on the richer (scoped, multi-field) resource first.
3. Convert **secrets** (rotate/value variant) — a 2nd resource before locking
   the API. Delete `FormDialog`; retarget the grid edit actions to navigate.
4. Wire the `from` param + `safeInternalPath` + dirty-guard split in the routes.

## Open questions

- **Scoped form tier.** Recommendation is *no* `ScopedResourceFormPage` tier
  (scope-on-create is a composed `<ScopeField>`); confirm before a second scoped
  resource forces the question.
- **Dirty→route signal.** `meta.onDirtyChange` + the one sanctioned effect vs.
  an alternative that avoids the effect entirely — flagged above; decide at
  implementation.
- **Edit-record SSR key sharing.** The edit loader must `setQueryData` under the
  exact key the client hook reads (the list route already does this for lists);
  confirm the single-record query key helper exists or add it alongside the list
  one.
- **Create entry for scoped resources.** Whether scope is always chosen in-form
  (`/connectors/new`) or a space can deep-link a pre-scoped create
  (`/spaces/{space}/connectors/new`); the `from` mechanism supports both.
