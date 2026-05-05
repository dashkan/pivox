# `internal/service/storage` — test rebuild spec

Largest deletion target — 4 unit files (~ 1500 lines). The
service has three public surfaces (StorageGateways, Endpoints,
Agents) plus the internal AgentService streaming RPC.

## Existing grpcharness coverage (kept)

- `integration_test.go` — gateway + endpoint lifecycle, install
  script, list/get, delete

## StorageGateways

- [x] CreateStorageGateway happy path (covered by integration)
- [x] GetStorageGateway happy path
- [x] DeleteStorageGateway happy path
- [x] GetInstallScript with cache size + custom port + bind address
  (#74 workaround)
- [ ] Auto-generated `StorageGatewayId` when not supplied
- [ ] Permission gate: `storage.gateways.create`
- [ ] CreateStorageGateway with cross-org parent → NotFound
- [ ] DELETE_REQUESTED parent org → FailedPrecondition
- [ ] UpdateStorageGateway: field-mask matrix (display_name,
  ip_addresses, annotations)
- [ ] DeleteStorageGateway: still has connected agents → ?? decide
  the contract (today probably succeeds; might want to refuse
  with FailedPrecondition until agents drain)
- [ ] RotateRegistrationToken: returns new token, old token stops
  authenticating subsequent agent connects
- [ ] GetUninstallScript: contains the gateway-specific tear-down
  command

## Endpoints

- [x] CreateEndpoint Filesystem (covered)
- [ ] CreateEndpoint S3 with access key — verify access_key is
  stored encrypted (not plaintext) — security boundary, worth a
  direct DB-read assertion
- [ ] CreateEndpoint S3 without access key (instance-profile mode)
- [ ] UpdateEndpoint: field-mask honors configuration vs annotations
  vs cache-config independently. The deleted tests had ~10 cases
  for this; one table-driven test is enough.
- [ ] UpdateEndpoint: configuration replacement (S3 → Filesystem)
- [ ] CacheEviction LFU vs LRU + cache enable/disable
- [ ] DeleteEndpoint: removes endpoint cleanly
- [ ] Endpoint name shape: nested under gateway + org

## Agents (read-only public RPCs)

- [ ] GetAgent returns row by name
- [ ] ListAgents returns agents under a gateway
- [ ] DrainAgent: marks state=DRAINING, observed via subsequent
  ListAgents
- [ ] RemoveAgent: removes the row, agent's stream closes
- [ ] All four: cross-org / unknown gateway → NotFound
- [ ] Permission gate: `storage.agents.read` for Get/List;
  `storage.agents.update` for Drain/Remove

## AgentService (streaming RPC, not exposed publicly)

The deleted `agent_service_unit_test.go` had ~30 tests on the
Connect handshake/heartbeat/disconnect flow. This is exercised
by `internal/storageagent/connect_test.go` end-to-end (TCP +
real interceptor), which we just fixed. The unit tests that
mock-stubbed every error branch are mostly redundant.

The behaviors worth keeping (in connect_test.go's grpcharness-
shaped successor):

- [x] FullHandshake (existing)
- [x] InvalidToken rejection (existing)
- [x] ContextCancelDuringHeartbeat (existing)
- [x] ReconnectingAgent (existing)
- [ ] First message is not a Handshake → InvalidArgument and
  stream closes
- [ ] HandshakeAck contains the gateway's endpoint configurations
  (cache state, denied patterns)
- [ ] Server-pushed config update reaches the agent stream
- [ ] Heartbeat updates `last_heartbeat_at` on the row
- [ ] Disconnect: agent state → DISCONNECTED, last gateway agent
  disconnecting flips gateway state → OFFLINE (the
  CountConnectedStorageAgentsByGateway==0 path)

## Pure-function tests

- [ ] `parseEndpointConfig` for S3 / Filesystem / unknown / bad
  JSON — small pure-function file, no harness needed
- [ ] `buildEndpointConfigs` happy path + empty + per-endpoint
  error propagation
- [ ] `EndpointConfigJSON.AccessKey` getter shape (round-trip
  encryption + redaction)

## Drop list

- ~~Every `*_DBError` test in agents/endpoints/gateways unit tests~~ —
  mock theater
- ~~`*_NotFound` matrix~~ — happy-path tests with bad inputs cover
  the same surface organically
- ~~`*_InvalidName` matrix~~ — protovalidate
- ~~`TestUnit_*_Unimplemented`~~ — pinned-future-rpc tests are
  worse than no test (they "pass" while the feature doesn't exist)
- ~~`TestParseStorageGatewayName_Invalid`~~ — fold into the one
  Get-with-bad-name happy-path test

## Shape of the rewrite

- `integration_test.go` — extend with permission gates, S3
  encryption assertion, RotateRegistrationToken behavior,
  field-mask matrix (one table-driven test covers it)
- `agents_e2e_test.go` — **NEW** file for the public Agents RPCs
- `endpoint_config_test.go` — **NEW** small pure-function file
  for the JSON parsers
- AgentService extra cases land in `internal/storageagent/connect_test.go`
