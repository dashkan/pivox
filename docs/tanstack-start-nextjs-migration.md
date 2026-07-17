# TanStack Start → Next.js (App Router) migration analysis

Status: analysis / decision input. No code changed. Read-only survey of the
`web/apps/start` app and the shared packages it consumes, plus a
level-of-effort and risk estimate for porting it to the latest Next.js
(App Router, Next 16).

Scope note: this doc is **purely technical** — it describes, quantifies, and
maps. It does **not** recommend for or against migrating; that call is the
reader's.

Every structural claim below cites a file that was actually read. External
facts are date-stamped (surveyed 2026-07-16).

---

## 1. Summary at a glance

The web app is a pnpm monorepo (`web/`) with one TanStack Start app
(`web/apps/start`), one Electron app (`web/apps/electron`), and six shared
packages. The migration is almost entirely confined to `apps/start`: every one
of its ~31 source files is bound to a TanStack API (file routing, `beforeLoad`
loaders, `createServerFn`, or `validateSearch`), because that app **is** the
framework-binding layer. The shared packages are largely framework-agnostic and
port with little or no change — with one measured exception (`@pivox/features`
imports the TanStack router in 2 files; see §5).

What a migration entails, mechanically:

- Rewrite the routing layer: file-based TanStack routes → Next App Router
  `app/` tree (layouts, pages, route handlers).
- Re-home the BFF (the security-critical part): the `/api/v1/*` reverse proxy,
  the OIDC sign-in/callback/logout handlers, and the auth gate. The proxy and
  handlers are already written against Web `Request`/`Response`/`fetch`, which
  is exactly the Next Route Handler contract, so the bodies port at high
  fidelity; the wrappers and cookie/context accessors change.
- Re-express the SSR prefetch→hydrate pattern: today it is TanStack Router
  loader + `queryClient.setQueryData(sameKey)` + `setupRouterSsrQueryIntegration`;
  in Next it becomes a Server Component that prefetches into a per-request
  QueryClient and passes `dehydrate()` through `<HydrationBoundary>`. Query-key
  parity (the mechanism that guarantees a cache hit) is preserved either way.
- Accept a concrete DX downgrade: TanStack Router's end-to-end typed search
  params have no built-in Next equivalent (mitigable with `nuqs` + the existing
  zod-style parser; see §7).
- `@pivox/oidc`, `@pivox/client`, `@pivox/primitives`, `@pivox/ui`,
  `@pivox/storage` need no framework changes.
- The Electron app is **not** touched by the BFF migration — it has no BFF and
  no SSR (confirmed §6).

LOE estimate: **~15–25 engineer-days (≈ 3–5 calendar weeks for one dev)**.
Ranked risks in §9.

---

## 2. Framework status (surveyed 2026-07-16)

### TanStack Start

- **Release stage: Release Candidate.** The official overview page
  (`tanstack.com/start/latest/docs/framework/react/overview`, fetched
  2026-07-16) carries the banner: *"TanStack Start is currently in the Release
  Candidate stage! This means it is considered feature-complete and its API is
  considered stable… This does not mean it is bug-free or without issues."*
  Some third-party posts assert a stable v1.0 landed in March 2026, but the
  authoritative docs page still shows RC as of the survey date. v1.0 RC was
  announced 2025-09-22 (`tanstack.com/blog/announcing-tanstack-start-v1`).
- **Versions pinned in this repo** (`web/pnpm-workspace.yaml` catalog):
  - `@tanstack/react-start`: `1.168.28`
  - `@tanstack/react-router`: `1.170.18`
  - `@tanstack/react-router-ssr-query`: `1.167.1`
  - `@tanstack/router-plugin`: `1.168.20`
  - transitive server runtime: `nitro`: `3.0.260610-beta` (Nitro 3 is itself
    beta), on `vite`: `8.1.4`.
- **Distinction that matters for risk-reading:** TanStack **Router** (the
  file-routing + typed-navigation core) has been GA/stable v1 for a long time
  and is what the app leans on most heavily. TanStack **Start** (the SSR
  framework layer: `createServerFn`, the Nitro server, SSR streaming) is the
  RC/newer piece, and in this repo it is confined to the `server/` dir and the
  route wrappers.
- **Backing:** TanStack is an independent OSS org (Tanner Linsley et al.),
  funded by sponsorships and partners, not a single hyperscaler. React Server
  Components support is documented as in-progress, landing as a non-breaking
  1.x addition.

### Next.js

- **Latest stable: 16.2.x** (16.2.7 current on npm as of 2026-06-10 per
  release trackers; 16.2.6 published 2026-05-07). Next 16 became the stable
  major in October 2025.
- **App Router:** stable since 13.4, the default for new projects; Pages Router
  is in maintenance mode.
- **Defaults in 16:** Turbopack is the default bundler for dev and build;
  React 19.2; Cache Components (beta); async `params`/`searchParams`; middleware
  file renamed to `proxy.ts`; minimum Node 20.
- **Backing:** developed by Vercel; very large ecosystem and community.

Sources:
[TanStack Start overview](https://tanstack.com/start/latest/docs/framework/react/overview),
[TanStack Start v1 RC announcement](https://tanstack.com/blog/announcing-tanstack-start-v1),
[TanStack router releases](https://github.com/TanStack/router/releases),
[Next.js releases](https://github.com/vercel/next.js/releases),
[Next.js updates tracker](https://releasebot.io/updates/vercel/next-js),
[Next.js docs — App Router](https://nextjs.org/docs/app).

---

## 3. Feature-by-feature mapping

| TanStack Start capability (as used here) | Next.js App Router equivalent | Difficulty | Notes |
|---|---|---|---|
| File-based routing (`routes/**`, `createFileRoute`) — 14 route files | `app/**` folder routing (`page.tsx`, `layout.tsx`, `route.ts`) | Moderate | Mechanical but touches every route file. `$workflowId` → `[workflowId]`; `_app` layout group → `(app)/layout.tsx`; `__root.tsx` → `app/layout.tsx`. |
| Server route handlers (`server.handlers.GET/ANY`) — auth routes, api proxy | Route Handlers (`app/**/route.ts`, `export async function GET/POST`) | Trivial–Moderate | Same Web `Request`→`Response` contract. Must set `export const runtime = 'nodejs'` (openid-client + postgres.js). |
| `createServerFn` SSR-only RPC (`prefetch.ts`, `prefs.ts`, `oidc-session.ts`) | Direct calls in Server Components, or Server Actions (`'use server'`) | Moderate | The RSC boundary replaces the "compiler strips server code from client bundle" trick. `getCookie`→`cookies()`, `getRequest()`→`headers()`/`cookies()`. |
| SSR data loading (`beforeLoad`/`loader` + `queryClient.setQueryData`) | Server Component prefetch into a request-scoped QueryClient + `<HydrationBoundary state={dehydrate(qc)}>` | Hard | Biggest conceptual fork; §4/§8. Key parity preserved; the wiring (router-context QueryClient + `setupRouterSsrQueryIntegration`) is replaced. |
| Typed search params (`validateSearch`, `useSearch`, `loaderDeps`, typed `navigate`) | `searchParams` prop (server) + `useSearchParams()`/`useRouter()` (client); no built-in type safety | Hard (DX) | Real downgrade. Runtime parsing already lives in a portable helper; compile-time link safety is lost. Mitigate with `nuqs`. §7. |
| BFF reverse proxy (`routes/api/v1/$.ts`) | `app/api/v1/[...path]/route.ts` (Node runtime) | Moderate | High-fidelity port; `params._splat` → `params.path` (array → join `/`). §4. |
| Auth gate in `beforeLoad` (`requireKcSession`) | `(app)/layout.tsx` Server Component reads session + `redirect()`; optional `proxy.ts` cookie-presence pre-gate | Moderate | Middleware runs on Edge by default — do the real (Node) session resolve in the layout, not middleware. |
| Streaming SSR (Nitro) | Next streaming SSR (React 18/19 streaming, Suspense) | Trivial | Native. |
| Build: Vite + `@tanstack/router-plugin` + `@tailwindcss/vite` | Turbopack (Next 16 default) + Tailwind v4 PostCSS plugin | Moderate | Router plugin drops (Next owns routing). Tailwind moves from the Vite plugin to PostCSS. App-local Vite config (`vite.config.shared.js`, `vite-externalize-deps.js`, `vite-tsgo-dts.js`) is retired **for the app only** — shared packages keep their own Vite lib builds. |
| Font/asset `?url` imports (`__root.tsx` woff2, `styles.css?url`) | `next/font` (or a raw `<link>` in root layout) | Trivial–Moderate | `?url` import syntax is Vite-specific. |
| Pre-hydration inline boot script (`buildBootScript()` in `__root.tsx`) | Inline `<script>` in `app/layout.tsx` (before children) | Trivial | `@pivox/storage`'s `buildBootScript()` is framework-agnostic; just re-mount it. |
| Devtools (`@tanstack/react-devtools`, router devtools) | N/A (drop) or Next-native tooling | Trivial | Dev-only. |
| Deploy target: host process on `:3000` behind agentgateway | `next dev`/`next start` host process on `:3000` behind agentgateway | Trivial | Port-compatible swap; §10. |

---

## 4. The auth / BFF migration (highest-risk area)

The BFF is the crux. Today it lives in `web/apps/start/src/server/**` plus three
route files. Mapping each piece:

### 4.1 The reverse proxy — `routes/api/v1/$.ts`

Forwards `/api/v1/*` → `<PIVOX_API_URL>/v1/*`, injecting the Keycloak access
token as a Bearer, with transparent refresh-before-forward and 401 pass-through.
The handler is already written entirely against Web platform types
(`Request`, `Headers`, `new URL`, `fetch`, streamed `request.body` with
`duplex: 'half'`, `Response`) — see lines 66–137 of the file. That is precisely
the Next Route Handler contract.

Port target: `app/api/v1/[...path]/route.ts` with `export const runtime = 'nodejs'`
and an `ANY`-equivalent (`export async function GET/POST/PUT/PATCH/DELETE`, or a
shared handler). Changes:

- `createFileRoute('/api/v1/$')({ server: { handlers: { ANY } } })` wrapper →
  named method exports.
- `params._splat` → `params.path` (Next gives a `string[]` for `[...path]`;
  join with `/`). The existing `%2e` guard and the `/v1/` prefix re-check
  (lines 99–110) port verbatim.
- Everything else — the CSRF check (Origin + `Sec-Fetch-Site`, lines 73–78),
  the `STRIP_REQUEST_HEADERS`/`STRIP_RESPONSE_HEADERS` sets, the
  `isTokenFresh`→`refreshSession`→`deleteSession` path — is pure logic over Web
  types and moves unchanged.

Difficulty: Moderate. It reads as a near-copy, but it is the security boundary,
so it must be re-verified live (CSRF rejection, refresh rotation, 401 clearing
the cookie).

### 4.2 OIDC handlers — `routes/auth/{sign-in,callback,logout}.ts`, `routes/auth/create-org.tsx`

Each is a server route handler that drives the Authorization Code + PKCE flow
via `@pivox/oidc` (`buildAuthorizationRequest`, `exchangeAuthorizationCode`,
`buildEndSessionUrl`) and the cookie/session helpers. All logic is Web-types +
`@pivox/oidc` calls. Port to `app/auth/sign-in/route.ts` etc. (Node runtime).
The origin-derivation logic in `server/oidc/client.ts` (`publicOrigin`,
`callbackUrl`, reading `x-forwarded-proto`/`x-forwarded-host`) is unchanged —
it already reads headers off the `Request`.

### 4.3 `@pivox/oidc` — no changes

`web/packages/oidc/` is explicitly framework-agnostic: its only dependency is
`openid-client` (`package.json`), and its `index.ts` header states it is *"shared
by the start BFF (server-side…) and the Electron main process."* It exports pure
protocol mechanics (discovery/config provider, PKCE authorize, code exchange,
refresh, end-session, token freshness, id-token claim decode). **Zero changes.**

### 4.4 `openid-client` under the Next server runtime

`openid-client` (`6.8.4`) is a standard Node library. It runs unchanged in
Next's **Node.js runtime**. The only hazard is the **Edge** runtime: Next
middleware defaults to Edge, and neither `openid-client` nor `postgres.js`
(the session store) run there. Mitigation is a one-line invariant: every route
handler / layout that touches OIDC or the session store sets
`export const runtime = 'nodejs'`, and the auth gate does its real session
resolve in a Node-runtime layout rather than in `proxy.ts`/middleware.

### 4.5 Session store — `server/oidc/session-store.ts`

Postgres-backed (`postgres.js`), its own `sessions` DB, `web_sessions` table,
idempotent `ensureSchema`, lazy expiry, opaque-id sessions, single-flighted
refresh (`server/oidc/session.ts`, `refreshSession`). None of this is
framework-coupled — it is plain Node + SQL. It moves file-for-file. It needs the
Node runtime (§4.4). Note the module comment already flags the multi-replica
caveat (single-flight is per-process); Next changes nothing about that.

### 4.6 The auth gate — `lib/auth-gate.ts` + `getServerSession`

`requireKcSession` today branches on `typeof window` and either throws a
TanStack `redirect(...)` (SSR→302) or sets `window.location` (client). In Next
this collapses to a **server-only** concern: the `(app)` layout Server Component
calls the equivalent of `readServerSession()` (`server/oidc-session.server.ts`,
which is already a plain `() => Promise<ServerSessionStatus>` — deliberately
extracted from the `createServerFn` wrapper for testability) and calls
`redirect('/auth/sign-in?return=…')` when there is no user. The client-branch
half of the gate disappears because gating happens server-side before render.

### 4.7 Electron impact: none

Confirmed (§6): Electron has no BFF, no `createServerFn`, no `/api/v1` proxy. It
consumes `@pivox/oidc` in its **main process** and talks to the API with a
bearer token directly. Migrating `apps/start`'s BFF does not touch it.

---

## 5. SSR prefetch / hydration mapping

Today (`routes/_app.tsx` `beforeLoad`, `routes/_app/connectors/index.tsx`
`loader`, `server/prefetch.ts`, `router.tsx`):

1. On the SSR pass only (`typeof window === 'undefined'`), a `createServerFn`
   prefetch resolves the user's Keycloak access token via `getSsrAccessToken()`,
   builds a direct server API client via `createServerApiClient(token)`, and
   fetches (`prefetchOrgsForCurrentUser`, `prefetchSpacesForActiveOrg`,
   `prefetchConnectors`, `prefetchConnectorAgents`).
2. The loader calls `queryClient.setQueryData(queryKey, data)` using the **same
   key** the client `useQuery` reads — `$api.queryOptions('get', path, params)`
   is deterministic on `(method, path, params)`, so server and client agree
   byte-for-byte. That guarantees a cache hit on hydration → rows are in the
   server HTML → no skeleton flash.
3. `router.tsx` builds a **per-request** `QueryClient` (never a module
   singleton — the comment warns this would leak one user's cache into another's
   SSR) and `setupRouterSsrQueryIntegration` wires the dehydrate/hydrate
   boundary. Default `staleTime` 60s keeps SSR-primed data authoritative on cold
   load.

Next App Router analogue (keeping react-query on the client — the like-for-like
path):

1. A **Server Component** (the `(app)` layout, or a per-route server segment)
   does the same token-resolve + `createServerApiClient(token)` + fetch. The
   `createServerFn` wrappers are dropped — a Server Component is already
   server-only, so `server/prefetch.ts`'s bodies become plain async functions
   called directly. `getCookie(...)` → `cookies().get(...)` from `next/headers`;
   `getRequest()` → `headers()`.
2. Create a **request-scoped** `QueryClient` in the Server Component,
   `queryClient.setQueryData(queryKey, data)` (or `prefetchQuery`) with the
   **identical** `$api.queryOptions(...)` key — `@pivox/client` and its
   `queryOptions` are framework-agnostic, so the key generator is reused as-is —
   then render `<HydrationBoundary state={dehydrate(queryClient)}>` around the
   client subtree. This is the officially documented TanStack Query + Next App
   Router SSR pattern, and it preserves key parity.
3. Client components keep calling the exact same `$api.useQuery('get', path,
   params)` hooks (`lib/api-client.ts` is browser-only and unchanged: it points
   at `/api` → the proxy). They hydrate from the boundary instead of from the
   router-SSR-query integration.

What changes concretely: the QueryClient moves from router context to a
per-request Server Component; `setupRouterSsrQueryIntegration` is replaced by
`<HydrationBoundary>`; the `typeof window` SSR-only guard is replaced by the
RSC server/client split; `createServerFn` wrappers disappear. What stays: the
query keys, the direct server API client, the token resolution, the 60s
staleTime rationale, and the "prefetch failure returns null, client refetches"
degradation contract.

Alternative (not like-for-like): go full RSC and render the initial rows in
Server Components without react-query for these reads. Larger rewrite of the
feature components (they currently assume `useQuery`), and it fractures the
shared `@pivox/features` data hooks that Electron also uses. Not scoped into the
LOE below.

---

## 6. What's shared vs. what's rewritten

### Shared, unaffected (or near-unaffected)

| Package | Framework coupling | Migration change |
|---|---|---|
| `@pivox/oidc` | None (only `openid-client`) | **None.** |
| `@pivox/client` | None (openapi-fetch + openapi-react-query) | **None.** Query-key generator reused for hydration. |
| `@pivox/primitives` | None (shadcn + Base UI) | **None.** |
| `@pivox/ui` | Router-free by design (`docs/resource-list-ui.md`: *"knows no router or SSR"*) | **None.** |
| `@pivox/storage` | None; `buildBootScript()` is framework-agnostic | **None** (re-mount the boot script in the Next root layout). |
| `@pivox/observability` | Browser tracing, SSR-guarded | **None.** |
| `@pivox/features` | **Mostly** injected/DI; **but** imports `@tanstack/react-router` in 2 files | **Small change** — see below. |

**Measured boundary exception (correction to the stated invariant).** The task
brief says `@pivox/features` "MUST NOT import the router or react-query
directly." Verified against the code:

- react-query: **clean** — 0 files in `packages/features/src` import
  `@tanstack/react-query`.
- router: **2 files import it** —
  `packages/features/src/org-gate/org-gate-feature.tsx` and
  `packages/features/src/auth-gate/auth-gate-feature.tsx`, each doing
  `import { Navigate } from '@tanstack/react-router'`. `@tanstack/react-router`
  is declared as a `peerDependency` in `packages/features/package.json`.

`<Navigate to=… replace />` is TanStack-Router-specific; Next's analogue is
`redirect()` (server) or `useRouter().replace()` (client). So `@pivox/features`
is **not** fully router-portable today. Fix is small (§8, phase 6): inject a
`Navigate`/redirect primitive as a prop or via a tiny context, so the web app
supplies a Next implementation and Electron keeps supplying the TanStack one.

### Rewritten (concentrated in `apps/start`)

`web/apps/start/src` holds **31 source files** (excluding the generated
`routeTree.gen.ts`). Per-API inventory (grep counts, files):

| Start-specific API | Files |
|---|---|
| `@tanstack/react-router` (any import) | 18 |
| `createFileRoute` | 14 (every route) |
| `beforeLoad` | 8 |
| `@tanstack/react-start` (any) | 6 (`routeTree.gen.ts` + the 5 server files) |
| `react-start/server` (`getCookie`/`getRequest`) | 4 |
| `createServerFn` | 4 (`server/oidc-session.ts`, `prefetch.ts`, `prefs.ts`, and the ssr-token path) |
| `validateSearch` | 2 (`connectors/index.tsx`, `lib/connectors-search.ts`) |
| `createRootRouteWithContext` | 1 (`__root.tsx`) |
| `setupRouterSsrQueryIntegration` | 1 (`router.tsx`) |
| `useSearch` / `useNavigate` / `loaderDeps` | 1 each (connectors route) |

Route files touched (`createFileRoute`): `_app.tsx`, `_app/about.tsx`,
`_app/connectors/index.tsx`, `_app/image-editor.tsx`, `_app/index.tsx`,
`_app/secrets/index.tsx`, `_app/workflows/$workflowId.tsx`,
`_app/workflows/index.tsx`, `api/v1/$.ts`, `auth/callback.ts`,
`auth/create-org.tsx`, `auth/logout.ts`, `auth/sign-in.ts`, `launch.ts`.

Server files touched (whole `server/` dir): `oidc-session.server.ts`,
`oidc-session.ts`, `oidc/client.ts`, `oidc/session-store.ts`, `oidc/session.ts`,
`oidc/ssr-token.ts`, `pivox-server-api.ts`, `prefetch.ts`, `prefs.ts`.

Portable helpers already isolated from the framework (move with light rewiring,
not rewrite): `lib/connectors-search.ts` (`validateConnectorsSearch`,
`searchToValue`, `valueToSearch` — imports only `@pivox/ui/resource-admin`, no
router), `lib/connector-agents-query.ts` (no imports), `lib/api-client.ts`
(browser fetch client, no router), `server/pivox-server-api.ts` (openapi-fetch,
no router), and the entire `server/oidc/**` + `oidc-session.server.ts` bodies
(plain Node logic; only their `createServerFn`/`getCookie` wrappers are
TanStack).

Net: the rewrite is ~31 files, but a meaningful fraction is *rewiring* (swap the
wrapper/accessor, keep the body) rather than *reauthoring*.

---

## 7. Typed search params — the concrete DX downgrade

`routes/_app/connectors/index.tsx` stores **all** list state in the URL:
`validateSearch: validateConnectorsSearch`, `loaderDeps: ({ search }) => search`
(re-runs the SSR loader per query), `Route.useSearch()`, `Route.useNavigate({
search: valueToSearch(next) })`. TanStack Router gives this end-to-end type
safety: the search shape is inferred, `navigate({ search })` is type-checked
against every route, and `useSearch()` returns the typed object.

Next App Router has **no built-in equivalent**. Server side: the `searchParams`
prop is `Record<string, string | string[] | undefined>` (all strings, in Next
16 it's async). Client side: `useSearchParams()` returns a `URLSearchParams`
(untyped). Type-safe navigation across routes (the compile-time guarantee that a
link's search shape matches its target) is gone.

Mitigation, and why it is partial:

- The **runtime** parser already lives in a portable, framework-free helper:
  `validateConnectorsSearch` / `searchToValue` / `valueToSearch`
  (`lib/connectors-search.ts`) import nothing from the router. They move
  unchanged and still coerce raw params into `ConnectorsSearch`. So the
  *validation* is not lost.
- `nuqs` (type-safe search-param hooks for Next App Router) restores typed
  read/write of query state and integrates with the router. It does not restore
  the *cross-route* link type-safety TanStack gives, but it recovers most of the
  per-route ergonomics.
- The loader-re-runs-on-search-change behavior (`loaderDeps`) is replaced by
  Next re-rendering the server segment when `searchParams` change.

Residual loss: compile-time link safety across the app, and the single typed
`useSearch()` surface. Contained to routes that carry search state (today, the
connectors list).

---

## 8. LOE estimate (one developer)

Phases, with rough effort. Serial dependencies noted; several route ports
parallelize once the scaffold + BFF + hydration pattern exist.

| # | Phase | Effort (eng-days) | Serial/parallel |
|---|---|---|---|
| 1 | **Scaffold** Next 16 app in `web/apps/` — Next config, Turbopack, Tailwind v4 PostCSS, tsconfig, pnpm-workspace wiring, root layout + boot script, `next/font`. | 1–2 | Serial (blocks all) |
| 2 | **Routing skeleton** — `app/` tree: root layout, `(app)` layout, route folders, `not-found`, param folders (`[workflowId]`). No data yet. | 1–2 | Serial after 1 |
| 3 | **BFF** — `app/api/v1/[...path]/route.ts`; `auth/{sign-in,callback,logout,create-org}` handlers; Node-runtime invariant; session store + `server/oidc/**` moved; auth gate in `(app)` layout. Live-verify against Keycloak through the tunnel. | 3–5 | Serial after 2; highest risk |
| 4 | **SSR prefetch → HydrationBoundary** — per-request QueryClient in a Server Component; port `prefetch.ts`/`prefs.ts`/`ssr-token.ts` bodies; `cookies()`/`headers()`; verify key parity → no double-fetch. | 2–4 | Serial after 3 (needs token resolve) |
| 5 | **Per-route ports** — connectors (incl. search-param rewrite via nuqs + existing parser), workflows list + `[workflowId]`, secrets, about, index, image-editor. | 3–5 | Parallelizable across routes after 4 |
| 6 | **`@pivox/features` router de-coupling** — inject `Navigate`/redirect for the 2 gate files; keep Electron on TanStack Router. | 0.5–1 | Parallel with 5 |
| 7 | **Cutover + teardown** — remove `apps/start`, delete app-local Vite config/plugins/devtools, update `aspire/apphost.cs` (Vite app → Next app), `configs/agentgateway.yaml` (port-compatible), and docs. | 1–2 | Serial last |

**Total: ~15–25 engineer-days (≈ 3–5 calendar weeks).**

Parallelizable: the phase-5 route ports (each route is independent once phases
1–4 land) and phase 6. Serial spine: 1 → 2 → 3 → 4, then 7 at the end. The
critical path is the BFF (3) and the hydration pattern (4); those two determine
whether the estimate lands at the low or high end.

Not included in the range (would extend it): a full-RSC rewrite of feature data
hooks (§5 alternative); adopting Cache Components; any redesign beyond a
like-for-like port.

---

## 9. Risks & unknowns (ranked)

1. **BFF correctness under the new runtime (highest).** The proxy and OIDC
   handlers carry the security-critical logic — CSRF (Origin + `Sec-Fetch-Site`),
   single-flighted refresh-token rotation (Keycloak reuse-detection will revoke
   the token family if double-spent), cookie `Secure`-per-request, redirect-uri
   origin validation, `%2e`/path-escape guards. The code ports at high fidelity
   (already Web-standard types), but a subtle regression here is a login outage
   or a token-family revocation, and it must be re-verified live, not just unit
   tested. Impact: high. Likelihood: medium.

2. **react-query ↔ RSC hydration key parity.** The current guaranteed cache-hit
   depends on the server `setQueryData` key matching the client `useQuery` key
   exactly, plus a correctly-scoped per-request QueryClient. If the
   `<HydrationBoundary>`/`dehydrate` wiring or the query keys drift, the symptom
   is silent double-fetch (skeleton flash returns) or a hydration mismatch, and
   the per-request-vs-singleton QueryClient mistake reintroduces the cross-user
   cache-leak the current `router.tsx` comment explicitly warns against. Impact:
   medium-high. Likelihood: medium.

3. **Search-param type-safety loss (DX).** End-to-end typed search + typed
   cross-route navigation have no built-in Next equivalent; only per-route
   ergonomics are recoverable (nuqs + the existing parser). Ongoing DX cost, not
   a runtime failure. Impact: medium. Likelihood: certain (it is a known gap,
   not a risk of failure).

4. **Edge-vs-Node runtime footgun.** `openid-client` and `postgres.js` require
   the Node runtime; Next middleware defaults to Edge. A stray Edge import of an
   OIDC/session path bricks login in a way that may pass local checks and fail
   in a deployed edge context. Mitigated by an explicit `runtime = 'nodejs'`
   invariant, but easy to get wrong once. Impact: high (login outage).
   Likelihood: low-medium.

5. **Vite-only mechanics with no drop-in Next form.** App-local: `?url` asset
   imports (fonts, `styles.css?url`), the `@tailwindcss/vite` plugin, the
   `buildBootScript()` inline pre-hydration script, and the app's Vite config
   trio. Each has a Next equivalent (`next/font`, Tailwind PostCSS, inline
   `<script>` in root layout), so this is low-risk — and note the **shared
   packages keep their Vite lib builds untouched**; only the app's Vite config
   is retired. Impact: low. Likelihood: low.

6. **Dual-app shared-package churn.** The `@pivox/features` router de-coupling
   (the 2 `<Navigate>` files) must not regress Electron, which stays on TanStack
   Router. An injected-primitive approach isolates this, but it is a shared
   package both apps build against. Impact: low-medium. Likelihood: low.

7. **TanStack Start RC vs. what's actually used.** Start is RC (§2) with a beta
   Nitro 3 under it; the app's heaviest dependency, TanStack Router, is GA. A
   migration trades RC-Start for GA-Next but discards working, heavily-commented
   code and takes on risks 1–4 for weeks. This is a stability/maturity trade to
   weigh, not a code-failure risk. Impact: n/a (judgment input). Likelihood:
   n/a.

**Stakes-lowering factor (applies to all of the above):** this is net-new /
dev-mode. There is no live production traffic and **no data migration** — the
`web_sessions` table is BFF-owned and can be dropped/recreated at will
(`session-store.ts` self-creates its schema). A botched cutover costs dev time,
not user data or uptime. That materially lowers the blast radius of every risk
except #1's token-family-revocation, which is a live-Keycloak concern regardless
of app maturity.

---

## 10. Technically-available paths (neutral)

Three approaches exist; each is described with its LOE/tradeoffs. No path is
recommended here.

### A. Full migration now

Port `apps/start` to Next in one project, as phased in §8. **LOE: ~15–25
eng-days.** Tradeoff: single coherent cutover; the app runs entirely on GA-Next
afterward. Concentrates the risk (esp. the BFF) into one push; low
data-migration stakes because it is dev-mode.

### B. Incremental / parallel app

Because the shared packages are portable (§6), a second app (`web/apps/next`)
could be stood up alongside `apps/start`, sharing `@pivox/oidc`,
`@pivox/client`, `@pivox/features`, `@pivox/ui`, `@pivox/primitives`,
`@pivox/storage`, and ported route-by-route. **LOE: same total as A plus
overhead** — during the transition you maintain **two** BFFs (two `/api/v1`
proxies, two OIDC handler sets) and two sets of routes, and route both behind
the ingress. Tradeoff: lower blast radius per step, longer wall-clock, duplicate
BFF maintenance while both exist. Note the two apps cannot share a single
running server process; this is parallel apps sharing source packages, not a
hybrid.

### C. Adapter-wrapping to lower future migration cost (stay on Start)

Leave the app on TanStack Start and reduce the future port cost by isolating the
Start-specific touchpoints further. Most of the isolation already exists (the
`server/` dir, the `createServerFn` stubs over plain-function bodies, the
router-free `lib/connectors-search.ts` parser, the framework-agnostic
`@pivox/oidc`/`@pivox/client`). The remaining concrete item is the measured
`@pivox/features` router leak (§5/§6): injecting the `Navigate`/redirect
primitive makes `@pivox/features` fully router-portable. **LOE: ~0.5–1 eng-day**
for the features de-coupling; near-zero for the rest since it is already
isolated. Tradeoff: no Next benefits now; keeps the app on RC-Start; makes a
later A or B cheaper and keeps the shared packages migration-clean regardless of
what is decided.

---

## Appendix — files read for this analysis

- App: `web/apps/start/package.json`, `web/apps/start/src/**` (routes, `server/`,
  `lib/`, `router.tsx`), full listing in §6.
- BFF/auth: `routes/api/v1/$.ts`, `routes/auth/{sign-in,callback,logout}.ts`,
  `lib/auth-gate.ts`, `lib/kc-auth-provider.tsx`, `server/oidc/{client,session,
  session-store,ssr-token}.ts`, `server/oidc-session{,.server}.ts`,
  `server/pivox-server-api.ts`.
- Prefetch/SSR: `server/prefetch.ts`, `server/prefs.ts`, `routes/_app.tsx`,
  `routes/_app/connectors/index.tsx`, `routes/__root.tsx`, `router.tsx`,
  `lib/connectors-search.ts`, `lib/api-client.ts`.
- Packages: `web/packages/oidc/{package.json,src/index.ts}`,
  `web/packages/features/{package.json,src/**}` (boundary grep + the 2 gate
  files).
- Electron confirmation: `web/apps/electron/{package.json,src/**}` (grep).
- Config/versions: `web/pnpm-workspace.yaml` (catalog), `configs/agentgateway.yaml`
  (web backend ref).
- House style reference: `docs/resource-list-ui.md`.
- External (2026-07-16): TanStack Start overview + v1 RC blog; Next.js releases +
  updates tracker + App Router docs.
