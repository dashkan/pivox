# Workflow UI

Plan for the web UI over the workflow backend. React Flow (`@xyflow/react`,
already a dependency) skinned with the Vercel **ai-elements** primitives, wired
to the existing OpenAPI-typed client through the `start` BFF.

Status: **Phase 1 code-complete + reviewed.** T1–T6 + BE-1 landed; 3 adversarial
reviewers passed (no blockers); all should-fix items fixed + re-validated;
automated gate green (frontend 159+17+51, backend `make test` — only the
pre-existing `dashboards` seed test fails). Live verification (routes + PATCH-mask
behavior on the restarted Aspire) is the last step before Phase 1 formally closes.
Phases 2–3 + CLEANUP-1 not started. Decisions in [§3](#3-locked-decisions).

---

## 1. Goal

Give customers a UI to:

- **See** a workflow definition as a diagram (read-only), and **watch** a run
  execute live on that same diagram.
- **Author** workflows end-to-end — create, edit, version, promote — through a
  structure-aware editor.
- Manage the supporting resources: **Connectors**, **versions**, **runs**.

Three phases:

- **Phase 1 — read-only viz (RO).** View a workflow definition as a diagram.
  Ships the canvas renderer, the AST→graph transform, the data layer, and the
  read-only surfaces (workflows list, definition canvas, versions, connectors).
  No run state yet.
- **Phase 2 — run.** Watch runs execute live on the diagram, browse run history,
  trigger and cancel runs. The run monitor is the killer surface. Reuses Phase
  1's canvas + transform.
- **Phase 3 — edit.** Full structured authoring — create, edit, version,
  promote, fork. Bolts onto Phase 1's canvas + transform without a rewrite (a
  design constraint on Phase 1, not an afterthought).

---

## 2. Backend model (the contract the UI binds to)

Source of truth: `api/proto/pivox/workflows/v1/{workflow,workflow_run,connector}.proto`.
Engine semantics: `internal/engine/`. Read these before implementing; the
summary below is orientation, not a substitute.

**The backend is a block-structured AST, not a free-form node/edge DAG.** This
single fact shapes the entire UI. There are no arbitrary edges — every construct
is a tree node with a fixed shape.

### Resource graph

- **`Workflow`** — the container. Points at a live `WorkflowVersion` (`version`
  field, output-only, set via `PromoteWorkflowVersion`). Holds persistent
  `config` (a `Struct`), `enabled`, and `origin`:
  - `OWNED` — customer-owned, structurally editable.
  - `MANAGED` — Pivox system workflow; editable only via `config`, or **forked**
    (`ForkWorkflow`) for structural change. The editor must refuse structural
    edits on MANAGED workflows and offer "Fork to edit" instead.
- **`WorkflowVersion`** — **immutable** definition. Editing = mint a new version
  (`CreateWorkflowVersion`) → it's a **draft** until `PromoteWorkflowVersion`
  makes it live. Carries `parameters` (the run input contract, `ParameterDef[]`),
  an optional `trigger`, the `root` `Sequence`, and an optional `error_sequence`.
- **`Connector`** — reusable credentialed HTTP endpoint. `base_url` + `headers`
  are CEL over the connector-config env, the **only** place `secret("…")`
  resolves. Optional `agent` routes execution to an on-prem agent. Referenced by
  name from `HttpActivity`.
- **`WorkflowRun`** — one execution of a pinned version. LRO-shaped: `state`
  (PENDING/RUNNING/WAITING/SUCCEEDED/FAILED/CANCELLED), `input`, `output`,
  `error`, `triggered_by`, timings, and **`steps[]` — per-step `StepState`**
  (`step_id`, `state`, `output`, `error`, timings). This array is how the run
  monitor lights up the diagram.

### The AST (`WorkflowVersion.root`)

```
Sequence   = ordered Step[]
Step       = { id, oneof kind }        // id is unique across the version
  kind:
    Activity   = oneof { http | set | run_workflow | fail | end }
    Condition  = { Branch[] branches, Sequence? otherwise }   // if / else-if / else
    Parallel   = { Sequence[] branches }                       // concurrent, join at end
    Try        = { Sequence body, Sequence? catch, bool rethrow }
Branch     = { string when (CEL), Sequence then }
```

Activity leaves:

- **`http`** — `connector` (ref), `method`, `path`/`query`/`headers`/`body` (CEL),
  `retry` (`RetryPolicy`), `success_status[]`, `retryable_status[]`.
- **`set`** — `assignments`: map of var name → CEL. The data-transform activity.
- **`run_workflow`** — `workflow` (ref) + `parameters` (CEL map). Sub-workflow
  invocation (cycle-guarded, depth-capped).
- **`fail`** — raise a catchable error (message). Caught by an enclosing `Try`,
  else runs `error_sequence` then FAILs the run. Never retried.
- **`end`** — terminate the run **successfully**, unwinding all blocks
  (cancelling in-flight `Parallel` siblings). Not an error; `Try` doesn't catch
  it, `error_sequence` doesn't run.

CEL is everywhere (guards, activity inputs, transforms). The run context exposes
`steps.<id>.output`, `vars.<name>`, params, and (in a `catch`/`error_sequence`)
`error`. `secret()` is **not** available in run-context CEL — only connector
config. The editor's CEL fields (Phase 2) must reflect that scoping.

### Engine execution (for the run monitor)

`internal/engine/interpreter.go` tree-walks `root`, emitting per-step lifecycle
to a `StepReporter`. Terminal outcomes: completed (incl. `end`), failed (runs
`error_sequence` on uncaught terminal error), cancelled. Retryable infra faults
re-run the whole job and do **not** fire `error_sequence`. The monitor is a
projection of `WorkflowRun.steps` onto the AST by `step_id` — it does not model
the engine's control flow itself, it reflects the reported states.

### RPC surface

| Service | RPCs | UI use |
|---|---|---|
| `Workflows` | List/Get/Create/Update/Delete/Fork/PromoteWorkflowVersion | list, header, container edit, promote, fork-to-edit |
| `WorkflowVersions` | List/Get/Create/Delete | versions tab, load definition, save draft |
| `WorkflowRuns` | RunWorkflow/GetWorkflowRun/ListWorkflowRuns/CancelWorkflowRun | runs list, run detail, manual run, cancel |
| `Connectors` | List/Get/Create/Update/Delete | connectors CRUD |
| `Secrets` (`pivox.secrets.v1`) | List/Get/Create/Update/Delete | secrets CRUD, set-only (value `INPUT_ONLY`, never returned; delete-in-use → `FAILED_PRECONDITION`) |

All are already exposed as REST via grpc-gateway, **implemented server-side**
(incl. `internal/service/secrets`), and **already present in the generated
OpenAPI TS client** (`packages/client/src/generated/types.gen.ts`) — no
proto/client regen needed. Paths are `/v1/{parent=organizations/*}/workflows`
etc., reachable through the `start` BFF proxy at `/api/v1/*`.

---

## 3. Locked decisions

| Decision | Choice |
|---|---|
| Phasing | Phase 1 read-only viz (RO) → Phase 2 run (monitor + history + trigger/cancel) → Phase 3 edit (full structured authoring) |
| Editing model | **Structured editor** (palette + reorder + wrap-in-block + inspector), serializes to a valid AST. **Not** free-form edge drawing — the block structure can't represent arbitrary DAGs. Same expressive power as the backend. |
| Nesting on canvas | **Container/group nodes** — `Condition`/`Parallel`/`Try` are React Flow parent nodes; child steps nest inside their bounds. |
| Packaging | Reusable: `@pivox/ui/workflow` (presentational) + `@pivox/features/workflows` (data + transform) + routes in `apps/start`. Mirrors the `chat` feature. |
| Supporting surfaces | All four in scope: Connectors CRUD, Versions + promote, Runs list + detail, Manual run + cancel. |

### Coding conventions (binding on all tasks)

1. **Comments are technical, not editorial.** Comment *what a non-obvious piece
   of code does*, never *why a decision was made*. Rationale, trade-offs, and
   alternatives-considered live in this doc, not in the source. No decision
   narration, no "we chose X because Y" in code. Do **not** copy the verbose
   decision-commentary style of some existing files (e.g.
   `apps/start/src/lib/api-client.ts`).
2. **TDD, DRY, KISS.** Test-first for every behavior change (§10). No premature
   abstraction — extract on the third repetition, not the first. Simplest thing
   that satisfies the requirement.
3. **Composition skills are mandatory where they apply.** Follow
   `vercel-composition-patterns` to the letter for the workflow components (they
   are compound + composable):
   - Activity node renderers are **explicit variant components** per kind
     (`HttpNode`, `SetNode`, …) — not one node switching on a `kind` prop, not
     boolean-prop modes (`architecture-avoid-boolean-props`,
     `patterns-explicit-variants`).
   - Canvas / inspector / editor are **compound components with shared context**;
     the provider owns state and is the only place that knows how it's managed
     (`architecture-compound-components`, `state-lift-state`,
     `state-decouple-implementation`, `state-context-interface`).
   - Compose via **children, not `renderX` props** (`patterns-children-over-render-props`).
   - **React 19** (19.2.7): no `forwardRef` (ref is a prop); `use()` over
     `useContext()` (`react19-no-forwardref`).
   Also apply `vercel-react-best-practices` (re-render, memoization, async,
   bundle rules). **`vercel-react-view-transitions` is out of scope** — verified
   July 2026 that `<ViewTransition>`/`addTransitionType` are canary-only and the
   pinned `react@19.2.7` does not export them; see §12. Do not reach for it.
4. **No overreaching decisions.** Anything not settled in this doc — a new
   dependency, a data-shape choice, a UX fork, a deviation from the plan — stops
   and asks. Surface options; don't bury a choice in a diff.
5. **Flag codebase problems.** When a task touches or reads code that is
   structurally wrong or inconsistent, surface it as a separate note — don't
   silently fix or silently cargo-cult it.

---

## 4. Architecture & packaging

Follows the established three-layer split (`primitives` → `ui` → `features` →
`apps/start`), same as `chat`/`app-shell`.

```
web/packages/ui/src/workflow/               # presentational, data-free
  workflow-canvas.tsx                        # <ReactFlow> host: nodeTypes, edgeTypes, layout apply
  nodes/
    activity-node.tsx                        # http | set | run_workflow | fail | end renderers
    container-node.tsx                       # Condition | Parallel | Try group nodes
    branch-node.tsx                          # a Condition branch / Parallel lane header
  run-status.tsx                             # State → color/icon/badge (shared by node + list)
  inspector/                                 # Phase 3: per-kind config panels
  index.ts

web/packages/features/workflows/src/
  resource-paths.ts                          # pure: AIP name → openapi path params + isSpaceScoped
  transform/
    ast-to-graph.ts                          # WorkflowVersion.root → { nodes, edges }
    graph-to-ast.ts                          # Phase 3: editor graph → WorkflowVersion
    layout.ts                                # elk hierarchical layout over container nodes
    ids.ts                                   # stable node ids ↔ AST step paths
  use-workflow-definition.ts                 # load + memoize graph for a version
  use-workflow-run.ts                        # poll GetWorkflowRun, merge StepState onto graph
  use-workflow-editor.ts                     # Phase 3: editor state machine over the AST
  index.ts

web/apps/start/src/routes/_app/
  workflows/
    index.tsx                                # workflows list
    $workflowId.tsx                          # workflow detail shell (tabs: Definition | Runs | Versions | Settings)
    $workflowId.runs.$runId.tsx              # run detail + live monitor
  connectors/
    index.tsx                                # connectors list + create/edit
  secrets/
    index.tsx                                # secrets list + create/rotate/delete (set-only)
  runs/
    index.tsx                                # org/space-wide runs list (workflows/-; needs BE-1)
```

### Navigation / information architecture

The workflow UI introduces the first `navMain` sidebar groups (today the shell
has only the org picker, a Spaces group, and the user menu). `nav-main.tsx`
already supports a collapsible group with `items[]` subitems.

```
├─ Workflows
│   ├─ Definitions   → /workflows                     (the authored workflow catalog)
│   ├─ Runs          → /runs   (org/space-wide; via workflows/- — needs BE-1)
│   └─ Connectors    → /connectors
└─ Admin
    └─ Secrets       → /secrets
```

Names track the API vocabulary: "Definitions" (a `WorkflowVersion` is an
immutable *definition*), "Runs" (`WorkflowRun`), "Connectors", "Secrets" — all
plural, matching the existing "Spaces" group. "Admin" is scoped to Secrets for
now; it is the natural later home for IAM/members, API keys, and org settings
(all real resources), added when each UI lands, not speculatively.

The top-level **Runs** view is org/space-wide (all runs across workflows). The
current API only lists runs under a single workflow, so this depends on the
backend prerequisite **BE-1** below; until it lands, runs are reachable only via
the per-workflow Runs tab (§7.2).

### Backend prerequisite — BE-1: org/space-wide run listing

Separate backend task (not UI); gates the top-level Runs view. Follows
`internal/CLAUDE.md` (TDD, `apierr`, sqlc, tx rules) + `aip-reviewer`,
`golang-grpc`, `golang-database`, `golang-safety` skills.

- **API shape (AIP-159 `-` wildcard).** Reuse `ListWorkflowRuns`; no new RPC or
  HTTP binding — `workflows/*` already matches `workflows/-`:
  `GET /v1/organizations/{org}/workflows/-/runs` and the `spaces/{space}`
  variant. The handler detects `-` in the workflow segment and lists across
  workflows in that scope. Permission check moves to org/space scope
  (`workflows.read` on the org/space, not one workflow).
  - **Membership model (confirmed).** Org membership is a prerequisite for space
    membership — there is no pure space-only persona. `MembershipRequiredInterceptor`
    gates every RPC on org membership, so a caller without org membership is denied
    everything (including a space wildcard) before any permission check. BE-1's
    cross-scope isolation test encodes this (org-viewer + space-editor: allowed
    the space wildcard, denied the org wildcard). By design; not a gap.
  - **Rollup semantics (decided + shipped).** Org-scope `workflows/-` returns ALL
    runs in the org, **including** runs of space-scoped workflows (org = org +
    all its spaces; the "global" view). Space-scope `workflows/-` returns only
    that space's runs. Org-scope per-run resource names are built from each run's
    actual location (space-scoped runs carry the `spaces/{space}` segment), so
    the handler resolves each run's space slug (batched, not N+1).
  - **AIP-159 (resolved).** Compliant: AIP-159's one naming "must" is that
    responses use each resource's canonical name with real ids (no `-`), which we
    do. AIP-159 does not constrain returned-name depth vs. the queried collection
    and doesn't address multi-parent-pattern resources, so the org rollup
    returning names at mixed depths is fine. `make api-lint` is green. (An early
    review read a stricter rule into AIP-159 that isn't in the text — dismissed
    after checking the spec.) Pending nicety: a proto field-doc note that `parent`
    accepts `-` to read across nested collections (undocumented today).
- **Storage (denormalize + index).** `workflow_runs` has only `workflow_id`
  today. Add `org_id` (NOT NULL) + `space_id` (nullable), mirroring `workflows`;
  set them on insert from the run's workflow; backfill existing rows from
  `workflows`. Add keyset indexes `(org_id, id)` and `(space_id, id)`. Pre-prod:
  edit the init migration directly. New sqlc query for the scoped list;
  keyset-paginated like the per-workflow one.
- **Filter/order** parity with the existing per-workflow list (`state` filter,
  order_by).

**Data flow (follows the `app-shell` / `create-org` precedent — verified).** The
**consumer route owns client creation, injection, routing, and SSR**; the feature
receives the client and exposes **domain** shapes, never raw react-query types:
- Presentational `ui/workflow` components take plain props (`nodes`, `edges`,
  `onSelect`, `runState`) and never call the API.
- `features/workflows` domain hooks (`use-workflow-definition`, `use-workflow-run`,
  `use-workflow-editor`) take an **injected** `$api: ReactQueryApi` (type imported
  from `@pivox/client/react-query` — so features needs NO `@tanstack/react-query`
  dep) and/or an injected `apiClient`. They call `$api.useQuery(...)` internally
  for reads and `apiClient.POST/PATCH/DELETE(...)` for writes (the `create-org`
  pattern), run the AST↔graph transform, poll runs — and **return domain state**
  (`{ graph, run, isLoading, actions }`), never a `UseQueryResult`.
- `resource-paths.ts` (pure, tested) maps an AIP resource name → openapi path
  params, so a single hook handles both the org- and space-scoped path variants;
  there is no monolithic pass-through `api.ts`.
- Routes create `$api`/`apiClient` and inject them into the feature (as `_app.tsx`
  does for `AppShellFeature`). This keeps the canvas reusable (electron can host
  it) and the transform unit-testable in isolation.

**Auth/data path** is unchanged: browser → `start` BFF (`/api/v1/*`, injects the
Keycloak bearer from the httpOnly session) → cloud `/v1/*`. Use `$api` from
`apps/start/src/lib/api-client.ts`. SSR prefetch (as in `_app.tsx`) is optional
for the list pages and can be added later; not required for v1.

---

## 5. The AST ↔ graph transform (the crux)

This is the highest-risk, highest-value module. All three phases depend on it;
it is where correctness lives. Build it test-first (pure functions, no DB/RPC — ideal
for unit tests).

### `ast-to-graph` (Phase 1, reused by all phases)

Walk `root` depth-first, producing React Flow `nodes` + `edges`:

- Each `Activity` step → one **activity node**, `data` carrying the kind +
  config + the **AST path** (e.g. `root.steps[2].try.body.steps[0]`) as a stable
  id. The path is the join key for run state (Phase 2) and the anchor for edits
  (Phase 3).
- Each `Condition`/`Parallel`/`Try` step → one **container node** (React Flow
  `parentId` grouping), with:
  - `Condition` → a branch sub-header per `Branch` (label = the `when` CEL) plus
    an `otherwise` region.
  - `Parallel` → one lane per branch `Sequence`.
  - `Try` → `body` region + `catch` region + a `rethrow` marker.
- **Edges** are *implied*, never authored, and **explicit at boundaries (decided)**
  — additive to the container hierarchy, which stays:
  - sequential steps in a `Sequence` → a linear edge;
  - `Condition` → a labeled edge to each branch's first step (label = the branch
    `when`; `otherwise` labeled "else");
  - `Parallel` → fork edges to each lane's first step + join edges from each
    lane's last step to the Parallel's continuation (synthetic join marker ok);
  - `Try` → an edge into the body's first step + a labeled "catch" edge into the
    catch region's first step.
- **`error_sequence` (decided).** Rendered as a SECOND region with its own
  id-space (an `error` root, distinct from `root`), surfaced as a separate,
  clearly-labeled "on error" region not wired into the main flow's edges. A
  version without an `error_sequence` renders only `root` (no phantom region).
- **Layout** via `elkjs` (`layered`/`mrtree` with hierarchy support) — it sizes
  and positions container nodes and their children. React Flow does not
  auto-layout nested nodes; elk does. Run layout in `layout.ts`, memoized on the
  version identity.

Invariants to test: every AST step yields exactly one node; every node id maps
back to a unique AST path; deeply nested (`Try` inside `Parallel` inside
`Condition`) round-trips; empty sequences render a placeholder drop target
(Phase 3) but no phantom node (Phase 1).

### `graph-to-ast` (Phase 3)

The inverse, driven by editor operations rather than free edge topology. The
editor mutates an **in-memory AST** (not the graph); the graph is re-derived from
the AST after each edit via `ast-to-graph`. This avoids the DAG-reconciliation
trap entirely: the AST is always the source of truth, the canvas is always a
projection. Operations: insert step (at a path), delete, reorder within a
sequence, wrap selection in `Try`/`Condition`/`Parallel`, edit a leaf's config,
add/remove a `Condition` branch or `Parallel` lane. Each op is a pure
`(ast, op) → ast` function — test-first, exhaustively.

Validation before `CreateWorkflowVersion`: unique step ids, required fields per
kind, connector refs resolve, CEL non-empty where required. Surface errors
inline; optionally use `validate_only: true` on the create RPC as a server-side
check.

---

## 6. Phase 1 — read-only viz (RO)

View a workflow definition as a diagram. No run state. Ships the canvas, the
transform, and the read-only surfaces.

### 6.1 Canvas renderer (`ui/workflow` + `features/workflows/transform`)
- `WorkflowCanvas` hosting `<ReactFlow>` with custom `nodeTypes`
  (activity/container/branch) and the ai-elements skin (`Canvas`, `Controls`,
  `Panel`, `Node`, `Edge`). Read-only: `nodesDraggable={false}`,
  `nodesConnectable={false}`, pan/zoom + fit-view on.
- Activity node renderers per kind, using `Node`/`NodeHeader`/`NodeContent` from
  ai-elements: icon + kind label + a one-line summary (method + path for http,
  target vars for set, target workflow for run_workflow, message for fail).
- Container nodes for Condition/Parallel/Try with branch/lane/body-catch regions.
- `ast-to-graph` + `elk` layout wired through `use-workflow-definition`.

### 6.2 Read-only surfaces
- **Workflows list** (`workflows/index.tsx`) — `ListWorkflows`, table/cards:
  display name, origin badge (OWNED/MANAGED), enabled, live version, updated-by.
- **Workflow detail shell** (`$workflowId.tsx`) — tabs: **Definition** (canvas of
  the live version), **Runs** (Phase 2), **Versions**, **Settings**.
- **Versions tab** — `ListWorkflowVersions`, live badge, notes, create-time;
  selecting a version renders its definition on the same canvas.
- Click a node → side panel (ai-elements `panel`) showing that step's static
  config (connector, method, CEL, retry, etc.).

### 6.3 Connectors CRUD (Workflows nav)
- List + create/edit form (`base_url`, `headers` map with `secret(...)` helper
  text, `agent` picker), delete with etag. Straightforward forms; no canvas.
  Included in Phase 1 so `http` activities are legible (nodes can resolve
  connector display names) and because it's independent of the run/edit work.
- **Future — reachability validation on save (documented, not built).** When an
  HTTP connector is saved, validate that its `base_url` is actually reachable
  from the execution locality: the cloud (api/worker) when no `agent` is set, or
  the chosen on-prem agent's network when one is. A cloud-side probe is a
  Cloud-Controller action; an agent-side probe rides the agent stream. Surface
  the result as a non-blocking warning (a connector can be saved before its
  target is up). Scope it as its own task when Connectors graduate past basic
  CRUD.

### 6.4 Secrets CRUD — set-only (Admin nav)
The credential vault (`pivox.secrets.v1.Secrets`) — fully implemented backend,
already in the generated client. **Set-only, enforced by the API**: `value` is
`INPUT_ONLY` and never returned, so the UI never displays a value.
- **List / view** — metadata only (display name, actors, timestamps,
  annotations, etag). No value, ever; no "reveal."
- **Create** — value required (`max_len` 65536; multi-field creds go in as a JSON
  blob the connector parses).
- **Rotate** — `UpdateSecret` with `value` in the field mask; metadata-only edits
  omit `value` from the mask.
- **Delete** — guard etag; `DeleteSecret` returns `FAILED_PRECONDITION` when a
  connector still references it (no cascade). Catch it and name the referencing
  connector(s) instead of surfacing a raw error.

**Phase 1 exit criteria:** a customer can browse workflows, connectors, and
secrets; open a workflow and read its definition (any version) as a clear nested
diagram with per-step config inspectable; and manage connectors + secrets
(secrets set-only).

---

## 7. Phase 2 — run (monitor + history + trigger/cancel)

Reuses Phase 1's canvas + transform; adds run state, history, and control.

### 7.1 Run monitor (`features/workflows/use-workflow-run` + `run-status`)
- `use-workflow-run(runName)` polls `GetWorkflowRun` (react-query
  `refetchInterval`, stop when state is terminal), merges `steps[]` onto the
  graph by `step_id`/AST path.
- Node overlay: per-step `State` → border/badge/icon (PENDING dim, RUNNING pulse
  via `Edge.Animated`/shimmer, WAITING amber, SUCCEEDED green, FAILED red,
  CANCELLED grey). Reuse ai-elements `shimmer`/`task` where they fit.
- Run header: overall state, trigger (`triggered_by`), timings, `input`/`output`
  (JSON via ai-elements `code-block`), `error` on failure.
- Click a step → side panel with that step's `output` / `error` / timing
  (extends the Phase 1 config panel with runtime data).

### 7.2 Runs list + run detail
- **Per-workflow Runs** (Runs tab) — `ListWorkflowRuns` with state filter
  (AIP-160 `filter`), paginated; row → run detail.
- **Org/space-wide Runs** (`/runs`, top-level nav) — same list hook against
  `workflows/-` (BE-1). Adds a workflow column since runs span workflows. Ships
  once BE-1 lands; the per-workflow tab does not depend on it.
- **Run detail** (`$workflowId.runs.$runId.tsx`) — hosts the monitor.

### 7.3 Manual run + cancel
- **Run** button on the workflow header → parameter form generated from the live
  version's `ParameterDef[]` (type-aware inputs; defaults prefilled) →
  `RunWorkflow` → navigate to the new run's detail.
- **Cancel** on a RUNNING/WAITING run → `CancelWorkflowRun`.

**Phase 2 exit criteria:** a customer can trigger a manual run, watch it execute
live step-by-step on the diagram, inspect per-step outputs/errors, browse run
history, and cancel an in-flight run.

---

## 8. Phase 3 — edit (full structured authoring)

Bolts onto Phase 1's canvas + transform. The AST is the edit target; the graph
re-derives after each op ([§5](#5-the-ast--graph-transform-the-crux)).

- **Editor state** (`use-workflow-editor`) — holds the working AST, an op log
  (undo/redo), dirty tracking, validation state.
- **Step palette** — drag/insert a new step (http/set/run_workflow/fail/end) or a
  container (Try/Condition/Parallel) at a drop target between siblings or into a
  container region. Container nodes expose insertion points; there is no
  free-edge draw.
- **Inspectors** (`ui/workflow/inspector/`) — per-kind config panels:
  - http: connector picker, method, path/query/headers/body CEL fields, retry
    policy, success/retryable status chips.
  - set: assignment rows (var name + CEL).
  - run_workflow: workflow picker + parameter CEL map.
  - fail: message. end: (none). Condition branch: `when` CEL. Try: `rethrow`.
  - version-level: `parameters` contract editor, `trigger`, `error_sequence`.
- **CEL editing** — start with a plain monospace field + inline validation;
  scope hints per location (no `secret()` in run-context). A richer CEL
  editor/autocomplete is a later enhancement, not Phase 3 blocking.
- **Save flow** — validate → `CreateWorkflowVersion` (draft) → optional
  `PromoteWorkflowVersion`. MANAGED workflows: structural edits disabled; offer
  `ForkWorkflow`. New-workflow flow: `CreateWorkflow` (container) →
  `CreateWorkflowVersion` → promote.
- **Create Workflow** entry point from the list.

**Phase 3 exit criteria:** a customer can create a new OWNED workflow from
scratch, author its full definition (all step kinds, nesting, CEL), save it as a
version, and promote it live — with MANAGED workflows guarded behind fork.

---

## 9. Dependencies to add

- **`elkjs`** — hierarchical/nested auto-layout (React Flow won't lay out nested
  container nodes). Add to `@pivox/features/workflows` (or `primitives` if we
  want it colocated with `@xyflow/react`); pin via the pnpm `catalog`.
- (Consider) **`@xyflow/react` layout helpers** already suffice for edges; no
  dagre needed if we standardize on elk.
- Everything else (`@xyflow/react`, `@tanstack/react-query`, ai-elements,
  shadcn) is already present.

---

## 10. Testing (TDD — required)

Per repo policy, tests come first for every behavior change.

- **Transform** (`ast-to-graph`, `graph-to-ast`, edit ops) — pure-function unit
  tests, the bulk of the coverage. Table-driven over AST shapes incl. deep
  nesting and every activity kind. This is where correctness is proven.
- **Hooks** — `use-workflow-run` polling/termination and StepState merge; mock
  `$api` at the fetch boundary.
- **Components** — render tests for node renderers and run-state overlays
  (webapp-testing skill / vitest + testing-library).
- **Routes** — smoke tests for load + empty/error states.
- Layout (`elk`) is not unit-tested for pixel positions — assert structural
  invariants (node count, parent/child grouping), not coordinates.

---

## 11. ai-elements / primitives inventory

Reuse, don't reinvent (`web/packages/primitives/src/vercel/ai-elements/`):

| Component | Use |
|---|---|
| `canvas` | React Flow host (Background wired to `--sidebar`) |
| `node` | activity + container node chrome (Card-based) |
| `edge` | `Edge.Animated` (running flow) / `Edge.Temporary` |
| `controls` | zoom/fit controls |
| `panel` | canvas overlays (minimap toggle, run legend) |
| `toolbar` | node hover toolbar (Phase 3: edit/delete/wrap) |
| `connection` | Phase 3 insertion affordance styling |
| `code-block` | JSON input/output/error rendering |
| `shimmer` / `task` | running-state affordances in the monitor |
| shadcn (card, badge, tabs, form, select, table) | shells, forms, lists |

---

## 12. Risks & open questions

- **elk nested layout tuning** — nested container sizing/spacing is the fiddliest
  visual work; budget iteration. Structural invariants are testable; aesthetics
  are manual.
- **Run polling vs. streaming** — Phase 2 polls `GetWorkflowRun`. If run latency
  or fleet size makes polling costly, a server-stream/watch is a later
  optimization; the hook boundary (`use-workflow-run`) isolates the change.
- **Trigger config UI** — `ResourceEventTrigger`/`ScheduleTrigger` are currently
  empty messages in the proto; their config surface is undefined. Editor treats
  trigger as present/absent + kind for now; revisit when the proto fills in.
- **CEL authoring depth** — Phase 3 ships plain-field CEL with validation.
  Autocomplete over the run context is a follow-up; flag if users need it sooner.
- **`config` vs. `parameters`** — MANAGED workflows are configured via the
  container `config` (Struct) against the live version's parameter schema. A
  config editor for MANAGED workflows may be worth a small surface between Phase
  2 and 3 (separate from the structural editor). Confirm priority.
- **View transitions deferred** — `vercel-react-view-transitions` is out of scope.
  Verified July 2026: `<ViewTransition>`/`addTransitionType` are `react@canary`
  only; pinned `react@19.2.7` does not export them. Adopting would require moving
  the workspace to canary on top of the dual-React issue below. Revisit as
  isolated polish after Phase 1, only on an explicit decision to go canary.
- **Secret rotation mask is body-derived (verify live).** The REST surface has no
  `update_mask` query param; grpc-gateway derives the PATCH mask from body-field
  presence, so T6 rotates by including `value` in the PATCH body and omits it for
  metadata-only edits (verified statically against `internal/service/secrets/server.go`
  + the bare `runtime.NewServeMux`). NOT yet confirmed against a live PATCH —
  drive it when the stack is up (Phase 1 gate).
- **UI render tests — resolved.** T4 added `jsdom` + `@testing-library/react` as
  `@pivox/ui` devDeps (jsdom is already the repo standard — `apps/start` used it
  at HEAD), so component render tests now run (`@pivox/ui` canvas/run-status tests
  green). T6's resource-admin components predate this and are still validated via
  types + builds + pure-logic tests — worth backfilling render tests for them.
- **Phase 1 review — deferred nits** (non-blocking; sweep later): `ast-to-graph.ts`
  calls `activityConfig(step.activity)` twice per activity step — compute once
  (DRY). A Parallel-with-continuation emits BOTH a `sequence` edge and `join`
  edges to the continuation — spec-conformant per §5 and confirmed intended, but
  the container→continuation sequence edge is filtered out of every test
  assertion, so nothing positively pins it — add an assertion. An empty region
  sequence (e.g. a Condition branch with no steps) elk-sizes the region box 0×0 —
  RO-only aesthetic, relevant when §5's Phase-3 placeholder drop targets land.
  BE-1 `order_by` is silently ignored (documented, consistent with sibling
  services). None block Phase 1 close.
- **`error_sequence` round-trip is now wired (was a BUG).** The read/write
  persistence path landed: `marshalDefinition` (write) sets
  `ErrorSequence: in.GetErrorSequence()` (`internal/service/workflows/server.go`)
  and `WorkflowVersionToProto` (read) lifts `out.ErrorSequence =
  scratch.GetErrorSequence()` (`internal/convert/workflows.go:94`), so
  `error_sequence` now persists to `workflow_versions.definition` and re-emits on
  read alongside `parameters/trigger/root`. T3's "on error" canvas region has data
  to render. The runtime executor also honors it: the Worker Process's
  `RunWorkflowWorker` loads the pinned definition's `errorSequence` and passes it
  to `engine.Interpreter.Run`, which runs it via `runErrorSequence` on an uncaught
  terminal failure (`internal/engine/interpreter.go`). `error_sequence` is
  therefore functional end-to-end. Surfaced by SEED-1.
- **Dual React in the tree** — the workspace resolves both `react@19.2.7` (the
  apps) and `react@18.3.1` (transitive via `react-jsx-parser` ← `@pivox/primitives`).
  A past "two-React null dispatcher" crash is on record. The workflow UI adds no
  new React; just don't introduce a second copy, and treat any hook-dispatcher
  crash as a resolution problem, not app logic. Logged in §14.

---

## 13. Task breakdown for subagents

Ordered; boundaries chosen so agents work in parallel where deps allow. Each
task is TDD and ends at a phase/PR boundary with a code-review pass. Every task
is bound by the §3 conventions. Component tasks (T4, T5, T6, T11, T12, T13) must
apply `vercel-composition-patterns` + `vercel-react-best-practices`; pure-logic
tasks (T3, T10) are test-first pure functions with no React surface.

**Phase 1 — RO**

1. **T1 — Package scaffolding.** Create `@pivox/ui/workflow` and
   `@pivox/features/workflows` packages (exports, tsconfig, build) mirroring
   `chat`. Add `elkjs` via catalog. No behavior. *(blocks all)*
2. **T2 — API layer.** `features/workflows/api.ts`: typed `$api` query/mutation
   hooks for every RPC in [§2](#rpc-surface). Thin; typed against the generated
   spec. *(depends T1)*
3. **T3 — `ast-to-graph` + layout.** The transform + elk layout + id/path
   mapping, fully unit-tested. *(depends T1; the critical-path module)*
4. **T4 — Canvas + node renderers.** `WorkflowCanvas`, activity/container/branch
   nodes, read-only, with a static config side-panel. *(depends T3)*
5. **T5 — Definition + versions routes.** Workflows list + detail shell
   (Definition tab) + Versions tab (view any version on the canvas). *(depends
   T2, T4)*
6. **T6 — Connectors + Secrets CRUD.** List + create/edit/delete forms for both;
   Secrets is set-only (no value display; rotate via field mask; handle the
   delete-in-use `FAILED_PRECONDITION`). Shared form scaffolding across the two
   (DRY). *(depends T2)*

**Phase 2 — Run**

BE-1 (backend) can run in parallel with T7–T9; only the org/space-wide runs view
depends on it.

- **BE-1 — Org/space-wide run listing (backend).** `-` wildcard handling in
  `ListWorkflowRuns` + denormalize `org_id`/`space_id` onto `workflow_runs` +
  keyset indexes + backfill + org/space-scoped permission. TDD + `aip-reviewer`.
  See §4 BE-1. *(independent; gates the `/runs` view in T8)*
7. **T7 — Run monitor.** `use-workflow-run` polling + StepState merge +
   `run-status` overlays; extend the side panel with runtime output/error.
   *(depends T4, T2)*
8. **T8 — Runs views + run detail.** Per-workflow Runs tab + run detail route
   hosting the monitor; org/space-wide `/runs` list (via `workflows/-`).
   *(per-workflow depends T7; `/runs` also depends BE-1)*
9. **T9 — Manual run + cancel.** Param form from `ParameterDef`,
   RunWorkflow/CancelWorkflowRun. *(depends T2, T8)*

**Phase 3 — Edit**

10. **T10 — Edit ops + `graph-to-ast`.** Pure AST edit operations + validation,
    unit-tested. *(depends T3)*
11. **T11 — Editor state + palette.** `use-workflow-editor`, undo/redo, insertion
    UX on the canvas. *(depends T10, T4)*
12. **T12 — Inspectors.** Per-kind config panels + version-level (params/trigger/
    error_sequence) + CEL fields. *(depends T11)*
13. **T13 — Save/promote/fork/create flows.** CreateWorkflow → CreateWorkflowVersion
    → Promote; MANAGED fork guard. *(depends T11, T12, T2)*

Management: I coordinate, keep this doc's status in sync as tasks land, and run
reviews at two levels:

- **Per-task** — a `code-reviewer` agent over each task's diff at its boundary.
- **Per-phase (gate)** — a **full code review of the whole phase** before it's
  considered done: the complete phase diff reviewed together (cross-task
  integration, the AST↔graph transform, security on the connector/secret/BE-1
  paths, composition-skill conformance, test coverage), plus a build/lint/test
  run. A phase does not close — and the next phase does not start — until its
  full review passes and findings are resolved. BE-1 additionally gets an
  `aip-reviewer` pass.

---

## 14. Convention-violation ledger

Existing-code issues spotted while building the workflow UI, for a later cleanup
sweep. Log-only — do **not** fix inline (§3 rule 5). Append `file:line`,
the rule, and a one-line note as they're found.

| Location | Rule | Note |
|---|---|---|
| broad (est. ~50% of files, per owner) | §3.1 no decision-comments | Verbose "why we chose X" prose in source (e.g. `apps/start/src/lib/api-client.ts`, `apps/start/src/server/prefetch.ts`). Owner-confirmed sweep planned. |
| `web/packages/ui/src/chat/chat.tsx:256` | `react19-no-forwardref` | `forwardRef` under React 19; ref is a prop now. Lone holdout (shadcn primitives are clean). |
| workspace dep tree | dependency hygiene | Dual React (`19.2.7` + transitive `18.3.1` via `react-jsx-parser` ← `@pivox/primitives`). Prior null-dispatcher crash on record; resolve to a single React when `react-jsx-parser` is replaced/updated. |
| `web/packages/ui/package.json` `./auth` export | broken export | Points at `dist/esm/shared/auth-provider.js`, but `src/shared/auth-provider.ts` isn't a vite build `entry`, so only the `.d.ts` emits — the `.js` never builds; `publint --strict` on `@pivox/ui` fails on it. Pre-existing on HEAD. Fix: add the source to the ui vite `entry[]` (or an `auth.ts` barrel) / repoint the export. Found during T1. |
| ~15 list handlers (spaces, apikeys, aichat *, tags, requests, assets, operations, dashboards, org/space members, workflows/versions/runs) | keyset pagination off-by-one | Next-page token = `results[pageSize].ID` (first *un*returned row) while resume predicate is strict `id > cursor` → **one row silently skipped at every page boundary**. Existing pagination tests miss it (assert only `page2[0] != page1[0]`, never full coverage). Found during BE-1; fixed **only** in BE-1's new run-list code (`renderRunPage` uses last-returned row). Needs a codebase-wide fix + a shared full-coverage pagination test helper. |
| `workflow_runs.org_id/space_id` (`000001_init.up.sql`) | missing integrity constraint | Denormalized scope columns are set once at insert from the workflow and never reconciled; there's no DB `CHECK`/composite-FK tying `workflow_runs.space_id` to `workflows.space_id`. Fine today (scope is immutable), but a future write path that sets them inconsistently would silently misfile runs in the scope-wide listing the org rollup depends on. Raised by BE-1 aip-review. |
| `scripts/seeds/10_storage_gateways.sql:15` | broken seed test | Uses psql-only `\set public_host` meta-command (added by `4cf80eec`, Cloudflare-tunnel change); the Go seed-runner execs via pgx, which can't parse `\set` → `SQLSTATE 42601`. Breaks `TestSeed_AssetVersionsMatchAssetContentType` on the current tree, independent of workflow work. Found during BE-1. |
