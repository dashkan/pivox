# Pivox — agent instructions

This is the root agent doc. It orients you to the repo, captures
cross-cutting rules, and points you at per-stack conventions.

## Components

Refer to components by their canonical names, not their tech stack:

- **Cloud Controller** — SaaS management layer. Source of truth.
  Hosts the public gRPC API + REST gateway, owns Postgres
  persistence, integrates Keycloak (OIDC) auth. Pure RPC — no
  background workers (those run in the Worker Process).
- **Worker Process** — background work runner paired with Cloud
  Controller. Hosts every periodic job (org/space purge, domain
  verification, LRO reaping, auth-artifact cleanup) via River
  queue. Multi-replica safe via River's leader election; scales
  independently of Cloud Controller.
- **Engine** — playout engine. Compositor, plugins (CEF, Rive,
  FFmpeg), output adapters.
- **Storage Agent** — on-prem agent for asset storage, paired with
  Cloud Controller.
- **Playout Agent** — on-prem agent installed alongside engines.

> **Auth model.** Auth is **Keycloak-only**. Identity provisioning flows
> from Keycloak (KC→Kafka→Pivox event sync). Both web apps are Keycloak:
> `start` via a server-side BFF (openid-client + httpOnly cookie
> session), and the Electron app via a public PKCE client whose OIDC
> flow runs in the **main process** (system-browser Authorization Code +
> PKCE, loopback/`pivox://` redirect capture, tokens in `safeStorage`) —
> see `web/apps/electron/src/main/`. The shared OIDC core (discovery,
> PKCE, code exchange, refresh, end-session, id-token claims) lives in
> `@pivox/oidc`, consumed by both the `start` server and the Electron
> main process.

Tech-stack references ("Go backend", "Rust engine") are appropriate
in build docs / architecture decisions where the technology is the
point. Avoid them in user-facing copy and in conversation about
features.

## Rate limiting

Cloud Controller does **not** implement app-level per-IP rate limiting.
Rate limiting is the edge proxy's responsibility (Cloudflare, GCLB,
nginx — whatever sits in front of pivox-cloud in production). App-level
abuse defenses live in:

- Auth on every endpoint that can be authenticated
- Keycloak brokering the SSO/OAuth flow to customer IdPs — KC owns the
  OAuth state token + PKCE now; the app-level custom broker was removed

If pivox-cloud is ever deployed without an edge proxy (small
self-hosted, dev), put `nginx` / `caddy` in front for volumetric
defense — there is no `--rate-limit-enabled` flag.

## Repository layout

Top-level directories you'll touch most:

```
cmd/                  Go binary entrypoints
  pivox-cloud/        Cloud Controller server (gRPC + REST, pure RPC)
  pivox-worker/       Worker Process — River-backed periodic jobs +
                      future on-demand LRO handlers (no gRPC)
  pivox-agent/        Storage Agent binary
  encrypt-sso-secret/ One-shot operator tool: KMS-encrypt SsoConfig client_secret
  gen-permissions/    Codegen: regenerates permission catalog Go
  gen-permission-registry/  Codegen: regenerates RPC→permission registry

internal/             Go application code (Cloud Controller + Storage Agent)
  AGENTS.md           Go conventions — required reading for backend work
  apierr/             gRPC status-error builders. Always go through this.
  appkey/             Codec for opaque resource-name encoding (HMAC-signed).
  audit/              Identity → Actor resolver with in-process LRU cache.
  authn/              Auth abstraction (Identity, Service interface), identity tokens.
  oidc/               Keycloak/OIDC access-token verifier (JWKS-backed).
  config/             Server config structs (CLI flags hydrate these).
  convert/            Proto ↔ DB row conversion helpers.
  crypto/             Encryptor interface, KMS implementation, NoOp passthrough for tests.
  db/                 Postgres data access
    queries/          Hand-written sqlc queries (*.sql)
    migrations/       golang-migrate files (000001_init.up.sql is THE source)
    generated/        sqlc output — do not edit
  filter/             AIP-160 filter parser + DB-query lowering.
  lro/                Long-running operation manager (AIP-151).
  permission/         IAM permission catalog + resolver.
  pkg/gen/            Generated proto code (do not edit).
  resource/           AIP resource-name parser/formatter.
  server/             gRPC interceptor chain + internal HTTP hooks.
  service/            One package per gRPC service:
    organizations/    Organizations + IAM Member CRUD at org scope
    spaces/           Spaces + IAM Member CRUD at space scope
    iam/              Cross-cutting IAM (DeleteUser LRO, role/permission reads)
    apikeys/          API key issuance + revocation
    aichat/           AI chat (StreamGenerateContent)
    assets/           Asset CRUD
    requests/         Asset request workflow
    tags/             Tag keys/values/bindings
    storage/          Storage gateways + endpoints + agent service
    operations/       LRO surface (AIP-151)
  storageagent/       Storage Agent runtime (HTTP serve + agent stream)
  testutil/           Test fixtures + grpc harness + mocks
  workers/            Background workers (purge, space-purge, verify-domain)

api/                  Proto definitions (source of truth for gRPC API)
proto/ buf.yaml ...   buf config + dependencies

aspire/               Aspire AppHost — preferred local dev orchestration
  apphost.cs          Resource graph (source of truth)
configs/agentgateway.yaml  L7 ingress config (agentgateway)
aspire/api-docs/      nginx container serving the Scalar API reference
configs/keycloak/     Keycloak realm import (acme) for the dev IDP
scripts/seeds/        Dev DB seed data (psql -f)
docs/                 Architecture + feature docs (read before designing)
tools/                Pinned dev-tool versions as a separate Go module
                      (Go 1.24+ `tool` directive). Houses buf,
                      golangci-lint, api-linter, sqlc, protoc plugins,
                      etc. — invoked via `go tool -modfile=./tools/go.mod`.
                      Makefile wraps these as `$(TOOL) <name>` so callers
                      don't type the long form.
```

## Per-stack docs

The dominant stack is Go (Cloud Controller). Go conventions are
non-trivial — read **`internal/AGENTS.md`** before touching backend
code. It covers:

- Database transactions (load-bearing rule — handlers that touch
  multiple statements MUST run in a transaction via
  `db.RunInTx`).
- Constructor pattern (`Config` struct + panic-on-required).
- Error handling (`apierr` package — never `status.Error` directly).
- Audit fields + identity Actor resolution.
- Permission model + interceptor chain.
- AIP resource naming.
- sqlc workflow.

## Build + run + test

### Go (Cloud Controller + Worker Process + Storage Agent)

Single build mode — there is no `-tags dev` variant. Local
development uses production binaries pointed at a local Postgres
and a local Keycloak (the dev IDP). Tests run against the shared
docker-compose stack defined in `docker-compose.test.yml`.

```sh
# Build / run
make build                              # bin/pivox-cloud + bin/pivox-worker + bin/pivox-agent
make run-server                         # go run ./cmd/pivox-cloud serve
make run-worker                         # go run ./cmd/pivox-worker
make run-agent                          # go run ./cmd/pivox-agent storage

# Hot reload (install `air` separately — not pinned in tools/go.mod
# due to a transitive dep conflict with api-linter)
make air                                # configs/air.toml
make air-worker                         # configs/air.worker.toml

# Tests — `make test` brings up the docker-compose stack and runs
# the suite. Real Postgres + rustfs; per-test isolation via
# template-clone DBs and per-test buckets.
make test                               # go test against shared stack
make test-up / test-down                # manage compose stack manually
go test -race ./internal/<pkg>/...      # race-detector for concurrency code

# Lint / format
make lint                               # golangci-lint (via tools/go.mod)
make lint-fix                           # auto-fix lints
make fmt                                # gofmt -w .
make tidy                               # go mod tidy + tools mod tidy

# Database
make db-up                              # apply migrations
make db-down                            # roll back one migration
make db-seed                            # apply scripts/seed.sql
make db-clear                           # truncate all tables (keeps schema)
make db-drop / db-create                # drop / create the database
make db-force VERSION=N                 # force migration version (recovery)

# Proto + codegen
make proto-generate                     # full chain: proto → Go + sqlc
make proto-generate-go                  # buf generate (Go)
make lint-proto                         # buf lint
make api-lint                           # AIP api-linter
make proto-format                       # buf format -w

# Docker (local pg + adminer)
make docker-up / docker-down
```

All `make` targets that invoke pinned tools route through
`$(TOOL) = go tool -modfile=./tools/go.mod`. That uses Go 1.24+'s
`tool` directive to pin tool versions in a separate module
(`tools/go.mod`) without polluting the main module's deps. New tools
get added by editing `tools/go.mod`'s `tool (...)` block + running
`cd tools && go mod tidy`.

Go binaries always go to `bin/`. **Never** bare `go build`
(deposits a binary in repo root). Use `make build` or
`go build -o bin/<name> ./cmd/<name>`.

### Aspire dev environment (`aspire/`)

The preferred local dev loop is the **Aspire AppHost** in `aspire/`,
which orchestrates the entire stack in one process tree: Postgres
(pgvector), one-shot migrate + first-use seed, rustfs, Keycloak, the
cloud API + Worker Process, the agentgateway ingress, an nginx
api-docs container, and the TanStack Start dev server (the public
Cloudflare tunnel runs as a host service, not an Aspire resource).

**Aspire is the ONLY local dev loop.** The old multi-process `make dev`
(a host envoy + the Go/web watchers, run under `concurrently`) was
removed along with envoy itself — there is no `make dev`, no `proxy-*`
target, and no second ingress config to keep in sync. `docker-compose.yaml`
still exists and `make docker-up` still works, but it brings up bare
postgres/rustfs/keycloak with **no ingress** — it is not a dev loop on its
own. (`make test` uses the separate `docker-compose.test.yml`; unaffected.)

Read the **`aspire` skill** first (`.claude/skills/aspire`) — it's the
router for all AppHost operations. Prereqs: .NET 10 SDK + the Aspire
CLI (`curl -sSL https://aspire.dev/install.sh | bash`).

```sh
cd aspire
aspire start                  # bring up the whole stack (run in the direnv shell)
aspire stop
aspire logs <resource>        # api | worker | agentgateway | keycloak | start | ...
aspire describe               # resource table: state, health, endpoint URLs
dotnet build apphost.cs       # typecheck the single-file C# apphost
```

**Always** `dotnet build apphost.cs` (typecheck) after editing `apphost.cs`.
Use `aspire logs`/`aspire describe` to diagnose — don't guess.

Conventions and gotchas (each was paid for once; don't relearn them):

- **`apphost.cs` is the source of truth** — a single-file C# AppHost;
  the `#:sdk` / `#:package` directives at the top pin the Aspire
  packages, and `.data/` + the dotnet build artifacts are gitignored.
  New integrations are added with `aspire add <name>`, which appends the
  corresponding `#:package Aspire.Hosting.*` directive (CommunityToolkit
  packages are added the same way — everything is a C# package ref now).
- **Go apps (`AddGoApp`) and the Vite app run as host processes**, not
  containers — they bind their own ports on the host. Postgres, rustfs,
  Keycloak, agentgateway and api-docs are containers.
- **Containers reach host processes via `host.docker.internal`** (the
  agentgateway container fronts the host-bound cloud/worker/agent/web).
  On Docker Desktop this resolves to the host incl. loopback. Sibling
  CONTAINERS are reached by network alias instead (`api-docs:80`,
  `otel-collector:4318`, `api-docs:80`).
- **`PIVOX_DATABASE_URL` must be a libpq URL** (`postgres://…`), not
  Aspire's default Npgsql keyword string — pgx can't parse the latter.
  Built in `apphost.cs` from the Postgres endpoint + parameters.
- **Postgres uses `pgvector/pgvector:pg18`** with the bind mount at
  `/var/lib/postgresql` (the pg18 parent-dir convention), not
  `/var/lib/postgresql/data`.
- **Non-container endpoints pinned with `port == targetPort` need
  `isProxied: false`** (e.g. the Vite app on `:3000`) — Aspire can't
  proxy them otherwise.
- **Container env is NOT inherited from direnv `.envrc`** (host
  processes are). Forward what containers need explicitly via
  `WithEnvironment` (e.g. `PIVOX_HOSTNAME` from `Environment.GetEnvironmentVariable`).
- **`configs/agentgateway.yaml` is STATIC** — mounted as-is, no apphost
  codegen. It shell-expands its own `${...}` cert placeholders, and
  filters its own noisy ingress spans via a CEL `filter`, so there is
  nothing to generate. **Read its header before editing** — four
  non-obvious behaviours are documented there, each of which cost a
  debugging session: route precedence is not list order; `pathPrefix` is
  segment-aware; backend refs need a LEADING SLASH (`backend: /web_app`
  — a bare name validates, starts, then 500s every request); and the
  upstream `Host` + `x-forwarded-proto` are NOT what you'd expect
  (`Host` is rewritten to the backend unless you say otherwise, and
  X-Forwarded-* is never injected).
- **`--validate-only` is necessary but NOT sufficient.** It parses the
  config and compiles every CEL expression, but it does not resolve
  backend references — a config can validate cleanly and then fail every
  request at runtime. Boot it and curl a route.
- **Keycloak serves at the root path** (not `/keycloak`) — the ingress
  proxies `/realms/` + `/resources/` for the browser SSO login; the
  admin console is reached directly on `:8082`, not through the ingress
  (same as agentgateway's own admin UI, on `:15000/ui`). The acme SSO
  issuer is
  `$PIVOX_PUBLIC_HOST/realms/acme`. This issuer lives in **two**
  places that must agree: `scripts/seeds/02_acme_sso.sql` and the live
  `sso_configs.oidc_config` row (re-seed an empty DB, or `UPDATE` in
  place — the seed only runs on first use).
- **Public tunnel**: a Cloudflare Tunnel (`cloudflared`, config in
  `~/.cloudflared/config.yml`) fronts the public host (`PIVOX_PUBLIC_HOST`)
  → the ingress on `localhost:8081`. Start it with `cloudflared tunnel run`
  (only one tunnel is configured, so it needs no name). It's independent of
  the Aspire lifecycle; nothing to start/stop in the AppHost.

  Do NOT use `brew services start cloudflared`. The Homebrew plist invokes
  `cloudflared` with no arguments, so it prints "use `cloudflared tunnel
  run`" and exits immediately, leaving the service in `error` state — the
  tunnel silently never comes up. Everything behind `PIVOX_PUBLIC_HOST` then
  returns Cloudflare **530 (origin unreachable)**, which does NOT look like a
  tunnel problem at the call site: an error page carries no
  `Access-Control-Allow-Origin`, so the browser reports it as a **CORS**
  failure, and the desktop app's OIDC login reports `discovery_failed`. If
  you see 530, check the tunnel before you touch `configs/agentgateway.yaml`.

## Skills (`.agents/skills/golang-*`)

The repo carries a standardized set of Go skills under
`.agents/skills/`. **Skills are not optional reference material —
applicable skills MUST be invoked on the first pass.** The cost of
skipping an applicable skill is paid in cleanup commits, audit
findings, and re-review cycles. Don't make the user clean up what
the skill would have caught.

For every skill in the table below:

- **Applies?** = Yes / No, based on what the codebase actually uses
  today (libraries imported, patterns in use, surface area). When
  the answer changes (we adopt or drop a library, add or remove a
  feature), update this table **in the same commit** that triggered
  the change.
- **Use on review?** = ALWAYS / SOMETIMES / NEVER. ALWAYS = invoke
  on every change that touches the relevant code path; SOMETIMES =
  invoke when the trigger column matches the change; NEVER = the
  skill exists but the codebase doesn't use the underlying tech.
- **Trigger** = the concrete signal that says "use this now." Be
  literal. "When writing a new DB query" beats "as needed."

When in doubt, invoke the skill. The skill itself decides whether
its guidance applies; the cost of a no-op skill load is far smaller
than the cost of missing guidance that would have caught the bug.

### Skill-loading mechanics

Skills live at `.agents/skills/<name>/` and are symlinked into
`.claude/skills/<name>/` so any AI tool that reads the `.claude/`
directory picks them up. The symlinks are a one-time setup; verify
with `ls -la .claude/skills/ | grep golang- | wc -l` (should match
the count of golang-* directories in `.agents/skills/`).

**The skill registry is computed at session start.** Adding a new
skill mid-session (or symlinking a previously-untracked one) means
the current session won't see it via the Skill tool — you'll get
"Unknown skill: <name>" even though the file exists on disk.
Subagents inherit the parent's registry, so the same caching applies
to them. Workarounds:

- For a fresh skill addition: restart the Claude Code session.
- For a one-shot review where restarting isn't worth it: load the
  skill content directly with `Read .agents/skills/<name>/SKILL.md`
  in the subagent prompt and ask it to apply that guidance.
- When invoking a subagent for code review, **explicitly enumerate
  the skills to load** in the prompt. Subagents don't auto-invoke
  skills based on AGENTS.md context alone; the table is reference
  material, not a dispatcher. Pass the list of skill names and the
  reason each applies, and tell the subagent to report skill loads
  that fail (so silent fallback is detectable).

| Skill | Applies? | Use on review? | Trigger |
|---|---|---|---|
| `golang-benchmark` | Yes | SOMETIMES | Adding a benchmark; investigating a measured perf regression; before optimizing a hot path. |
| `golang-cli` | Yes | SOMETIMES | Touching anything in `cmd/` (cobra/viper config, flags, exit codes, signal handling, completion). |
| `golang-code-style` | Yes | ALWAYS | Any Go change. Names, comments, package layout, gofmt-equivalent decisions. |
| `golang-concurrency` | Yes | ALWAYS for any goroutine/channel/lock/sync code | Touching `internal/audit/` (LRU + atomics), `internal/lro/` (workers + cancellation), `internal/storageagent/`, agent stream code, anything spawning goroutines, anything with `sync.*`, channels, or `errgroup`. |
| `golang-context` | Yes | ALWAYS | Any handler, any RPC, any goroutine — i.e. nearly every Go change. Cancellation, deadlines, value propagation. |
| `golang-continuous-integration` | Sometimes | SOMETIMES | Editing GitHub Actions workflows, adding linters/scanners to CI, release pipeline changes. Not for code-only changes. |
| `golang-data-structures` | Yes | SOMETIMES | Choosing slice vs map vs container; preallocating capacity; using `unsafe.Pointer` / `weak.Pointer`; building a generic container. |
| `golang-database` | Yes | **ALWAYS** for any DB-touching change | Writing/editing a sqlc query in `internal/db/queries/`; touching any handler that calls `s.queries.*`; transactions; isolation levels; SELECT FOR UPDATE; pgx pool config; migration. **This is the skill that catches the bugs that drove issue #13.** |
| `golang-dependency-injection` | No | NEVER | Pivox uses Config-struct + panic-on-required constructors deliberately. We do not run a DI container. Don't introduce one. |
| `golang-dependency-management` | Yes | SOMETIMES | Adding/upgrading a dep in `go.mod` or `tools/go.mod`; security advisory; resolving a version conflict. |
| `golang-design-patterns` | Yes | SOMETIMES | Designing a new constructor, choosing functional options vs builder, setting up graceful shutdown, picking a resilience pattern. |
| `golang-documentation` | Yes | ALWAYS for new exported APIs | Writing godoc on exported types/funcs; touching example tests; updating package-level comments. |
| `golang-error-handling` | Yes | **ALWAYS** | Any new error path. We use `internal/apierr` — never `status.Error` directly. Wrap with `%w`, use `errors.Is/As`, surface structured details. |
| `golang-graphql` | No | NEVER | No GraphQL surface. We're gRPC + grpc-gateway. |
| `golang-grpc` | Yes | **ALWAYS** for service handlers | Any change in `internal/service/*/`; proto changes; interceptor work in `internal/server/`; status code mapping; streaming RPCs; bufconn tests. |
| `golang-lint` | Yes | SOMETIMES | Editing `.golangci.yml`; suppressing a warning with nolint; new linter rollouts. |
| `golang-modernize` | Yes | ALWAYS | Trigger on any Go change. Catches old-style patterns (e.g. `interface{}` → `any`, manual loops → `slices`/`maps`/`cmp`). Cheaper to fix on first write than in a sweep later. |
| `golang-naming` | Yes | ALWAYS for new types/funcs | Naming a new package, type, constructor, error, boolean, receiver, test. Catches `utils`/`helpers`-style anti-patterns. |
| `golang-observability` | Yes | ALWAYS for any logging/metrics | We use `log/slog` everywhere; future Prometheus/OTel work lands here. Apply when adding logs, picking log levels, structuring fields, or instrumenting a new feature. |
| `golang-performance` | Yes | SOMETIMES | After a benchmark or profile identifies a bottleneck. Not for speculative optimization. |
| `golang-popular-libraries` | Yes | SOMETIMES | Picking a new library; comparing alternatives. Always check if the codebase already standardizes on something before pulling a new dep. |
| `golang-project-layout` | Yes | NEVER (the layout is settled) | Only on a structural reorg of `cmd/` or `internal/`. The current layout is documented above and should not drift. |
| `golang-safety` | Yes | **ALWAYS** | Nil safety, append aliasing, map concurrent access, `defer` in loops, numeric conversions, zero-value design. Catches a class of bugs that don't surface in tests. |
| `golang-samber-do` | No | NEVER | Not adopted. Don't add it. |
| `golang-samber-hot` | No | NEVER | We have a hand-written LRU in `internal/audit/`. Don't replace without explicit ask. |
| `golang-samber-lo` | No | NEVER | Not imported. Use stdlib `slices`/`maps` or write the loop. |
| `golang-samber-mo` | No | NEVER | Not imported. Pivox uses `(T, error)` returns. |
| `golang-samber-oops` | No | NEVER | We use `internal/apierr`. Don't replace. |
| `golang-samber-ro` | No | NEVER | No reactive-streams surface. |
| `golang-samber-slog` | No | NEVER | We use plain `log/slog`. Don't add a samber slog handler without explicit ask. |
| `golang-security` | Yes | **ALWAYS** for auth/crypto/I/O/secrets/user-input | Anything in `internal/authn/`, `internal/oidc/` (Keycloak token verify), `internal/crypto/`, `internal/server/` (interceptors, HTTP hooks), password/token paths, KMS, secret management, file-path handling. Also any new public-internet-facing input. |
| `golang-stay-updated` | No | NEVER | Resource list, not a code-review skill. |
| `golang-stretchr-testify` | Yes | **ALWAYS** for new tests | testify is the test library. `assert` vs `require`, mock argument matchers, `Eventually`, `JSONEq`. The mock package is what `internal/testutil/mocks/querier_mock.go` extends. |
| `golang-structs-interfaces` | Yes | ALWAYS for new types | Designing a struct/interface; pointer vs value receivers; embedding; composition; `accept interfaces, return structs`. |
| `golang-swagger` | No | NEVER | We use proto + grpc-gateway. No swaggo annotations. |
| `golang-testing` | Yes | **ALWAYS** for new code | TDD is the rule (see § Testing). Table-driven tests, parallel tests, fixtures, goleak, fuzzing, coverage. Pair with `golang-stretchr-testify`. |
| `golang-troubleshooting` | Yes | SOMETIMES | Debugging a bug, deadlock, race, or "something is wrong." Not for routine reviews. |
| `golang-uber-dig` | No | NEVER | DI container, not adopted. |
| `golang-uber-fx` | No | NEVER | DI framework, not adopted. |

### Maintenance rule

This table is not a snapshot — it's a contract. Whenever a change
to the codebase shifts an answer in any column, the same commit
must update this row. Concretely:

- Adopting `samber/lo` (or any Not-Applicable library above)? Flip
  `Applies?` to Yes, set `Use on review?` to ALWAYS or SOMETIMES,
  fill in the Trigger.
- Removing the last user of a library? Flip back to No / NEVER.
- Adding a new feature category (e.g. background jobs, GraphQL,
  CLI subcommand)? Re-evaluate every SOMETIMES row that could now
  apply ALWAYS.
- Stripping a feature out? Same, in reverse.

If you find yourself reaching for a skill not in this table, add a
row for it instead of invoking it ad-hoc. Treat the table as the
single index of "what we run on this codebase."

## Code quality bar

The bar is "would a senior Google engineer ship this." Concretely:

- **Production-quality from the start.** No "fix it later." If it's
  worth doing, it's worth doing once.
- **Foundational issues are your responsibility to flag**, not just
  the diff in front of you. If you read a handler that's
  structurally wrong (missing transaction, missing scope check,
  races, partial-failure-state), surface it to the user even when
  it's outside your current task. Don't go fix it without
  permission, but don't stay silent.
- **Tests passing ≠ correctness.** Unit tests with mocks cover happy
  paths. Ask: under partial failure? concurrent load? mid-flow
  restart? If those aren't tested, they aren't proven. Treat green
  CI as "this didn't regress what's tested," not "this is correct."
- **Don't add features, refactors, or abstractions beyond what was
  asked.** Three similar lines is better than a premature
  abstraction.
- **Push back on flawed ideas.** Don't rubber-stamp. The user wants
  honest reads, not agreement.
- **Call out overengineering** when you see it, even if it's
  something written earlier in the same session.
- **Verify before claiming green.** When validating a change, run
  the actual command (build, test, lint) and confirm the output.
  Don't trust gopls diagnostics — they go stale.

## Testing

**TDD is required for every feature, bug fix, and behavior change.
No exceptions.** Tests come first — production code only after a test
exists that fails for the right reason.

The cycle:

1. **Red.** Write a failing test that captures the intended behavior.
   Run it. Confirm it fails, and that the failure message points at
   the missing behavior — not at a typo, missing import, or unrelated
   compile error.
2. **Green.** Write the smallest production-code change that makes
   the test pass. Resist the urge to add nearby "while-I'm-here"
   improvements.
3. **Refactor.** With the test green, clean up the implementation
   AND the test. Re-run; both stay green.

Applies equally to:
- New Go handlers, services, packages
- Bug fixes (write the test that reproduces the bug first; watch it
  fail; then fix)
- Schema migrations (the integration test that exercises the new
  shape comes before the migration)
- The Engine (Rust `cargo test`, C/C++ Google Test) and any other
  stack — same rule, different framework

Narrow exceptions, used sparingly:
- Pure renames, formatting, and gofmt-equivalent mechanical changes
- Doc-only edits
- Build-tooling / Makefile / CI config that has no behavioral
  surface to test
- Generated code (sqlc output, proto code) — the *generator* is
  tested, not the output

If you're an AI assistant and you find yourself writing production
code without a failing test in hand, **stop**. Either write the test
first, or surface to the user that you're about to deviate and why.
"It's a small change" is not a reason. "There's no clean way to
test this layer" is a design smell — surface it; don't bypass TDD.

Run tests before committing. Commits that introduce new behavior
must include the test in the same commit, not a follow-up.

| Language | Framework | Run |
|---|---|---|
| Go | `go test` — table-driven | `go test ./...` |
| Rust | `cargo test` — `#[cfg(test)]` + `tests/` | `cargo test` |
| C/C++ | Google Test via CMake FetchContent | `ctest` |

See `docs/dev/testing.md` for framework patterns and CI integration.

### How to write Go tests

The TDD rule above says **when**. This section says **what shape**.
New agents: read this before reaching for `MockQuerier` or generating
any mock package.

**The shared test infrastructure is mandatory. No shortcuts.**

For every new or migrated Go test, you MUST:

1. Use **`internal/testutil/grpcharness`** for any test that touches a
   gRPC service. Constructing a service server directly + calling
   handler methods bypasses the interceptor chain (auth, membership,
   permission) and produces false-positive coverage. The handler will
   `panic: MustUserID called without an identity on context` the
   first time something sweeps the codebase looking for un-authed
   tests; don't be that test.
2. Use **`internal/testutil/fixtures`** for resource creation
   (organizations, spaces, operations, storage gateways, etc.). Do
   NOT inline a `createTestOrg` / `createTestSpace` helper — every
   inline copy is a future drift point when the schema changes.
   Missing a fixture you need? Add it to the fixtures package, then
   use it; don't inline.
3. Use **`internal/testutil/mocksetup`** for any helper that wraps
   testify mocks (`Expect*`-style helpers around the surviving
   `MockQuerier`-based tests during the migration). Same rule: add
   to the package, don't inline.
4. Use **`internal/testutil.SetupTestDB`** only when you need raw DB
   access without the gRPC stack. If you're calling SetupTestDB and
   then constructing a service server, you've taken a wrong turn —
   go back to grpcharness.

These aren't aspirations. They're the bar. "But the existing tests
in this package do it inline" is **not** a reason to add the
(N+1)th copy — that's exactly the cumulative-cost problem #71
exists to fix. New tests use the shared infrastructure from line 1;
tests being migrated as part of touching a package use it too.

The codebase's dedicated test infrastructure:

- **`internal/testutil/grpcharness`** — runs the real gRPC server with
  the real interceptor chain (auth, membership, permission, audit).
  Stub `authn.Service` lets tests control which user is authenticated
  via `SetCaller`. This is the right granularity for testing handlers
  end-to-end. Most existing handler tests bypass this and mock at the
  Querier level instead — that's the wrong layer and is being phased
  out (see #71).
- **`internal/testutil.SetupTestDB`** — testcontainers-go Postgres,
  real schema, per-test cleanup. For raw DB tests that don't need the
  full gRPC stack.
- **`rivertest.RequireInsertedTx[*riverpgxv5.Driver]`** — assert River
  jobs were enqueued by a handler. Checks the real `river.river_job`
  table inside the test's tx. No mocks.
- **`riverdbtest.TestSchema` / `TestTxPgx`** — auto-rolling tx +
  auto-cleanup schemas for test isolation without per-test DB
  recreate.
- **`internal/testutil/mocksetup` + `internal/testutil/fixtures`** —
  shared helpers/fixtures for the surviving mock-based tests during
  the migration (#71 Phase 1 lands these). New helpers go here; don't
  inline mock setup that matches an existing pattern.

**Narrow uses for mocks.**

- **`pgxmock`** — only for simulating connection-level errors that are
  hard to induce in integration (`ErrTxClosed` mid-tx, deadlock retry,
  connection pool exhaustion). Don't use it for happy-path coverage.
- **Pure-function tests** — for code that doesn't touch DB/RPC/network
  (validators, parsers, transformers, marshallers, error mappers). No
  mock library needed; call the function and assert output.

**Anti-patterns. Refuse these even if existing tests do them.**

- New tests using `internal/testutil/mocks/MockQuerier`. The mock is
  hand-maintained, tests assert call shape rather than behavior, and
  is being phased out per #71. Use `grpcharness` or integration tests
  for service-layer logic; pure-function tests for the rest. If the
  package's existing tests are `MockQuerier`-based, the new test
  should be the FIRST one in the new shape — don't add the (N+1)th
  copy of the broken pattern.
- Generating gRPC service mocks (`mockgen` for `OrganizationsServer`,
  etc.). Use `grpcharness` to dial the real server via bufconn.
- Stubbing the interceptor chain. The interceptors ARE the security
  boundary; tests that bypass them produce false confidence ("the
  test passed" doesn't mean "the production path passed" when
  production includes interceptors the test stubbed out).
- "Did we call `qtx.GetOrg(orgID)` with the right ID" as the sole
  assertion. Testing call shape duplicates what integration tests
  catch via the response. Acceptable when nested inside a richer
  test; not acceptable as the only point.
- Auto-generating `MockQuerier` (mockery / equivalent). Doesn't fix
  the testing-implementation problem; just makes the misfit cheaper
  to maintain. The migration plan is to delete the mock, not
  automate it.

**When you reach for copy-paste.**

If you're copying a test setup that already exists in another test,
extract a helper to `internal/testutil/mocksetup` (or a fixture to
`internal/testutil/fixtures`) **before** adding the third copy. Two
copies might be acceptable; three triggers consolidation. "But the
existing tests do it inline" is not a reason to add the (N+1)th
copy — the cumulative cost is what motivated #71.

**For AI agents specifically.**

Before writing a Go test that constructs a `MockQuerier`, stop and
ask:

1. Could this be an integration test against a real DB via
   `grpcharness` or `testutil.SetupTestDB`? **Default yes** for
   service-layer behavior.
2. Could this be a pure-function test with no mocks? Often yes for
   validators, parsers, transformers.
3. Is the mocked call shape the actual behavior I'm asserting, or
   am I imitating an existing test's shape because it's familiar?

If (1) or (2) is yes, write it that way. If you find yourself
adding to `MockQuerier`-based tests because "that's how the others
are written," surface to the user and ask whether to follow the
pattern or start the integration-test path. The pattern is on the
way out; don't add to it.

## Documentation

- Architecture docs live in `docs/`. Read the relevant ones before
  making design decisions.
- Do not create README files unless explicitly asked.
- Update affected docs when making architectural changes — including
  the per-stack `AGENTS.md` if the change introduces a new
  convention.
- Do not create planning, decision, or analysis documents unless
  asked. Work from conversation context.

## Commits and review

- Commit messages must explain the *why*, not just the *what*. The
  diff already shows the what.
- Stage explicit paths (`git add path/...`), never `git add -A` or
  `git add .` — those can include secrets, conflict markers, or
  unintended files.
- Never commit unless the user explicitly asks for it.
- Never push to `main` without explicit confirmation.
- Never use `--no-verify`, `--force` (push), or amend already-pushed
  commits unless explicitly requested.
- Surface phase completion + audit recommendation + commit-ready
  signal at every phase boundary.

## When to spawn audits

At every phase boundary, auto-spawn a code-reviewer agent over the
uncommitted changes — don't wait to be asked. The audit catches
mis-categorizations, missed call sites, and semantic regressions
before they ship. Don't accept "tests pass" as a substitute for
review on changes that touch foundational layers (auth, data
integrity, error handling, schema).

When you read code outside your current change scope and notice
something foundationally wrong, surface it as a separate finding —
not a silent fix.

## Pre-prod freedom and the destructive-action floor

- "Pre-prod freedom" applies: drop proto fields outright (no
  `reserved`), edit the init migration directly, drop/recreate dev DB
  at will when schema changes.
- DO NOT run destructive operations (`git clean -fd`, `--force`
  pushes, `DROP DATABASE` against shared/remote DBs, `rm -rf`
  on tracked files) without explicit confirmation in the current
  conversation. A previous approval doesn't authorize a future
  destructive action.

## File this doc

If you're an AI assistant: this doc is the floor. Subproject
`AGENTS.md` files refine the rules for their stack. Conflicts
between root and subproject — subproject wins for stack-specific
rules; root wins for cross-cutting rules. When in doubt, ask.
