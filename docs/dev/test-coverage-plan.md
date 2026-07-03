# Test Coverage Plan — Go Codebase

> **Status: historical.** This plan was written when the codebase
> still carried `-tags dev`, the hand-written `MockQuerier`, and a
> testcontainers-per-package shape. The actual landed state is:
> `make test` against the docker-compose stack, no dev tag, no
> querier mock, service tests through `internal/testutil/grpcharness`.
> The phase-by-phase notes below remain useful as a record of what
> was migrated — but commands and file paths must be cross-checked
> against the current tree before anyone re-runs them.

## Goal

90%+ coverage for every hand-written Go package. Generated code (`internal/db/generated/`, `internal/pkg/gen/`) is excluded — it's exercised indirectly through integration tests. (This plan predates the Keycloak migration; the external SDK wrapper `internal/firebase/` it also excluded has since been **deleted** — auth is now the `internal/oidc` Keycloak verifier, covered by its own unit tests.)

## Current State (unit tests only, `-short` mode)

| Package | Coverage | Status |
|---|---|---|
| `internal/apierr` | **100%** | ✅ Done |
| `internal/convert` | **100%** | ✅ Done |
| `internal/crypto` | **100%** | ✅ Done |
| `internal/lro` | **98.8%** | ✅ Done |
| `internal/agentstream` | **96.3%** | ✅ Done |
| `internal/iam` | **95.2%** | ✅ Done |
| `internal/resource` | **95.0%** | ✅ Done |
| `internal/service/operations` | **88.9%** | Near-complete |
| `internal/service/requests` | **67.9%** | List needs integration |
| `internal/service/assets` | **67.8%** | List needs integration |
| `internal/storageagent` | **66.4%** | Stream + S3 paths remain |
| `internal/service/apikeys` | **59.9%** | List needs integration |
| `internal/service/projects` | **59.8%** | List needs integration |
| `internal/service/tags` | **55.7%** | List needs integration |
| `internal/service/storage` | **54.5%** | AgentService Connect + updates |
| `internal/service/organizations` | **51.8%** | CreateOrg + List need integration |
| `internal/filter` | **40.5%** | Query/Scan need real DB, transpiler edges |
| `internal/server` | **40.8%** | InternalHooks entirely untested |
| `internal/authn` | — | Interface-only, no logic |
| `internal/config` | — | Plain structs, no logic |

## Prerequisites (all done)

- [x] `db.Querier` interface — every service accepts it
- [x] `authn.Service` interface — IDP-agnostic auth, replaces `*firebase.AuthService`
- [x] `LROManager` interface — in `operations/server.go`, mockable
- [x] `TxBeginner` interface — in `organizations/server.go`, mockable
- [x] `testutil/db.go` — testcontainers `pgvector/pgvector:pg18` + pgvector types + migrations
- [x] `testutil/grpc.go` — bufconn gRPC server
- [x] `testutil/mocks/querier_mock.go` — full MockQuerier
- [x] Dead `pool` fields removed from storage servers, LRO manager, requests server
- [x] `errors.Is(err, pgx.ErrNoRows)` across all sites
- [x] `lro.runWork` properly fails operations on marshal errors

## Notes

- Use `-tags dev` when running `./...` or testing `internal/crypto/` or `internal/storageagent/` (activates `NoOpEncryptor` and `devSkipAuth`). Other packages don't need it but it's harmless to always pass.
- **Read production code before writing tests** — don't assume structure from this doc.
- Integration tests use `testing.Short()` guard.
- testcontainers image: `pgvector/pgvector:pg18`.

## Known Bugs to Fix

- [ ] `filter/scan.go` `ScanTagBindings` — missing `origin` column in row scan. Fix before running List integration tests for tag bindings.
- [ ] `UndeleteKey` / `UndeleteProject` — `GetApiKeyByOrgAndKeyID` and `GetProjectByName` filter `delete_time IS NULL`, so undelete can never find the soft-deleted row. Integration tests for undelete must be skipped until fixed.

## Remaining Work

### Phase 1: Integration tests for all List endpoints

All `List*` methods use `filter.Query()` which builds raw SQL — only testable with real Postgres. This is the single biggest coverage gap across all service packages.

Each existing `*_integration_test.go` file already sets up testcontainers + bufconn. Add List subtests to each.

**`service/apikeys/server_integration_test.go`** — add:
- `ListKeys` — create 3 keys under org, list with parent, verify count and order

**`service/projects/server_integration_test.go`** — add:
- `ListProjects` — create 2 projects, list with parent, verify count

**`service/organizations/server_integration_test.go`** — add:
- `ListOrganizations` — create 2 orgs, list, verify both returned (already partially tested — ensure pagination path hit)

**`service/requests/server_integration_test.go`** — add:
- `ListRequests` — create requests in approve workflow, list, verify count
- `ListRequests_ShowDeleted` — delete a request, list with `show_deleted=true`

**`service/assets/server_integration_test.go`** — add:
- `ListAssets` — create 2 assets, list, verify count
- `ListAssets_ShowDeleted` — delete an asset, list with `show_deleted=true`

**`service/tags/tags_integration_test.go`** — add:
- `ListTagKeys` — already partially tested, ensure separate subtest
- `ListTagValues` — already partially tested
- `ListTagBindings` — **fix ScanTagBindings first** (add `origin` column to scan)

**`service/storage/integration_test.go`** — add:
- `ListEndpoints` — already partially tested, verify as separate subtest
- `ListAgents` — create gateway, connect agent (or insert directly), list

This also covers `filter.Query`, all `filter.Scan*` functions, and `filter.ParseOrderBy`.

**After this phase**: services should jump to 75-85%, filter to ~60%.

### Phase 2: Server InternalHooks

All `InternalHooks` methods are 0% covered. They are HTTP handlers testable with `httptest`, `MockQuerier`, and a mock `authn.Service`.

Create **`internal/server/internal_hooks_test.go`** with `//go:build dev` tag:

```go
type mockAuthService struct{ mock.Mock }
func (m *mockAuthService) VerifyToken(ctx context.Context, token string) (*authn.Identity, error) { ... }
func (m *mockAuthService) CreateCustomToken(ctx context.Context, uid string) (string, error) { ... }
func (m *mockAuthService) CreateTenant(ctx context.Context, displayName string) (string, error) { ... }
func (m *mockAuthService) DeleteTenant(ctx context.Context, tenantID string) error { ... }
```

Tests:
- `TestNewInternalHooks` — constructor returns non-nil
- `TestRegister` — verify routes registered (call each path, expect non-404)
- `TestSyncAccount_Success` — POST valid JSON → 200, mock UpsertAccount called
- `TestSyncAccount_InvalidJSON` — bad body → 400
- `TestSyncAccount_MissingUID` — empty firebase_uid → 400
- `TestSyncAccount_DBError` — UpsertAccount returns error → 500
- `TestExchangeToken_Success` — valid bearer → VerifyToken + CreateCustomToken, return JSON with custom_token
- `TestExchangeToken_MissingHeader` — no Authorization → 401
- `TestExchangeToken_InvalidToken` — VerifyToken returns error → 401
- `TestExchangeToken_CustomTokenError` — CreateCustomToken fails → 500
- `TestDepositToken_Success` — valid token → VerifyToken + CreateAuthTokenCode, return code
- `TestDepositToken_InvalidToken` — VerifyToken fails → 401
- `TestDepositToken_EmptyBody` — missing id_token → 400
- `TestConsumeToken_Success` — valid code → ConsumeAuthTokenCode, return id_token
- `TestConsumeToken_InvalidCode` — bad UUID → 400
- `TestConsumeToken_ExpiredCode` — ConsumeAuthTokenCode returns error → 401
- `TestRateLimit_Allow` — under limit → handler called
- `TestRateLimit_Block` — over limit → 429
- `TestRequireSecret_Valid` — correct secret → handler called
- `TestRequireSecret_Invalid` — wrong secret → 401
- `TestIPRateLimiter` — newIPRateLimiter + allow behavior

**After this phase**: `server` should jump from 41% to ~75%.

### Phase 3: Storage AgentService + helper functions

The `agent_service.go` `Connect` method is complex bidi streaming. Break it into testable pieces.

**Unit tests** (create `internal/service/storage/agent_service_test.go`):

Pure functions (no mocks needed):
- `TestBuildEndpointConfigs_S3` — S3 config endpoint → correct proto
- `TestBuildEndpointConfigs_Filesystem` — filesystem config → correct proto
- `TestBuildEndpointConfigs_UnknownType` — unknown type → error
- `TestParseEndpointConfig_S3` — with access key, without
- `TestParseEndpointConfig_Filesystem` — path field
- `TestParseEndpointConfig_UnknownType` — error
- `TestAccessKeyID_Nil` — nil access key → empty string
- `TestSecretAccessKey_Nil` — nil → empty string
- `TestMintSessionJWT` — construct JWT, verify 3 parts, verify HMAC signature, decode claims

Mock-based tests for remaining gateways methods:
- `TestUpdateStorageGateway_WithMask` — display_name, ip_addresses, target_version, annotations
- `TestUpdateStorageGateway_NoMask` — full update
- `TestGetUninstallScript_Success` — mock chain, verify script content
- `TestCreateStorageSession` — mock ConnectionManager.SendToAll, verify JWT set-cookie header

**Bidi stream `Connect`** — create mock server stream:
```go
type mockConnectStream struct {
    grpc.ServerStream
    ctx      context.Context
    recvMsgs []*agentv1.AgentMessage
    sentMsgs []*agentv1.ControlMessage
    recvIdx  int
    mu       sync.Mutex
}
```

- `TestConnect_FullHandshake` — send Handshake with valid token → receive HandshakeAck
- `TestConnect_InvalidFirstMessage` — send Heartbeat first → InvalidArgument
- `TestConnect_BadToken` — unknown registration token → Unauthenticated
- `TestConnect_HeartbeatUpdate` — after handshake, send heartbeat → verify UpdateStorageAgentHeartbeat called
- `TestConnect_Disconnect` — stream ends → agent state DISCONNECTED, gateway state checked

**After this phase**: `service/storage` should jump from 55% to ~80%.

### Phase 4: Filter transpiler edge cases

Read `internal/filter/transpiler.go` and existing `transpiler_test.go`.

Add tests for uncovered transpiler paths:
- `TestTranspileTimestamp` — filter expression with timestamp comparison
- `TestTranspileConst` — string, int, float, bool constant values
- `TestTranspileSelect` — select expressions (field.subfield)
- `TestTranspileBinary_AND` — `a = 1 AND b = 2`
- `TestTranspileBinary_OR` — `a = 1 OR b = 2`
- `TestTranspileNot` — `NOT (a = 1)`
- `TestTranspileExpr_UnknownType` — unrecognized expression type
- `TestConstToValue_AllTypes` — all constant types
- `TestExpandBareLiteral_AllTypes` — all bare literal expansions

Also test filter declarations:
- `TestTagValueFilter` — non-nil, correct table
- `TestTagBindingFilter` — non-nil
- `TestApiKeyFilter` — non-nil

And `ParseOrderBy`:
- `TestParseOrderBy_Valid` — `"create_time desc"`, `"display_name asc"`
- `TestParseOrderBy_Default` — empty string → default order
- `TestParseOrderBy_Invalid` — unknown field → error

**After this phase**: `filter` should jump from 41% to ~75%.

### Phase 5: Storageagent remaining paths

Read existing `stream_test.go`, `endpoints_test.go`, `http_test.go`.

**`storageagent/http.go` `ServeHTTP` prod auth path** — create `internal/storageagent/http_auth_test.go` (WITHOUT dev build tag, or use a test that exercises the non-dev path):

Actually, since `devSkipAuth` is a const selected at build time, the auth path in `ServeHTTP` can only be tested by building without `-tags dev`. Create a separate test file without the dev tag:
- `TestServeHTTP_MissingCookie` — no pivox_session cookie → 401
- `TestServeHTTP_InvalidJWT` — bad JWT in cookie → 401
- `TestServeHTTP_ValidJWT_Authorized` — valid JWT + session authorizes path → proxy to endpoint
- `TestServeHTTP_ValidJWT_Forbidden` — valid JWT but session doesn't authorize path → 403

**`storageagent/endpoints.go`**:
- `TestEndpointStore_Update_Filesystem` — update with filesystem config
- `TestServeFilesystem_PathTraversal` — `../../etc/passwd` → 403
- `TestServeFilesystem_Directory` — path is a directory → 404
- `TestServeFilesystem_NotFound` — file doesn't exist → 404
- `TestServeFilesystem_Success` — serve real file from temp dir

**`storageagent/session.go`**:
- `TestStartCleanup` — context cancel stops the goroutine

Skip: `serveS3`, `newS3Client`, `Connect` (agent-side), `ListenAndServe` — these need real S3/gRPC/network.

**After this phase**: `storageagent` should reach ~80%.

### Phase 6: Gap sweep

Run full coverage including integration tests:

```bash
go test -tags dev ./internal/... -coverprofile=coverage.out -count=1 -timeout 300s
go tool cover -func=coverage.out | grep -v 'pkg/gen/' | grep -v 'db/generated' | grep -v 'testutil' | grep -v '100.0%' | sed 's|github.com/dashkan/pivox/||'
```

For every function below 90%, write a targeted test hitting the uncovered branch. Common patterns:
- Error paths (org not found, project not found)
- Name parsing with invalid formats
- Field mask with specific paths not yet tested
- Constructor functions (trivial but show as 0%)

**Target**: every hand-written package at 90%+.

## Permanently Excluded (can't unit test)

| Function | Why |
|---|---|
| `storageagent.Connect` | Agent-side gRPC dial + full connection lifecycle |
| `storageagent.serveS3` | Needs real S3/minio backend |
| `storageagent.newS3Client` | Needs real S3/minio backend |
| `storageagent.ListenAndServe` | Binds network port |
| `storageagent.version` | Returns build-time constant |
| ~~`firebase.*`~~ | Removed — the Firebase SDK wrapper was deleted in the Keycloak migration; auth is now `internal/oidc` (JWKS-verified), tested directly |
| `internal/db/generated/*` | sqlc-generated — exercised via integration tests |
| `internal/pkg/gen/*` | protoc-generated gRPC/proto code |

## Run Commands

```bash
# Bring up the docker-compose Postgres + rustfs stack (idempotent).
make test-up

# Full suite (the canonical entry point).
make test

# Race detector against a single package.
go test -race ./internal/<pkg>/...

# Coverage report.
go test ./internal/... -coverprofile=coverage.out -count=1
go tool cover -func=coverage.out | grep -v 'pkg/gen/' | grep -v 'db/generated' | grep -v 'testutil'

# HTML coverage report.
go tool cover -html=coverage.out -o coverage.html
```
