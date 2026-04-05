# Test Coverage Plan — Go Codebase

## Goal

100% test coverage for all hand-written Go code. Generated code (`internal/db/generated/`, `internal/pkg/gen/`) and external SDK wrappers (`internal/firebase/`) are excluded from the target — they're exercised indirectly through integration tests.

## Current State

Last updated: 2026-04-04

| Package | Coverage | Status |
|---|---|---|
| `internal/apierr` | **100%** | ✅ Done |
| `internal/convert` | **100%** | ✅ Done |
| `internal/crypto` | **100%** | ✅ Done |
| `internal/agentstream` | **96.3%** | Near-complete |
| `internal/resource` | **95.0%** | Near-complete |
| `internal/service/operations` | **88.9%** | Near-complete |
| `internal/iam` | **87.3%** | Near-complete |
| `internal/lro` | **83.7%** | Good |
| `internal/service/requests` | **67.9%** | In progress |
| `internal/service/assets` | **67.8%** | In progress |
| `internal/service/apikeys` | **59.9%** | In progress |
| `internal/service/projects` | **59.8%** | In progress |
| `internal/service/tags` | **55.7%** | In progress |
| `internal/service/operations` | **48.1%** | In progress |
| `internal/filter` | **40.5%** | Needs work |
| `internal/server` | **40.8%** | Needs work |
| `internal/service/storage` | **38.0%** | Needs work |
| `internal/storageagent` | **33.9%** | Needs work |
| `internal/service/organizations` | **17.9%** | Needs work |
| `internal/authn` | NO TESTS | Interface-only (no logic) |
| `internal/config` | NO TESTS | Plain structs (no logic) |
| `internal/firebase` | NO TESTS | Excluded (SDK wrapper) |
| `cmd/pivox-cloud` | NO TESTS | Wiring code |
| `cmd/pivox-agent` | NO TESTS | Wiring code |

## Prerequisites (Done)

- [x] All services accept `db.Querier` interface (mockable)
- [x] `internal/testutil/db.go` — testcontainers Postgres (`pgvector/pgvector:pg18`) + pgvector type registration + migrations
- [x] `internal/testutil/grpc.go` — bufconn gRPC server helper
- [x] `internal/testutil/mocks/querier_mock.go` — full MockQuerier
- [x] `internal/authn/authn.go` — IDP-agnostic auth interface (replaces `*firebase.AuthService`)
- [x] `LROManager` interface in `operations/server.go` (mockable)
- [x] `TxBeginner` interface in `organizations/server.go` (mockable)
- [x] Dead `pool` fields removed from storage servers, LRO manager, requests server
- [x] `errors.Is(err, pgx.ErrNoRows)` across all sites
- [x] `lro.runWork` properly fails operations on marshal errors

## Important Notes

- **Always use `-tags dev`** for test runs — activates `NoOpEncryptor` and `devSkipAuth`.
- **Read production code before writing tests** — don't assume structure from this doc.
- **Integration tests use `testing.Short()` guard** — run `-short` to skip them when Docker isn't available.
- **testcontainers image**: `pgvector/pgvector:pg18` (not plain `postgres:18`).
- **Mock pattern**: `MockQuerier` from `internal/testutil/mocks/`, `testify/mock` for custom mocks.

## Remaining Work

### Phase A: Integration tests for List endpoints

All `List*` methods use `filter.Query()` which builds raw SQL — can only be tested with a real database. These are the biggest uncovered paths in every service.

Run integration tests with testcontainers + bufconn. Each test creates prerequisite data, then calls the List RPC.

| Package | Function | Test |
|---|---|---|
| `service/apikeys` | `ListKeys` | Create 3 keys, list with parent, verify count + pagination |
| `service/projects` | `ListProjects` | Create 2 projects, list, verify |
| `service/organizations` | `ListOrganizations` | Already partially covered — verify pagination |
| `service/requests` | `ListRequests` | Create requests, list with/without `show_deleted` |
| `service/assets` | `ListAssets` | Create assets, list with/without `show_deleted` |
| `service/tags` | `ListTagKeys`, `ListTagValues`, `ListTagBindings` | Create chain, list each level |
| `service/storage` | `ListEndpoints` | Create gateway + endpoints, list |

This also covers `filter.Query`, `filter.Scan*`, and `filter.ParseOrderBy` indirectly.

**Bug to fix first**: `filter/scan.go` `ScanTagBindings` is missing the `origin` column.

### Phase B: Organizations CreateOrganization (integration)

The `CreateOrganization` method uses `db.New(tx)` internally which bypasses the mock querier. Must be tested with real Postgres.

| Test | What |
|---|---|
| `TestIntegration_CreateOrganization_Success` | Create org with noopAuthService, verify tenant ID set |
| `TestIntegration_CreateOrganization_DuplicateName` | Same name twice → AlreadyExists |
| `TestIntegration_CreateOrganization_TenantFailure` | Auth service returns error → org not created (tx rolled back) |

### Phase C: Server InternalHooks

The `InternalHooks` endpoints are HTTP handlers testable with `httptest`. They use `db.Querier` (mockable) and `authn.Service` (mockable).

| Function | Test |
|---|---|
| `NewInternalHooks` (dev) | Constructor with mock auth + mock querier |
| `Register` | Verify all routes registered on mux |
| `syncAccount` | Valid request → upsert account; invalid JSON → 400; missing UID → 400 |
| `exchangeToken` | Valid bearer → verify + create custom token; missing header → 401; invalid token → 401 |
| `depositToken` | Valid token → create code; invalid token → 401; empty body → 400 |
| `consumeToken` | Valid code → return ID token; invalid code → 401; bad UUID → 400 |
| `rateLimit` | Under limit → pass; over limit → 429 |
| `requireSecret` (dev) | Correct secret → pass; wrong secret → 401 |
| `ipRateLimiter` | `allow` returns true/false based on rate; `newIPRateLimiter` constructor |

### Phase D: Storage service — AgentService bidi stream

The `Connect` method is a bidirectional streaming RPC. Test with mock `grpc.ServerStream` and mock querier.

| Test | What |
|---|---|
| `TestConnect_Handshake` | Mock stream: send Handshake → receive HandshakeAck with endpoints |
| `TestConnect_InvalidFirstMessage` | Send heartbeat as first message → InvalidArgument |
| `TestConnect_InvalidToken` | Unknown registration token → Unauthenticated |
| `TestConnect_Heartbeat` | After handshake, send heartbeat → verify DB heartbeat update |
| `TestConnect_Disconnect` | Stream closes → agent state set to DISCONNECTED |
| `TestConnect_GatewayActivation` | First agent connects to PROVISIONING gateway → state becomes ACTIVE |
| `TestBuildEndpointConfigs` | S3 config, filesystem config, unknown type → error |
| `TestParseEndpointConfig` | S3, filesystem, unknown type |
| `TestAuditMessage` | Verify audit record created with redacted payload |
| `TestMintSessionJWT` | Verify JWT structure, signature, claims |
| `TestCreateStorageSession` | Session grant sent to all agents, JWT returned |
| `TestUpdateStorageGateway` | Field mask paths: display_name, ip_addresses, target_version, annotations |
| `TestGetUninstallScript` | Verify script content |

### Phase E: Storage agent stream + endpoints

| Function | Test |
|---|---|
| `NewStream` | Constructor |
| `Handshake` | Mock bidi client stream — send handshake, receive ack via roundTrip |
| `SendHeartbeat` / `SendTelemetry` / `SendEndpointHealth` / `SendUpgradeStatus` | Fire-and-forget send |
| `roundTrip` | Send with correlation ID, receive matching response; timeout |
| `send` | Basic send |
| `ReceiveLoop` | Route correlated responses to pending channels; dispatch server messages |
| `handleServerMessage` | ConfigUpdate → endpoints.Update + denied.Update; SessionGrant → sessions.Grant; SessionRevoke → sessions.Revoke; DrainRequest/CertDelivery/UpgradeRequest/ServerHeartbeat → logged |
| `StartCleanup` | Context cancellation stops ticker |
| `EndpointStore.Update` | Filesystem config (no S3 needed for test) |
| `EndpointStore.ServeFile` | Route to correct endpoint; 404 for missing endpoint; 404 for bad path |
| `serveFilesystem` | Serve file from temp dir; path traversal blocked; directory returns 404 |

Note: `serveS3` and `newS3Client` require minio — skip unless minio testcontainer is set up. `Connect` (agent-side) requires a real gRPC server — skip or test as E2E.

### Phase F: Filter package

| Function | Test |
|---|---|
| `TagValueFilter`, `TagBindingFilter`, `ApiKeyFilter` | Constructor (verify non-nil) |
| `ParseOrderBy` | Valid order strings, invalid, empty |
| `transpileTimestamp` | Timestamp filter expressions |
| `transpileConst` | String, int, float, bool constants |
| `transpileSelect` | Select expressions |
| `transpileBinary` | Remaining binary operators (AND, OR) |

`Query` + all `Scan*` functions are covered by Phase A integration tests.

### Phase G: Gap sweep

After all phases, run full coverage and write targeted tests for any remaining uncovered lines:

```bash
go test -tags dev ./internal/... -coverprofile=coverage.out -timeout 300s
go tool cover -func=coverage.out | grep -v 'pkg/gen/' | grep -v 'db/generated' | grep -v 'testutil' | grep -v '100.0%'
```

Target: every hand-written package at 90%+.

### Phase H: cmd/ packages (optional)

`cmd/pivox-cloud/main.go` and `cmd/pivox-agent/` are CLI wiring — `cobra` setup, flag parsing, service construction. Testing them means E2E tests (start the server, make requests). This is valuable but separate from unit/integration coverage.

| Test | What |
|---|---|
| `TestServe_MissingDB` | Invalid database URL → error |
| `TestEnvOrDefault` | Env set → env value; env empty → default |
| `TestMust` | Returns string, ignores error |

## Excluded from Coverage Target

| Package | Reason |
|---|---|
| `internal/db/generated/*.sql.go` | sqlc-generated — exercised indirectly via integration tests |
| `internal/db/generated/models.go` | sqlc-generated Scan/Value methods — exercised via DB round-trips |
| `internal/pkg/gen/` | protoc-generated gRPC/proto code |
| `internal/firebase/` | Thin SDK wrapper — tested via `authn.Service` mock everywhere |
| `internal/testutil/` | Test infrastructure — not production code |

## Known Production Bugs (fix before/during testing)

- [ ] `filter/scan.go` `ScanTagBindings` — missing `origin` column in row scan (9 columns, scans 8)
- [ ] `UndeleteKey` / `UndeleteProject` — queries filter `delete_time IS NULL`, can't find soft-deleted rows
- [ ] `pgvector.Vector` NULL handling — non-pointer type panics (workaround in testutil, needs schema fix)

## Run Commands

```bash
# Unit tests only (fast, no Docker)
go test -tags dev ./internal/... -short -count=1 -race

# Full suite including integration tests (needs Docker)
go test -tags dev ./internal/... -count=1 -race -timeout 300s

# Coverage report
go test -tags dev ./internal/... -coverprofile=coverage.out -timeout 300s
go tool cover -func=coverage.out | grep -v 'pkg/gen/' | grep -v 'db/generated' | tail -1

# Per-package coverage
go tool cover -func=coverage.out | grep -v 'pkg/gen/' | grep -v 'db/generated' | grep -v 'testutil'

# HTML coverage report
go tool cover -html=coverage.out -o coverage.html
```
