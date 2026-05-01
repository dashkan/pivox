# Pivox — agent instructions

This is the root agent doc. It orients you to the repo, captures
cross-cutting rules, and points you at per-stack conventions.

## Components

Refer to components by their canonical names, not their tech stack:

- **Cloud Controller** — SaaS management layer. Source of truth.
  Hosts the public gRPC API + REST gateway, owns Postgres
  persistence, integrates Firebase Auth.
- **Native App** — operator application. SwiftUI on macOS, WinUI 3
  on Windows, shared C++ core.
- **Engine** — playout engine. Compositor, plugins (CEF, Rive,
  FFmpeg), output adapters.
- **Storage Agent** — on-prem agent for asset storage, paired with
  Cloud Controller.
- **Playout Agent** — on-prem agent installed alongside engines.
- **Cloud Functions** — Firebase blocking functions for auth-time
  hooks (identity sync into Pivox at sign-up / sign-in).

Tech-stack references ("Go backend", "Rust engine") are appropriate
in build docs / architecture decisions where the technology is the
point. Avoid them in user-facing copy and in conversation about
features.

## Repository layout

Top-level directories you'll touch most:

```
cmd/                  Go binary entrypoints
  pivox-cloud/        Cloud Controller server (gRPC + REST + workers)
  pivox-agent/        Storage Agent binary
  encrypt-sso-secret/ One-shot operator tool: KMS-encrypt SsoConfig client_secret
  gen-permissions/    Codegen: regenerates permission catalog Go
  gen-permission-registry/  Codegen: regenerates RPC→permission registry

internal/             Go application code (Cloud Controller + Storage Agent)
  AGENTS.md           Go conventions — required reading for backend work
  apierr/             gRPC status-error builders. Always go through this.
  appkey/             Codec for opaque resource-name encoding (HMAC-signed).
  audit/              Identity → Actor resolver with in-process LRU cache.
  authn/              Firebase Auth verification, identity tokens.
  config/             Server config structs (CLI flags hydrate these).
  convert/            Proto ↔ DB row conversion helpers.
  crypto/             Pluggable encryptor (KMS prod, NoOp dev).
  db/                 Postgres data access
    queries/          Hand-written sqlc queries (*.sql)
    migrations/       golang-migrate files (000001_init.up.sql is THE source)
    generated/        sqlc output — do not edit
  filter/             AIP-160 filter parser + DB-query lowering.
  firebase/           Firebase Admin SDK wrapper.
  lro/                Long-running operation manager (AIP-151).
  permission/         IAM permission catalog + resolver.
  pkg/gen/            Generated proto code (do not edit).
  resource/           AIP resource-name parser/formatter.
  server/             gRPC interceptor chain, internal HTTP hooks, OAuth broker.
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

deployments/firebase/ Cloud Functions
  AGENTS.md           Cloud Functions conventions
  functions/          TypeScript source

native/               Native App
  AGENTS.md           Native conventions
  platform/macos/swift/  SwiftUI + AppKit code
  platform/windows/      WinUI 3 + C++/WinRT code
  core/                  Shared C++ core
  build-xcode/           Generated Xcode project (cmake -G Xcode)

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

Other stacks:

- **`native/AGENTS.md`** — macOS/Windows/shared-core conventions.
- **`deployments/firebase/AGENTS.md`** — Firebase Functions conventions.

## Build + run + test

### Go (Cloud Controller + Storage Agent)

Two build modes — production and `-tags dev`. The `dev` tag swaps a
handful of files for dev-friendlier alternatives at compile time
(see "The `dev` build tag" below).

```sh
# Production-mode build/run
make build                              # bin/pivox-cloud + bin/pivox-agent
make run-server                         # go run ./cmd/pivox-cloud serve
make run-agent                          # go run ./cmd/pivox-agent storage

# Dev-mode build/run (-tags dev)
make dev-build                          # same outputs, dev variants
make dev-server                         # go run -tags dev ./cmd/pivox-cloud serve
make dev-agent                          # go run -tags dev ./cmd/pivox-agent storage

# Hot reload (install `air` separately — not pinned in tools/go.mod
# due to a transitive dep conflict with api-linter)
make air                                # configs/air.toml — prod-mode reload
make dev-air                            # configs/air.dev.toml — dev-mode reload

# Tests
make test                               # go test ./... (default suite)
go test -tags=dev ./...                 # integration suite (real PG required)
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
make proto-generate                     # full chain: proto → Go + native + sqlc
make proto-generate-go                  # buf generate (Go only)
make proto-generate-native              # SwiftProtobuf + grpc-swift-2
make lint-proto                         # buf lint
make api-lint                           # AIP api-linter
make proto-format                       # buf format -w

# Docker (local pg + adminer)
make docker-up / docker-down

# Firebase
make firebase-emu                       # local emulator suite
make firebase-deploy                    # deploy functions
make clean-fn-revisions                 # prune old Cloud Run revisions
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

### The `dev` build tag

A small set of files have `dev` and non-`dev` variants. Building
with `-tags dev` selects the dev variant; default builds get the
production variant. Concretely what gets swapped:

| Component | Production (default) | `-tags dev` |
|---|---|---|
| `crypto.NewEncryptor` | KMS-backed `GoogleCloudKMSEncryptor` | `NoOpEncryptor` (passthrough) |
| `appkey.NewFromEnv` | requires `PIVOX_APP_KEY` env to be set | generates a random per-process key on missing env, warns |
| `server.NewInternalHooks` syncIdentity auth | OIDC identity-token verification | static shared-secret bearer |
| `storageagent` HTTP auth | enforces session auth | skips session auth |
| `config.SyncAuthConfig` shape | OIDC fields | shared-secret field |

The `dev` tag is for local-loop development against the Firebase
emulator (which can't mint OIDC tokens) and quick `air`-driven
iteration. **Don't ship a `-tags dev` binary to a real environment**
— the encryptor passthrough alone is a security hole.

Test files also use the `dev` tag: integration tests that need real
Postgres (`-tags=dev` runs them; default suite skips). Some
dev-tagged tests have known pre-existing issues (`-tags=dev` is not
fully green today — track separately, don't claim a change is green
based on `-tags=dev` runs unless you specifically validated the
affected packages).

### Native

Read **`docs/build.md`** first. Native build is `xcodebuild`, not
`cmake --build` (which builds broken UITests).

```sh
cd native
cmake -G Xcode -B build-xcode -S .                     # regen Xcode project
xcodebuild build -project build-xcode/Pivox.xcodeproj -scheme Pivox \
  -configuration Debug -allowProvisioningUpdates
xcodebuild test -scheme PivoxTests                     # unit tests
make test-native-ui                                    # UI tests (XCUITest)
```

### Cloud Functions

```sh
cd deployments/firebase
# Build/deploy is wrapped at repo root:
make firebase-emu                       # local emulator suite
make firebase-deploy                    # production deploy
```

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
  Don't trust SourceKit / gopls diagnostics — they go stale.

## Testing

All new code uses TDD. Write tests first, then implementation.

| Language | Framework | Run |
|---|---|---|
| Go | `go test` — table-driven | `go test ./...` |
| Rust | `cargo test` — `#[cfg(test)]` + `tests/` | `cargo test` |
| C/C++ | Google Test via CMake FetchContent | `ctest` |
| Swift / Obj-C | XCTest, XCUITest | `xcodebuild test -scheme PivoxTests` |
| WinUI 3 / C++/WinRT | MSTest, WinAppDriver, gtest | `vstest.console.exe` |

Run tests before committing. See `docs/dev/testing.md` for framework
patterns and CI integration.

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
