# Cloud Controller — Go backend conventions

Scope: `cmd/pivox-cloud/`, `internal/`. The Cloud Controller is the
SaaS management layer for Pivox — gRPC API, REST gateway, Postgres
persistence, Firebase auth integration.

Read this before touching Go code under `internal/`.

## Stack

- **Language**: Go (currently 1.24+).
- **API**: gRPC + REST gateway via grpc-ecosystem/grpc-gateway.
- **DB**: Postgres via `pgxpool`. Queries are `sqlc`-generated from
  `internal/db/queries/*.sql` into `internal/db/generated/`.
- **Migrations**: `internal/db/migrations/` (golang-migrate). Pre-prod
  edits the init migration directly; no migration squashing rituals.
- **Auth**: Firebase Auth (verified via `firebase-admin-go`),
  Identity tokens for service-to-service.
- **Errors**: standardized via `internal/apierr`. Always use this
  for gRPC status errors — never `status.Error` directly.

## Database transactions — **load-bearing rule**

Every handler that touches more than one DB statement, or mixes a
read with a write on the same resource, MUST run inside a
transaction. The single statement is fine without — autocommit covers
it. Anything else opts in via `internal/db.RunInTx` (or explicit
`pool.Begin → qtx → commit` if you have a reason to control the
boundary directly).

The handler-internal pattern:

```go
result, err := db.RunInTx(ctx, s.pool, func(qtx db.Querier) (T, error) {
    role, err := qtx.GetSystemRole(ctx, ...)   // scope check inside tx
    if err != nil { return zero, err }
    return qtx.CreateOrgUserMember(ctx, ...)   // write inside same tx
})
```

Inside the closure, **always use `qtx`, never `s.queries`**. Mixing
the two is a bug — `s.queries` runs on a different connection,
defeats the atomicity, and creates a TOCTOU window between the scope
check and the write.

Why this matters:

- Multi-step writes need atomicity. A `CreateRequest` that fans out to
  multiple `CreateAsset` + `CreateLineItem` calls without a tx leaves
  partial state on any failure mid-loop. Client gets an error;
  database has a half-built request. Real, observed bug class.
- Scope checks (`GetGroupByID(id, orgID)` to confirm same-org) only
  hold under the tx that does the subsequent INSERT. Outside a tx,
  the parent can mutate between the check and the write.

The lint check enforces this on PR — calls to
`<*Querier>.{Create,Update,Delete,Upsert,Soft*,Hard*,Insert*}` from
inside a function that also calls any other `<*Querier>` method MUST
be inside `RunInTx` (or have a `//nolint:tx <reason>` annotation
documenting why a tx isn't needed).

## Constructor pattern

Every multi-arg constructor takes a single `Config` struct (or
`XxxConfig` for multi-server packages):

```go
type Config struct {
    Pool       db.DBTX        // Required.
    Queries    db.Querier     // Required.
    Codec      *appkey.Codec  // Required.
    AuditResolver *audit.Resolver  // Optional; nil leaves Actor fields unset.
}

func NewServer(cfg Config) *Server {
    if cfg.Pool == nil { panic("pkg: Config.Pool is required") }
    // ...
}
```

Required fields panic at construct time. Startup-time programmer
errors fail loud on `pivox-cloud` boot rather than nil-deref mid-RPC.
Panic message format: `"<package>: <ConfigName>.<Field> is required"`.

Single-arg constructors (`NewResolver(queries db.Querier)`) and
primitive-input factories (`NewFromHex(hex string)`) keep their
positional shape — no positional ambiguity, Config adds noise without
information.

## Never dodge an import

If a function takes `*pgxpool.Pool`, declare `*pgxpool.Pool` and
import `pgxpool`. Same for `pgconn.CommandTag`, `pgx.Tx`, every
other type from a real package.

**Forbidden:**

- Made-up type aliases like `type pgconnTag any` to avoid `import
  "github.com/jackc/pgx/v5/pgconn"`.
- Structural interfaces with `any` returns invented to dodge the
  real return type (`interface{ Exec(...) (someAlias, error) }`).
- `interface{}` / `any` parameters when the real type is known and
  imported elsewhere in the codebase.

**Why:** these are dialects of the same anti-pattern that
motivated #71 — using fake types to make code "easier" to write
hides the actual types under test, breaks IDE navigation, dodges
type-system safety, and accumulates technical debt that surfaces
later as bugs the type system would have caught. We just spent a
session deleting 2,591 lines of MockQuerier code that ran on
exactly this principle. Don't reintroduce it.

**The rule:** if you find yourself reaching for a structural
shim or alias to avoid an import, **just import the package**.
The cost of the import line is zero. The cost of the made-up
type is "find this in audit six months later."

## Error handling

Always go through `internal/apierr`. The two main entry points:

- `apierr.HandleResourceError(err, resourceType, resourceName)` for
  read paths and write paths where any `pgx.ErrNoRows` / unique
  violation / FK violation maps cleanly to the named resource.
- `apierr.IsUniqueViolation(err)` for write-path handlers that want
  the AlreadyExists branch but explicitly NOT FK→NotFound mapping
  (CREATE handlers where every FK is pre-validated upstream and any
  23503 is a transient race).

Never call `status.Error(codes.X, ...)` directly in handlers. The
standardized errors carry `ErrorInfo` / `ResourceInfo` details that
clients depend on.

## Audit fields (`created_by` / `updated_by` / `deleted_by`)

Every mutable resource carries `created_by`, `updated_by`,
`deleted_by` UUID columns referencing `identities(id)`. Inflated to
proto Actor messages via `internal/audit.Resolver`:

```go
actors, err := s.audit.Resolve(ctx, []uuid.UUID{row.CreatedBy.Bytes, ...})
proto := convert.OrganizationToProto(row, actors)
```

The Resolver caches identity lookups in-process (LRU + TTL).
Mutation handlers (DeleteAccount, syncIdentity webhook, anything
that mutates `identities`) call `audit.Resolver.Invalidate(id)` to
drop stale entries. Other instances catch up via TTL.

## Permission model

- Permission resolution is centralized in `internal/permission`.
  `Resolver.HasPermission(ctx, identity, target, permName)` answers
  the IAM check.
- The auth chain at the gRPC server is: `AuthInterceptor` (verifies
  Firebase token + sets `pivox_user_id` ctx claim) →
  `MembershipInterceptor` (gates non-allowlisted RPCs on org
  membership) → `PermissionInterceptor` (per-RPC permission via
  registry).
- Handlers read `server.MustPivoxUserID(ctx)` for the caller's
  identity UUID and `server.MustResolvedOrgFromContext(ctx)` for the
  pre-resolved org row (no re-query in the handler).

## Resource naming

AIP-style. Resource paths look like
`organizations/{org}/spaces/{space}/conversations/{conv}` — slugs at
each scope. Parsing helpers live in `internal/resource`. UUIDs in
paths only at the leaf when AIP requires; intermediate scopes use
their stable slug name.

## Schema conventions

- Soft-delete (`delete_time`, `purge_time`, `is_deleted` flag for
  identities). Hard-delete is operator-only — no public RPC issues
  `DELETE FROM <table>`.
- Audit fields are FK-enforced to `identities(id)` and use
  `pgtype.UUID` (nullable) for unset values. `convert.PgUUID(id)`
  wraps `uuid.UUID` → `pgtype.UUID` and treats `uuid.Nil` as
  `{Valid: false}`.
- Junction tables that reference scope-bearing parents (`org_members`
  → `groups`, etc.) — the FK doesn't enforce same-scope. Either
  denormalize the scope column into the junction table for composite
  FK, or scope-check via the same `qtx` as the INSERT.

## sqlc

- Queries live in `internal/db/queries/*.sql`. Edit there, regenerate
  with `make proto-generate` (which chains to `go generate ./...` and
  the sqlc step).
- Generated code lives in `internal/db/generated/`. Don't edit by
  hand — regenerate.
- Mock querier is `internal/testutil/mocks/querier_mock.go`.
  Auto-generated stub; if a query is added/removed, the mock must
  be regenerated.

## Tests

- Default suite: `go test ./...`. All packages must pass.
- Integration suite: `go test -tags=dev ./...`. Some packages have
  pre-existing tech debt under `-tags=dev` — known, tracked
  separately. Don't claim a change is green if you only ran the
  default suite for a change that touches dev-tagged code.
- Race detector: `go test -race ./...` for any concurrency-relevant
  change (cache invalidation, locks, atomic counters).
- For unit tests that need a stub server, build a struct literal
  directly: `&XxxServer{queries: q}`. Don't call the constructor with
  all-nil placeholders — the constructor panics on missing required
  fields by design.

## When you're stuck

- Protocols + naming conventions: `docs/architecture/`.
- Build / migrations / DB seeding: `docs/build.md`,
  `scripts/seeds/`.
- Per-feature docs: `docs/features/`.
- AI-elements / chat: `internal/service/aichat/AGENTS.md` (pending).
