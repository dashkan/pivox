# Test Coverage Plan — Go Codebase

## Goal

100% test coverage for Cloud Controller + Storage Agent Go code. Unit tests + integration tests for every package.

## Prerequisites (Done)

- [x] All services accept `db.Querier` interface (mockable)
- [x] `internal/testutil/db.go` — testcontainers Postgres setup + migrations
- [x] `internal/testutil/grpc.go` — bufconn gRPC server helper
- [x] `internal/testutil/mocks/querier_mock.go` — full MockQuerier (153 methods)
- [x] `testify` available (assert, require, mock)

## Important Notes

- **Postgres 18** — use `postgres:18` image in testcontainers. PG18 has native `uuidv7()` — no extension needed.
- **Update `internal/testutil/db.go`** to use `postgres:18` image if it currently specifies a different version.
- **Do NOT use worktrees** for parallel test writing — they branch from the current commit and agents make conflicting production code changes. Write directly to main, sequentially.
- **Read production code before writing tests** — every time. Don't assume structure from docs.
- **Integration tests need a `testing.Short()` guard** — skip when Docker isn't available.

## Execution Order

Sequential. Each step may require production code changes for testability.

### Step 1: Testability refactoring

Before writing any tests, make production code changes needed for mockability:

1. **`internal/service/organizations/server.go`** — Extract `TenantService` interface from `*firebase.AuthService` dependency. Extract `TxBeginner` interface from `*pgxpool.Pool` for transaction creation. This enables unit testing the org creation + Firebase tenant + rollback logic without a real Firebase connection.

2. **`internal/resource/resource.go`** — `ResolveOrgParent` currently accepts `db.Querier` (already done). Verify it works with `MockQuerier` — the mock needs to satisfy whatever subset of methods `ResolveOrgParent` calls.

3. **`internal/crypto/`** — Already has `Encryptor` interface + `NoOpEncryptor`. Ready for testing.

4. **`internal/firebase/auth.go`** — Thin wrapper around Firebase SDK. Extract interface if any service tests need to mock it (organizations does).

Run `go build ./...` after refactoring. Commit.

### Step 2: Shared packages (no DB dependency)

These are pure unit tests — no testcontainers, no gRPC, fast.

| Package | Test file | What to test |
|---|---|---|
| `internal/agentstream/` | `connection_test.go` | Register, Unregister, SendToGateway, SendToAll, concurrent access |
| `internal/agentstream/` | `audit_test.go` | MarshalAndRedact, secret redaction, valid JSON output |
| `internal/crypto/` | `crypto_test.go` | NoOpEncryptor round-trip, interface compliance |
| `internal/iam/` | `iam_test.go` | GetIamPolicy, SetIamPolicy (etag), TestIamPermissions — mock DB |
| `internal/storageagent/` | `cache_test.go` | Put, Get, LRU eviction, TTL, memory limits |
| `internal/storageagent/` | `session_test.go` | Create, validate, expire, revoke |
| `internal/storageagent/` | `denied_test.go` | Pattern matching, replace, clear |
| `internal/storageagent/` | `stream_test.go` | Request/response correlation, fire-and-forget |
| `internal/storageagent/` | `endpoints_test.go` | Endpoint routing, serve file |
| `internal/storageagent/` | `http_test.go` | Auth middleware, CORS, denied patterns |

Run `go test ./internal/agentstream/ ./internal/crypto/ ./internal/iam/ ./internal/storageagent/`. Commit.

### Step 3: Thin CRUD services — unit tests

Mock DB, test handler logic. These are thin so unit tests focus on: resource name parsing, error paths, field mask handling.

| Package | Test file | What to test |
|---|---|---|
| `internal/service/apikeys/` | `server_unit_test.go` | CRUD, key string generation, name parsing |
| `internal/service/projects/` | `server_unit_test.go` | CRUD, LRO wrapping, field mask, soft delete |
| `internal/service/operations/` | `server_unit_test.go` | Get, List, Delete, Cancel, Wait — delegates to lro.Manager |

Run `go test ./internal/service/apikeys/ ./internal/service/projects/ ./internal/service/operations/`. Commit.

### Step 4: State machine services — unit tests

These have real logic. Test every valid and invalid state transition.

| Package | Test file | What to test |
|---|---|---|
| `internal/service/requests/` | `server_unit_test.go` | Every state transition (valid + invalid), CRUD, line item creation |
| `internal/service/assets/` | `server_unit_test.go` | PLACEHOLDER→PROCESSING→ACTIVE, version counting, CRUD |

Run tests. Commit.

### Step 5: Complex services — unit tests

| Package | Test file | What to test |
|---|---|---|
| `internal/service/organizations/` | `server_unit_test.go` | Create with Firebase tenant (mock), tx rollback on failure, CRUD, IAM |
| `internal/service/tags/` | `tags_unit_test.go` | Keys, values, bindings CRUD. Deletion constraints (can't delete key with values, value with bindings) |
| `internal/service/storage/` | `gateways_unit_test.go` | CRUD, token rotation, JWT session, install script |
| `internal/service/storage/` | `agents_unit_test.go` | CRUD |
| `internal/service/storage/` | `endpoints_unit_test.go` | CRUD, S3/filesystem config JSON marshaling |
| `internal/service/storage/` | `agent_service_unit_test.go` | Bidi stream: handshake, heartbeat, audit, reconnect (mock gRPC stream) |

Run tests. Commit.

### Step 6: Integration tests

All services, real Postgres (PG18 via testcontainers), real gRPC (bufconn).

| Package | Test file | What to test |
|---|---|---|
| `internal/service/apikeys/` | `server_integration_test.go` | Full CRUD through gRPC |
| `internal/service/projects/` | `server_integration_test.go` | Full CRUD through gRPC |
| `internal/service/operations/` | `server_integration_test.go` | Create/get/list/delete lifecycle |
| `internal/service/requests/` | `server_integration_test.go` | Full workflow: create→submit→assign→deliver→approve |
| `internal/service/assets/` | `server_integration_test.go` | State transitions, CRUD |
| `internal/service/organizations/` | `server_integration_test.go` | CRUD, IAM policies |
| `internal/service/tags/` | `tags_integration_test.go` | Full lifecycle: key→value→binding→delete chain |
| `internal/service/storage/` | `integration_test.go` | Gateway+endpoint workflow, bidi streaming |

Run all tests. Commit.

### Step 7: Coverage report

```bash
go test ./... -coverprofile=coverage.out -short
go tool cover -func=coverage.out | tail -1
```

Identify gaps, write additional tests to reach 100%.

## Packages with Existing Tests (Already Have Coverage)

- `internal/apierr/` — error construction
- `internal/convert/` — DB to proto conversions
- `internal/filter/` — query filter transpilation
- `internal/lro/` — LRO conversions
- `internal/resource/` — resource name parsing
- `internal/server/` — validation interceptor

These should be checked for coverage gaps in Step 7 but don't need new test files.
