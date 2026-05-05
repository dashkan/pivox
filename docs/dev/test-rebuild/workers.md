# `internal/workers` — test rebuild spec

Five periodic-job workers + one on-demand LRO worker. All current
tests use MockQuerier.

| Worker | Type | Existing test |
|---|---|---|
| `PurgeOrgsWorker` | periodic | `purge_orgs_test.go` (mock) |
| `PurgeSpacesWorker` | periodic | `purge_spaces_test.go` (mock) + space-purge cascade test in `internal/service/spaces/lifecycle_e2e_test.go` (real) |
| `VerifyDomainsWorker` | periodic | `verify_domains_test.go` (mock) |
| `ReapOperationsWorker` | periodic | `reap_operations_test.go` (mock) |
| `CleanupAuthWorker` | periodic | `cleanup_auth_test.go` (mock) |
| `UndeleteOrgWorker` | on-demand LRO | exercised by `lifecycle_undelete_river_e2e_test.go` (real) |

## Test pattern for workers

Workers run via River, which we don't want to start fully in unit
tests. The pattern from `lifecycle_undelete_river_e2e_test.go`
(start a real River client with the worker registered, kick a job
through the handler) works for on-demand workers but is overkill
for periodic workers that just call `Work(ctx, *river.Job[Args])`.

For periodic workers, the simpler shape is:

```go
h := grpcharness.New(t)         // for Pool + Queries
seed(...)                        // raw SQL or via Queries
w := &workers.PurgeOrgsWorker{Queries: h.Queries, Logger: ...}
require.NoError(t, w.Work(ctx, &river.Job[workers.PurgeOrgsArgs]{...}))
assertDBState(...)               // observe outcome
```

This is what the spaces lifecycle test already does for
`PurgeSpacesWorker.Work`.

## Behaviors per worker

### PurgeOrgsWorker

- [ ] Purges each org past `purge_time` (the actual cascade —
  observe via subsequent GetOrganization → NotFound)
- [ ] No-op when nothing is past purge_time
- [ ] Per-org failure (one bad row) doesn't block the rest
- [ ] List error from Queries returns the err (River retries)

### PurgeSpacesWorker

- [x] Cascade past grace (covered by spaces lifecycle test)
- [ ] No-op + per-row-failure semantics (same as PurgeOrgs)

### VerifyDomainsWorker

- [ ] Pending domain with valid TXT record → marked VERIFIED
- [ ] Pending domain with no TXT record → stays PENDING
- [ ] Pending domain with wrong TXT record → stays PENDING
- [ ] DNS lookup error → stays PENDING (treated as transient)
- [ ] Past grace deadline → marked FAILED (via the LRO worker
  terminal-failure path, which currently runs in a separate
  worker — verify the contract)

### ReapOperationsWorker

- [ ] Deletes operations past `expire_time` (terminal state +
  expire elapsed)
- [ ] Doesn't delete still-running operations
- [ ] Error from list propagates

### CleanupAuthWorker

- [ ] Deletes expired `auth_token_codes` rows
- [ ] Deletes expired `delegated_auth_sessions` rows
- [ ] Token-code failure doesn't block session cleanup (or
  vice-versa) — boundary between the two tables

### UndeleteOrgWorker

- [x] Happy path covered by River-driven e2e test
- [ ] Past purge_time → terminal-fail with FailedPrecondition
- [ ] Concurrent purge worker race: org row gone before
  UndeleteOrgWorker runs → terminal-fail

## Drop list

- ~~`*Args_Kind` tests~~ — every Args type has a `Kind() string`
  test that just asserts the kind constant. The compiler enforces
  the interface; the test is mock theater. Drop all six.

## Shape of the rewrite

- Per-worker `<worker>_test.go` file using grpcharness for Pool
  + Queries, seeding rows directly via SQL or Queries, calling
  Work() directly. Pattern matches the existing spaces purge
  test.

- River-side concerns (scheduling, leader election, periodic
  cadence) belong in `cmd/pivox-worker/main.go` smoke tests, not
  here.
