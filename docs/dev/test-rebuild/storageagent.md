# `internal/storageagent` — test rebuild spec

The `storageagent` binary is the on-prem agent that connects back to
Cloud Controller via the AgentService streaming RPC. Tests here
exercise both the agent's HTTP serve surface and the Connect
client.

## Existing coverage (kept)

- `cache_test.go` — pure-function tests for the memory cache
- `denied_test.go` — denied-patterns matcher
- `endpoint_store_test.go` — endpoint resolver
- `connect_test.go` (post-fix) — TestVersion, TestListenAndServe,
  TestConnect_FullHandshake, TestConnect_InvalidToken,
  TestConnect_ContextCancelDuringHeartbeat,
  TestConnect_ReconnectingAgent, TestConnect_TLS,
  TestConnect_InsecureDialSuccess

## What needs to change

The Connect tests are MockQuerier-based today, even after the
interceptor fix. They work but they're the legacy pattern. Two
options:

1. **Convert in place.** Connect from the agent side wants a *real
   AgentService server*; it can use the production Cloud Controller
   via grpcharness's existing Pool/Queries. The mock setup just
   becomes seeding real rows.
2. **Leave the MockQuerier tests for now**, since the AgentService
   tests aren't directly part of the public-API surface — they're
   integration tests of the stream protocol against a server.

Recommendation: option 1, mirroring the storage service rebuild.
The MockQuerier in the agent tests is an extra coupling point that
will rot.

## Behaviors to preserve

- [x] Version() returns the build version
- [x] ListenAndServe rejects invalid addresses
- [x] ListenAndServe binds and serves
- [x] Connect: full handshake completes (token → gateway → agent
  row created → endpoints delivered)
- [x] Connect: invalid token → Unauthenticated, retry-loop respects
  it
- [x] Connect: context cancellation closes the stream cleanly
- [x] Connect: reconnecting agent updates an existing row instead
  of duplicating
- [x] Connect: TLS path (with cert) succeeds
- [x] Connect: insecure dial path succeeds (dev mode)
- [ ] Connect: heartbeat tick updates `last_heartbeat_at`
- [ ] Connect: server-pushed config update is observed by the
  agent
- [ ] Connect: disconnect handler updates state in DB

## Drop list

- ~~`testhelpers_test.go`~~ — review what's there; extract anything
  reusable, drop the rest

## Shape of the rewrite

- Convert `connect_test.go` to use grpcharness with a registered
  AgentService server. Mock setup becomes Pool seeding via raw
  Queries. The MockQuerier tests then drop in favor of behaviors
  observed via real DB state.

  This is the bigger lift; doable when storage service rebuild
  lands (since they share the AgentService surface).
