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
- A **second, independent option** exists for the Next side of the BFF: instead
  of porting the hand-rolled `server/oidc/**` BFF as-is (which §4.1–4.7 assume),
  the Next app can delegate sign-in/callback/logout/session/cookie handling to
  **Auth.js v5** (a.k.a. NextAuth) and keep only the `/api/v1` proxy hand-written
  (§4.8). This decouples the two apps' auth stacks behind the existing
  `@pivox/features` `AuthContext` seam: **Electron stays on `@pivox/oidc`**
  (main-process PKCE), **Next runs on Auth.js**. It is presented as a
  technically-available alternative with its own tradeoffs, not a recommendation.

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

There are **two ways to land the Next side** of this, and §4.1–4.7 below describe
only the first:

- **(a) Port the hand-rolled BFF as-is** — move `server/oidc/**` (openid-client +
  the Postgres `web_sessions` store + single-flighted refresh) file-for-file into
  Next route handlers. This is what §4.1–4.7 quantify. It keeps behavior
  byte-identical and reuses `@pivox/oidc` on both apps.
- **(b) Replace the BFF's auth half with Auth.js v5** — delegate
  sign-in/callback/logout, the session, the session cookie, and refresh-token
  rotation to Auth.js (NextAuth), and keep **only** the `/api/v1` proxy
  hand-written. §4.8 describes this and the shared seam that lets Electron keep
  `@pivox/oidc` while Next runs on Auth.js. Auth.js is **not** an API gateway, so
  the proxy (§4.1) stays under either option; under (b) it reads the access token
  from Auth.js's `auth()` instead of the custom store.

The subsections below annotate, per piece, what the Auth.js variant changes.

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

**Auth.js variant (option b).** The proxy stays hand-written — Auth.js does not
proxy your API. What changes is only where the token comes from: instead of
`readSessionId(request)` → `getSession(id)` → `isTokenFresh`/`refreshSession`
against the Postgres store (`session.ts`, lines 79–96 of `$.ts`), the handler
calls `const session = await auth()` (the Auth.js helper — see §4.8) and reads
the access token off it. Refresh moves *into* Auth.js's `jwt` callback and fires
lazily on session read, so the explicit `isTokenFresh`/`refreshSession` block
collapses into the `auth()` call. The CSRF check, the `STRIP_*_HEADERS` sets, the
`%2e` guard, the `/v1/` re-check, and the streamed `fetch(target, { duplex })` are
untouched — they are proxy logic, independent of where the token is stored. The
one behavioral delta worth flagging: the current code single-flights refresh per
session id; Auth.js's `jwt`-callback refresh does not de-dupe concurrent reads
(§9).

### 4.2 OIDC handlers — `routes/auth/{sign-in,callback,logout}.ts`, `routes/auth/create-org.tsx`

Each is a server route handler that drives the Authorization Code + PKCE flow
via `@pivox/oidc` (`buildAuthorizationRequest`, `exchangeAuthorizationCode`,
`buildEndSessionUrl`) and the cookie/session helpers. All logic is Web-types +
`@pivox/oidc` calls. Port to `app/auth/sign-in/route.ts` etc. (Node runtime).
The origin-derivation logic in `server/oidc/client.ts` (`publicOrigin`,
`callbackUrl`, reading `x-forwarded-proto`/`x-forwarded-host`) is unchanged —
it already reads headers off the `Request`.

**Auth.js variant (option b).** These three handlers **disappear** — Auth.js
owns the Authorization Code + PKCE flow end to end. You register the Keycloak
provider and mount its catch-all route handler; the sign-in, provider callback,
and RP-initiated logout endpoints are generated by the library. Grounded in the
Auth.js v5 docs (surveyed 2026-07-16):

- **Provider** (`authjs.dev/getting-started/providers/keycloak`): import
  `Keycloak from "next-auth/providers/keycloak"` and add it to `providers: []`.
  It needs `AUTH_KEYCLOAK_ID`, `AUTH_KEYCLOAK_SECRET`, and `AUTH_KEYCLOAK_ISSUER`
  — and the docs state the **issuer must include the realm**, e.g.
  `https://…/realms/My_Realm`. That is exactly Pivox's acme issuer shape
  (`$PIVOX_PUBLIC_HOST/realms/acme`, per the root `CLAUDE.md`).
- **Config + route** (`authjs.dev/getting-started/installation`): an `auth.ts`
  at the app root calls `NextAuth({ providers: [Keycloak] })` and exports
  `{ handlers, auth, signIn, signOut }`; `app/api/auth/[...nextauth]/route.ts`
  re-exports `export const { GET, POST } = handlers`. A root `AUTH_SECRET`
  (Auth.js's own cookie-encryption secret, distinct from Keycloak's client
  secret) is required.
- **Callback URL** is fixed by the library at `/api/auth/callback/keycloak` (not
  the current `/auth/callback`). Keycloak's client `redirect_uri` allow-list and
  the ingress route table (`configs/agentgateway.yaml`) must be updated to that
  path. The bespoke `create-org.tsx` post-login step is **not** replaced by
  Auth.js and stays app-owned (drive it from a `signIn`/`events` callback or a
  post-callback redirect).
- **Logout**: `signOut()` clears the Auth.js session; RP-initiated end-session at
  Keycloak (what `buildEndSessionUrl` does today) is **not** automatic — you
  still trigger Keycloak's `end_session_endpoint` yourself (e.g. in the
  `signOut`/`events.signOut` path or a small logout route that redirects there).

### 4.3 `@pivox/oidc` — no changes

`web/packages/oidc/` is explicitly framework-agnostic: its only dependency is
`openid-client` (`package.json`), and its `index.ts` header states it is *"shared
by the start BFF (server-side…) and the Electron main process."* It exports pure
protocol mechanics (discovery/config provider, PKCE authorize, code exchange,
refresh, end-session, token freshness, id-token claim decode). **Zero changes.**

**Auth.js variant (option b).** Auth.js brings its own OIDC/OAuth engine
(built on `@panva/oauth4webapi`, the same author as `openid-client`), so on the
**Next** side `@pivox/oidc` is largely **superseded**: discovery, PKCE authorize,
code exchange, and the base refresh call are all done by Auth.js. What remains
potentially reusable are the *pure, transport-free* helpers —
`decodeIdTokenClaims` (`claims.ts`) and the `SessionTokens`/`isTokenFresh`
shapes (`tokens.ts`) — but even those overlap Auth.js's `profile`/`jwt` callback
outputs, so Next may end up not importing `@pivox/oidc` at all. **Electron still
consumes it in full** (§4.7, §6): the package's reason to exist does not go away,
its second consumer does. See the §6 table note.

### 4.4 `openid-client` under the Next server runtime

`openid-client` (`6.8.4`) is a standard Node library. It runs unchanged in
Next's **Node.js runtime**. The only hazard is the **Edge** runtime: Next
middleware defaults to Edge, and neither `openid-client` nor `postgres.js`
(the session store) run there. Mitigation is a one-line invariant: every route
handler / layout that touches OIDC or the session store sets
`export const runtime = 'nodejs'`, and the auth gate does its real session
resolve in a Node-runtime layout rather than in `proxy.ts`/middleware.

**Auth.js variant (option b).** Auth.js is designed to run its **core** (JWT
decrypt, cookie handling, the `auth()` session read) on the **Edge** runtime, and
the docs' `proxy.ts` recipe (`export { auth as proxy }`,
`authjs.dev/getting-started/installation`) assumes exactly that. But two things
still pull toward Node: (1) the **Keycloak refresh call** you write in the `jwt`
callback (§4.8) — a plain `fetch` to the token endpoint, edge-capable in
isolation but it runs wherever the session is resolved; and (2) any **database
adapter** if you pick the database-session strategy (§4.8), plus the `/api/v1`
proxy's streaming upstream `fetch(..., { duplex: 'half' })`. Net: the same
`export const runtime = 'nodejs'` invariant on the proxy and on any handler that
resolves the session is the safe default — Auth.js narrows the surface (no
`postgres.js` if you use JWT sessions) but does not remove the edge-vs-node
footgun (§9, risk 4).

### 4.5 Session store — `server/oidc/session-store.ts`

Postgres-backed (`postgres.js`), its own `sessions` DB, `web_sessions` table,
idempotent `ensureSchema`, lazy expiry, opaque-id sessions, single-flighted
refresh (`server/oidc/session.ts`, `refreshSession`). None of this is
framework-coupled — it is plain Node + SQL. It moves file-for-file. It needs the
Node runtime (§4.4). Note the module comment already flags the multi-replica
caveat (single-flight is per-process); Next changes nothing about that.

**Auth.js variant (option b).** This whole file is **replaced** by Auth.js's
session layer, and the shape depends on the chosen strategy
(`authjs.dev/concepts/session-strategies`, surveyed 2026-07-16):

- **JWT strategy (Auth.js default)** — "a JWT is created in a `HttpOnly` cookie…
  encrypted with a secret key only known to the server." The
  `access_token`/`refresh_token`/`expires_at` you stash in the `jwt` callback ride
  inside that encrypted cookie; there is **no `web_sessions` table and no
  `postgres.js`**. Caveats the docs name: the JWT "should not be assumed to be
  impossible to decrypt at some point," and there is "typically a limit of around
  4096 bytes per cookie" — the Keycloak access token (a signed JWT itself) plus
  refresh token must fit.
- **Database strategy** — "a session in your database… A session ID is then saved
  in a `HttpOnly` cookie" (an "obscure value pointing to the session"). Tokens go
  in Auth.js's adapter `account` table instead of the cookie, and per the
  refresh-rotation guide the refresh runs in the **`session`** callback (not the
  `jwt` callback) against that table. This is the closest analogue to today's
  opaque-id + Postgres design, but it uses Auth.js's **adapter schema** (its own
  `users`/`accounts`/`sessions` tables), not the current `web_sessions` shape —
  so it is a schema swap, not a lift-and-shift, and needs a DB adapter (Node).

Which strategy to pick is an open tradeoff (§9): JWT sessions remove the DB
dependency but put the Keycloak tokens in the cookie under the 4KB ceiling;
database sessions keep tokens server-side (matching today's posture) at the cost
of an adapter + a DB round-trip per `auth()`.

### 4.6 The auth gate — `lib/auth-gate.ts` + `getServerSession`

`requireKcSession` today branches on `typeof window` and either throws a
TanStack `redirect(...)` (SSR→302) or sets `window.location` (client). In Next
this collapses to a **server-only** concern: the `(app)` layout Server Component
calls the equivalent of `readServerSession()` (`server/oidc-session.server.ts`,
which is already a plain `() => Promise<ServerSessionStatus>` — deliberately
extracted from the `createServerFn` wrapper for testability) and calls
`redirect('/auth/sign-in?return=…')` when there is no user. The client-branch
half of the gate disappears because gating happens server-side before render.

**Auth.js variant (option b).** The gate reads the session through Auth.js's
`auth()` helper instead of `readServerSession()`. Per the docs
(`authjs.dev/getting-started/session-management/protecting`, surveyed
2026-07-16), `auth()` returns the session in Server Components and Route
Handlers (`const session = await auth()`), and route handlers can be wrapped
(`export const GET = auth((req) => { if (req.auth) … })`). The `(app)` layout
Server Component calls `auth()` and `redirect()`s to Auth.js's `signIn` entry
when there is no user; the docs also caution to verify the session "as close to
your data fetching as possible" rather than trusting a `proxy.ts`/middleware
pre-gate alone — which lines up with the §4.4 rule to resolve in the Node-runtime
layout, not the edge middleware.

### 4.7 Electron impact: none

Confirmed (§6): Electron has no BFF, no `createServerFn`, no `/api/v1` proxy. It
consumes `@pivox/oidc` in its **main process** and talks to the API with a
bearer token directly. Migrating `apps/start`'s BFF does not touch it.

### 4.8 Auth abstraction: Electron on `@pivox/oidc`, Next on Auth.js

The two apps do not have to share one auth implementation. There is already a
**platform-neutral seam** between the app shell / shared features and whatever
resolves the Keycloak session, and it can carry two different implementations
behind it:

**The existing seam.** `@pivox/features/auth` defines `AuthContext` /
`AuthContextValue` (`{ user: AuthUser | null; loading: boolean; signOut() }`) and
`useAuth()`/`useUserId()` over it (`packages/features/src/auth/use-auth.ts`,
`use-user-id.ts`). Each app supplies its own **provider** that maps its Keycloak
session into that shape:

- **Electron** — an IPC-backed provider whose `@pivox/oidc` main-process client
  (public PKCE, `safeStorage` tokens) decodes the id_token and pushes `AuthUser`
  over IPC (`web/apps/electron/src/main/**`; real `loading` phase during boot
  restore).
- **Web (today)** — `KeycloakAuthProvider` (`lib/kc-auth-provider.tsx`): no client
  auth SDK, `user` injected as a prop from the SSR gate, `loading` always
  `false`, `signOut()` = a same-origin form POST to `/auth/logout`.

The route gates use the same dependency-injection discipline: `AuthGateFeature` /
`OrgGateFeature` take an injected `NavigateComponent`
(`packages/features/src/auth-gate/auth-gate-feature.tsx`, `.../navigation`) so the
feature package stays router-agnostic — the consumer adapts *its* router's
redirect primitive. (This is the same decoupling §5/§6 track for full router
portability.)

**What each implementation must provide** behind the seam, regardless of library:

1. **Current identity** — an `AuthUser` (the Pivox identity id = Keycloak `sub`,
   plus email/displayName/photo) for `AuthContext`.
2. **An access token for the `/api/v1` proxy** — a *fresh* Keycloak access token
   the reverse proxy forwards as a Bearer (§4.1). This is the piece Auth.js must
   surface: it is not part of `AuthContextValue` (the browser never sees the
   token), it is read **server-side** by the proxy.
3. **Sign-in / sign-out redirect primitives** — start the Authorization Code +
   PKCE flow and the RP-initiated end-session, as full-document navigations.

**Two implementations behind that seam:**

- **Electron = `@pivox/oidc` (unchanged).** Main-process PKCE, `safeStorage`
  tokens, IPC provider. Nothing in this migration changes it; it keeps consuming
  `@pivox/oidc` in full (§4.3, §4.7).
- **Next = Auth.js.** The Keycloak provider + `auth.ts` config + the
  `[...nextauth]` route (§4.2) provide (1) and (3); the **`jwt`/`session`
  callbacks** provide (2). Grounded in `authjs.dev/guides/refresh-token-rotation`
  (surveyed 2026-07-16): Auth.js **does not auto-refresh provider access
  tokens** — you own it. On first sign-in the `jwt` callback stores the tokens
  off the `account`:

  ```ts
  async jwt({ token, account }) {
    if (account) {
      return { ...token,
        access_token: account.access_token,
        expires_at: account.expires_at,
        refresh_token: account.refresh_token }
    }
    if (Date.now() < token.expires_at * 1000) return token   // still fresh
    // expired → refresh at Keycloak's token_endpoint
    try {
      const res = await fetch(`${ISSUER}/protocol/openid-connect/token`, {
        method: 'POST',
        body: new URLSearchParams({
          client_id: process.env.AUTH_KEYCLOAK_ID!,
          client_secret: process.env.AUTH_KEYCLOAK_SECRET!,
          grant_type: 'refresh_token',
          refresh_token: token.refresh_token!,
        }),
      })
      const t = await res.json()
      return { ...token,
        access_token: t.access_token,
        expires_at: Math.floor(Date.now() / 1000 + t.expires_in),
        refresh_token: t.refresh_token ?? token.refresh_token }
    } catch {
      token.error = 'RefreshTokenError'; return token
    }
  }
  ```

  The `session` callback then exposes what the proxy needs
  (`session.access_token = token.access_token`, `session.error = token.error`),
  and the proxy reads it via `await auth()` (§4.1). On `RefreshTokenError` the
  app forces re-auth (the docs' pattern: `if (session?.error) signIn('keycloak')`).
  This maps Pivox's current *refresh-before-forward* behavior onto Auth.js's
  *refresh-on-session-read* — the proxy calling `auth()` triggers the lazy
  refresh. (Database-session strategy moves the same refresh into the `session`
  callback instead; §4.5.)

**What Auth.js replaces vs. what stays hand-written:**

| Piece (web) | Port-as-is (option a) | Auth.js (option b) |
|---|---|---|
| sign-in / callback / logout routes | move to Next handlers | **Auth.js** generates them (`[...nextauth]`); RP end-session still self-triggered (§4.2) |
| session store (`web_sessions` + `postgres.js`) | move file-for-file | **Auth.js** session (JWT cookie, or adapter DB) (§4.5) |
| session cookie + refresh rotation | move `session.ts` | **Auth.js** cookie + your `jwt`-callback refresh (above) |
| `/api/v1` reverse proxy | move file-for-file | **stays hand-written**; reads token from `auth()` (§4.1) |
| `@pivox/oidc` | reused on web + Electron | **Electron only**; web largely superseded (§4.3) |

**The tradeoff, stated neutrally.** Option (b) swaps bespoke-but-known BFF code
(the `server/oidc/**` tree — its single-flighted refresh, opaque-id store, and
CSRF/refresh behavior are already written and commented) for a **maintained
library plus a provider-refresh callback you own**. It removes the session-store,
cookie, and base-flow code; it adds an Auth.js learning curve, the hand-written
`jwt`-callback refresh (the new sharp edge — §9), the fixed
`/api/auth/callback/keycloak` URL to register, and a dependency on Auth.js's
release cadence. It is an **alternative to §4.1–4.7's assumption** that the
hand-rolled BFF ports as-is — not an addition on top of it.

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
| `@pivox/oidc` | None (only `openid-client`) | **None** under option a (reused on web + Electron). Under the **Auth.js option** (§4.8), **Next stops consuming it** — Auth.js's OIDC engine supersedes it; only pure helpers (`decodeIdTokenClaims`) are candidates for reuse, possibly none. **Electron still consumes it in full.** |
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

**Auth.js delta on phase 3 (§4.8).** Choosing the Auth.js option instead of
porting the hand-rolled BFF re-shapes phase 3, not the other phases. It **lowers**
the plumbing: the session store, the session cookie, and the base
sign-in/callback/logout flow stop being your code (Auth.js owns them), removing a
chunk of the port and its live-verification surface. It **adds** three items: the
provider-refresh `jwt` callback you own end-to-end (with the concurrency caveat —
§9), the Auth.js learning curve if the team is new to it, and small wiring changes
(the fixed `/api/auth/callback/keycloak` redirect URI to register in Keycloak +
the ingress, the `AUTH_SECRET`, the JWT-vs-DB-session decision). Net assessment:
roughly **neutral-to-slightly-lower on phase 3** (call it −0.5 to −1.5 eng-days if
the team knows Auth.js; roughly flat if learning it from cold), and it does **not**
change phases 1, 2, 4–7. The security-critical `/api/v1` proxy still needs the same
live re-verification (§4.1, §9 risk 1) under either option, so the highest-risk
work is not removed — only the auth-flow scaffolding around it shrinks.

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

8. **[Auth.js option only] Refresh-rotation in the `jwt` callback is the new
   sharp edge.** This risk replaces (does not add to) part of risk 1 when the
   Auth.js option (§4.8) is chosen. Auth.js does not auto-refresh provider
   tokens; the Keycloak refresh you write in the `jwt` callback fires
   **lazily on session read** and is **not single-flighted** across concurrent
   requests — whereas today's `refreshSession` single-flights per session id. If
   two in-flight `/api/v1` calls both resolve `auth()` on an expired token, both
   can POST the same one-time-use refresh token to Keycloak; Keycloak's
   **reuse-detection revokes the whole token family**, logging the user out. This
   is the same Keycloak concern named in risk 1, but the Auth.js default makes it
   *easier* to hit because the library gives you no built-in de-dupe. Mitigation
   is on you (a per-session single-flight/lock around the refresh, or the
   database-session strategy so rotation is serialized through one row) and must
   be verified live. The `RefreshTokenError`→`signIn()` recovery path
   (`authjs.dev/guides/refresh-token-rotation`) also needs testing so a failed
   refresh forces re-auth rather than looping. Impact: high (token-family
   revocation = forced logout). Likelihood: medium under concurrency.

9. **[Auth.js option only] Library-shaped constraints.** Auth.js fixes the
   callback URL at `/api/auth/callback/keycloak` (Keycloak client allow-list +
   ingress route must match), pins its own cookie/session encoding, and adds a
   dependency on its release cadence (`next-auth` v5 is published under a `beta`
   tag as of the survey). The JWT-session strategy also puts the Keycloak tokens
   in a cookie under the ~4KB ceiling (§4.5). None is a runtime-failure risk on
   its own; they are integration constraints to design around. Impact: low-medium.
   Likelihood: low.

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

### Auth-implementation fork (a variant crossing A and B)

Orthogonal to *when/how* you migrate (A vs B) is *what builds the Next auth half*
once you do. Whenever the Next app exists (under A, or the `web/apps/next` app
under B), its BFF auth layer has two forms, per §4.8:

- **a. Port the hand-rolled BFF** (`server/oidc/**` → Next handlers). Byte-for-byte
  behavior, `@pivox/oidc` reused on both apps, all the current code (and its
  single-flighted refresh) preserved.
- **b. Auth.js on the Next side, `@pivox/oidc` on Electron.** Auth.js owns
  sign-in/callback/logout/session/cookie/refresh behind the `AuthContext` seam;
  the `/api/v1` proxy stays hand-written and reads the token from `auth()`.
  Trades bespoke BFF code for a maintained library plus the `jwt`-callback refresh
  you own (§9, risks 8–9). Roughly neutral-to-slightly-lower on the phase-3
  estimate (§8).

This fork does not change the §8 total materially; it changes what the phase-3
BFF work *is*. Under option (b) the two apps deliberately run **different** auth
implementations behind a shared seam — Electron on `@pivox/oidc`, Next on
Auth.js — which is itself the reason §4.8 exists. Neither form is recommended
here.

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
- Auth abstraction (§4.8): `packages/features/src/auth/{use-auth.ts,use-user-id.ts,
  index.ts}` (the `AuthContext` seam), `packages/features/src/auth-gate/
  auth-gate-feature.tsx` (injected `NavigateComponent`), `apps/start/src/lib/
  kc-auth-provider.tsx`, `apps/start/src/lib/auth-gate.ts`,
  `apps/start/src/routes/api/v1/$.ts` (token-injection point), `packages/oidc/
  src/index.ts` (helper surface).
- External (2026-07-16): TanStack Start overview + v1 RC blog; Next.js releases +
  updates tracker + App Router docs.
- External — Auth.js v5 / NextAuth (all surveyed 2026-07-16):
  [Keycloak provider](https://authjs.dev/getting-started/providers/keycloak),
  [installation / App Router setup](https://authjs.dev/getting-started/installation),
  [refresh token rotation](https://authjs.dev/guides/refresh-token-rotation),
  [session strategies (JWT vs database)](https://authjs.dev/concepts/session-strategies),
  [protecting resources / `auth()`](https://authjs.dev/getting-started/session-management/protecting).
