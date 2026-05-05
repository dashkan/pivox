# `internal/testutil/` — test helpers and fixtures

Three packages, three purposes:

- **`fixtures/`** — typed factories for `db.X` test rows. Default
  values + functional options.
- **`mocksetup/`** — wrappers around `MockQuerier.On(...)` for the
  highest-frequency mock patterns. Centralizes the call shape so a
  sqlc regen updates one helper, not every test.
- **`grpcharness/`** — bufconn-based gRPC test harness with the real
  interceptor chain. **The right home for new service-layer tests.**

The other artifacts (`db.go` testcontainers helper, `mocks/` legacy
hand-written MockQuerier) are existing infrastructure.

## When to reach for which

The decision tree from `CLAUDE.md` (Testing section) applies. Brief
form:

| Need | Use |
|---|---|
| New service-layer test | `grpcharness` (real gRPC stack + real DB) |
| New raw DB test | `testutil.SetupTestDB` |
| New pure-function test (validators, parsers) | Plain `go test`, no helpers |
| Asserting River job enqueue | `rivertest.RequireInsertedTx` |
| Hard-to-induce DB error | `pgxmock` |
| **Touching a legacy `MockQuerier`-based test** | `fixtures` + `mocksetup` (the helpers in this directory) |

`fixtures` and `mocksetup` exist to **reduce churn during the migration
in [#71](https://github.com/dashkan/pivox/issues/71)**. They are not
the destination — they're the smaller, less-painful interim shape for
the legacy tests we haven't migrated yet.

**Don't add to them for new code.** New service-layer tests go through
`grpcharness`; new pure-function tests don't need helpers at all.

## fixtures conventions

Each fixture is a constructor function that returns a default
populated `db.X` value. Options modify specific fields:

```go
fixtures.Org()                              // default Acme org
fixtures.Org(fixtures.OrgID(specificUUID))  // with a specific ID
fixtures.Org(fixtures.OrgName("widgets"),   // composed
    fixtures.OrgState(db.ResourceStateDELETEREQUESTED))
```

- Constructor name = `<Type>()` (e.g. `Org`, `Operation`,
  `StorageGateway`).
- Option name = `<Field>(value)` (e.g. `OrgID`, `OrgName`,
  `OrgState`).
- Defaults are stable across calls (deterministic UUIDs, fixed
  timestamps) so tests can assert against them without snapshotting.
- Time defaults: `2026-01-01T00:00:00Z`. UUIDs: `00000000-0000-7000-8000-…`.

## mocksetup conventions

Helpers wrap one or more related `MockQuerier.On(...)` calls. Naming:

- `Expect<Method>(q, args, return)` — happy path.
- `Expect<Method>NotFound(q, args)` — `pgx.ErrNoRows`.
- `Expect<Method>Error(q, args, err)` — arbitrary error.

```go
mocksetup.ExpectGetOrgByName(q, "acme", fixtures.Org())
mocksetup.ExpectGetOrgByNameNotFound(q, "missing")
```

When sqlc regenerates and changes a method's signature, **update the
helper, not the call sites**. That's the maintenance win.

## Adding a new helper

If you're inlining the same `mockQ.On(...)` setup in a third test,
extract it. Two copies acceptable; three triggers the helper. Add to
the appropriate file by domain (`orgs.go`, `operations.go`,
`storage_gateways.go`, etc.) — split by domain, not by test
package.

When the legacy test it's helping eventually gets migrated to
`grpcharness`, the helper goes too. The helper layer shrinks as the
migration progresses; final state is empty/deleted.
