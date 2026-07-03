# `internal/testutil/` — test helpers and fixtures

Three packages, one canonical entry point per layer:

- **`grpcharness/`** — bufconn-based gRPC test harness with the real
  production interceptor chain (auth → membership → permission →
  validation) and a real Postgres backend. **The right home for
  every service-layer integration test.** Service handlers depend
  on context values populated by interceptors (`MustResolvedOrgFromContext`,
  `MustUserID`); a stub server bypasses those and produces
  false-positive coverage.
- **`fixtures/`** — typed, deterministic factories for `db.X` rows
  used directly against the DB (LRO, filter, anywhere a raw row is
  the system under test). Stable defaults so tests can assert
  against them without snapshots.
- **`authnmock/`** — mockery-generated mock for `authn.Service`,
  the only external boundary we can't reasonably integration-test
  against. Constructed via `authnmock.NewMockService(t)`, which
  auto-registers `AssertExpectations` in `t.Cleanup` so unverified
  expectations surface as failures.

Plus the package-level helpers:

- **`testutil.SetupTestDB(t)`** — returns a pool + `*db.Queries`
  pointing at a fresh per-test database cloned from a shared,
  migrated template. Cleanup is auto-registered via `t.Cleanup`.
- **`testutil.SetupTestS3(t)`** — same shape but for the
  rustfs-backed S3-compatible bucket store.

Both helpers depend on the docker-compose stack defined in
`docker-compose.test.yml` (started via `make test-up`, idempotent).

## When to reach for which

| Need | Use |
|---|---|
| Service-layer integration test | `grpcharness` (bufconn + real DB + real interceptor chain) |
| Raw DB query test (no gRPC) | `testutil.SetupTestDB` |
| Pure-function test (validators, parsers, transformers) | Plain `go test`, no helpers |
| Asserting River job enqueue | `rivertest.RequireInsertedTx` against the harness's pool |
| Hard-to-induce DB error (closed tx, deadlock) | `pgxmock` |
| Asserting Firebase Auth provider calls | `authnmock.NewMockService(t)` (or `grpcharness.NewMockedFirebaseAuth(t)`) |

Anti-patterns refused on review:

- Hand-rolled mocks for interfaces we already mockery-generate.
- Mocking `db.Querier` (deleted in #71 — service-layer tests use
  the real DB via `grpcharness`).
- Constructing a service server with all-nil fields to bypass the
  Config-with-panic validation.
- Bypassing the interceptor chain in service-layer tests. The
  interceptors ARE the security boundary; tests that stub them
  produce false confidence.

## fixtures conventions

Each fixture is a constructor function that returns a default
populated `db.X` value. Options modify specific fields:

```go
fixtures.Org()                              // default Acme org
fixtures.Org(fixtures.OrgID(specificUUID))  // with a specific ID
fixtures.Org(fixtures.OrgName("widgets"),
    fixtures.OrgState(db.ResourceStateDELETEREQUESTED))
```

- Constructor name: `<Type>()` (e.g., `Org`, `Operation`,
  `StorageGateway`).
- Option name: `<Field>(value)` (e.g., `OrgID`, `OrgName`,
  `OrgState`).
- Defaults stable across calls (no `time.Now()`, no `uuid.New()`).
- Time defaults: `2026-01-01T00:00:00Z`. UUIDs:
  `00000000-0000-7000-8000-…`.

When a new resource type starts appearing in three test files,
add its fixture here rather than copy the inline literal a fourth
time.
