# Testing Strategy

## Principles

- **TDD.** Write tests first, then implementation. No exceptions for new code.
- **Each stack uses its own test tooling.** No cross-stack test abstraction layers — Go uses `go test`, the Engine uses `cargo test` / Google Test.
- **Tests run before every commit.** CI enforces this. Local dev should too.

## Framework Reference

### Go (Cloud Controller, Worker Process, Storage Agent)

**Framework:** `go test` (standard library) + `testify/require` for assertions. **No** mocking framework for new code unless explicitly called for below.

**Run:** `make test` (brings up the docker-compose Postgres + rustfs stack, then runs the suite). `go test -race ./...` for any change in concurrency-relevant code.

#### Choose the right layer

Decide what bug the test is meant to catch *before* writing it. The wrong layer means tests pass while bugs ship.

| Layer | When to use | Cost | Catches |
|---|---|---|---|
| **Integration via `grpcharness`** | Service-layer / handler tests | ~50-100ms each | Real RPC flow including auth/membership/permission/audit interceptors; SQL correctness; FK violations; River enqueue atomicity |
| **DB integration via `testutil.SetupTestDB`** | DB queries, migrations, raw SQL paths | ~50ms each | SQL correctness, schema drift, FK behavior, transaction semantics |
| **Pure-function unit** | Validators, parsers, transformers, error mappers, anything without DB/RPC/network | sub-ms | Algorithm correctness, edge cases in pure logic |
| **`pgxmock` connection-level mock** | Hard-to-induce DB errors only (`ErrTxClosed` mid-tx, deadlock retry, pool exhaustion) | ~ms | Specific failure modes integration can't reproduce |
| **`rivertest.RequireInsertedTx`** | Asserting handler enqueued a River job | included with integration | Job enqueue + atomicity with `operations` row |

**Default for service-layer behavior is integration via `grpcharness`.** It runs the real gRPC server with the real interceptor chain (auth, membership, permission, audit) — only the token verifier (in production, the Keycloak OIDC verifier: `authn.Service` / `internal/oidc`) is stubbed via `testAuthService`. Tests assert RPC outcomes against the real DB and can pin caller identity via `Harness.SetCaller`.

#### Examples

**Integration test (the canonical shape):**

```go
func TestCreateOrganization_Success(t *testing.T) {
    h := grpcharness.New(t)
    defer h.Close()

    h.SeedIdentity(t, "alice")
    h.SetCaller(grpcharness.CallerFromUID("alice"))

    op, err := h.OrganizationsClient().CreateOrganization(t.Context(),
        &apiv1.CreateOrganizationRequest{
            Organization: &apiv1.Organization{Name: "organizations/acme", DisplayName: "Acme"},
        })
    require.NoError(t, err)

    // Real DB assertion:
    org, err := h.Queries().GetOrganizationByName(t.Context(), "organizations/acme")
    require.NoError(t, err)
    assert.Equal(t, "Acme", org.DisplayName)

    // Real River assertion (if the handler enqueues a job):
    rivertest.RequireInsertedTx[*riverpgxv5.Driver](t.Context(), t, h.Tx(),
        CreateOrgArgs{OrgID: org.ID}, nil)

    // Caller identity threaded through audit:
    assert.Equal(t, "alice", op.Metadata.CreatedBy)
}
```

This single test exercises auth → membership → permission → audit → handler logic → SQL → River enqueue, plus their composition. It replaces ~5-10 mock tests' worth of partial assertions with one real test of the production path.

**Pure-function test (validators, parsers, transformers):**

```go
func TestParseOperationName(t *testing.T) {
    cases := []struct{
        name, input string
        wantID uuid.UUID
        wantErr bool
    }{
        {"root-scoped", "operations/01abc...", uuidFromStr("01abc..."), false},
        {"org-parented", "organizations/acme/operations/01abc...", uuidFromStr("01abc..."), false},
        {"missing operations segment", "organizations/acme/01abc...", uuid.Nil, true},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            got, err := parseOperationName(tc.input)
            if tc.wantErr {
                require.Error(t, err)
                return
            }
            require.NoError(t, err)
            assert.Equal(t, tc.wantID, got)
        })
    }
}
```

No mock library, no DB. Just call the function and assert. Most useful for `internal/filter/`, `internal/resource/`, ID parsers, and similar.

**`pgxmock` for narrow error simulation:**

```go
func TestRetryOnErrTxClosed(t *testing.T) {
    mock, _ := pgxmock.NewPool()
    defer mock.Close()
    mock.ExpectBegin().WillReturnError(pgx.ErrTxClosed)
    // ... test the retry path
}
```

Use sparingly. If you can reproduce the error against a real DB, do that instead.

#### Anti-patterns

These get **refused** in code review:

- **Mocking `db.Querier`.** The querier mock was deleted in #71;
  service-layer tests go through `grpcharness` against a real DB
  (cloned from a per-process template, ~50ms per test).
- **`mockgen` / `mockery` for gRPC service interfaces.** Use
  `grpcharness` to dial the real server via bufconn. The full
  interceptor chain runs; tests assert real outcomes.
- **Stubbed interceptors.** The interceptor chain is the security
  boundary. Tests that stub it lie about coverage — "test passes"
  no longer means "production path passes."
- **Hand-rolled mocks for interfaces we already mockery-generate.**
  `authn.Service` is the only externally-controlled boundary; use
  `internal/testutil/authnmock.NewMockService(t)` (auto-registers
  `AssertExpectations` in `t.Cleanup`).
- **Call-shape assertions as the sole assertion.** "We called
  `qtx.GetOrg(orgID)`" duplicates what an integration test catches
  via the RPC response. Acceptable as one assertion in a richer
  test; not acceptable as the only point.

#### Helpers and fixtures

- **`internal/testutil/grpcharness/`** — bufconn gRPC server with
  the production interceptor chain. Canonical home for service-
  layer integration tests.
- **`internal/testutil/fixtures/`** — typed `db.X` row factories
  with deterministic defaults (`fixtures.Org()`, etc.) for tests
  that work directly against the DB.
- **`internal/testutil/authnmock/`** — mockery-generated
  `authn.Service` mock (regenerate via `make mocks`).
- **`internal/testutil/cryptotest/`** — round-tripping
  `crypto.Encryptor` for tests that need encryption to *happen*
  without the KMS round-trip. Distinguishes plaintext from
  ciphertext so accidental plaintext storage shows up.
- **`internal/testutil.SetupTestDB`** / **`SetupTestS3`** —
  per-test Postgres database (cloned from a shared template) and
  per-test S3 bucket. Cleanup auto-registers via `t.Cleanup`.

**Rule of three.** Two copies of an inline test pattern is
acceptable; the third triggers helper extraction.

### Rust (Engine)

**Framework:** `cargo test` (built-in)
**Style:** `#[cfg(test)]` modules in each file, integration tests in `tests/` directory
**Mocking:** Trait-based test doubles
**Run:** `cargo test`

```rust
#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn compositor_transparent_canvas() {
        let compositor = Compositor::new(1920, 1080);
        let frame = compositor.render(&[]);
        assert!(frame.pixels().iter().all(|&p| p == 0));
    }
}
```

### C++ (Engine plugins, CEF bridge)

**Framework:** Google Test (gtest) + Google Mock (gmock)
**Integration:** CMake `FetchContent` — no manual install
**Run:** `ctest` or direct binary execution

Google Test integrates via CMake `FetchContent`, so the Engine's C++
plugin and bridge code (e.g. the CEF bridge) is tested without a manual
gtest install.

### End-to-End Tests

E2E tests verify complete workflows across the full stack: Cloud
Controller / Playout Agent → engine.

#### Engine E2E

A gRPC test client (Go or Rust) sends playout commands to a running engine and verifies output:

```go
func TestEngine_PlayGraphicOverVideo(t *testing.T) {
    conn := connectToEngine(t)
    client := proto.NewPlayoutClient(conn)

    // Load video on layer 0
    _, err := client.VideoLoad(ctx, &proto.VideoLoadCommand{
        Channel: 0, Layer: 0, Path: "testdata/reference.mxf",
    })
    require.NoError(t, err)

    // Load graphic on layer 1
    _, err = client.Load(ctx, &proto.LoadCommand{
        Channel: 0, Layer: 1, Template: "testdata/lower-third.html",
    })
    require.NoError(t, err)

    // Take and capture NDI output frame
    _, err = client.Play(ctx, &proto.PlayCommand{Channel: 0, Layer: 1})
    require.NoError(t, err)

    frame := captureNDIFrame(t, "PIVOX-CH0")
    assertFrameMatchesReference(t, frame, "testdata/expected_composite.png", tolerance)
}
```

#### API E2E

Test the Cloud Controller and Playout Agent APIs end-to-end:

```go
func TestRundownWorkflow(t *testing.T) {
    // Create show
    show := createShow(t, "Test Show")

    // Create rundown with items
    rundown := createRundown(t, show.ID)
    addItem(t, rundown.ID, "lower-third", templateID, data)

    // Cue first item
    cue(t, rundown.ID, 0)
    status := getChannelStatus(t, 0)
    require.Equal(t, "cued", status.Layers[2].BackgroundState)

    // Take
    take(t, rundown.ID, 0)
    status = getChannelStatus(t, 0)
    require.Equal(t, "playing", status.Layers[2].ForegroundState)
}
```

## Test Organization

```
pivox-engine/
  ├── src/
  │   ├── compositor/
  │   │   ├── mod.rs
  │   │   └── tests.rs                   ← cargo test (inline)
  │   └── ...
  └── tests/                             ← cargo test (integration)

pivox-server/
  ├── internal/
  │   ├── playout/
  │   │   ├── controller.go
  │   │   └── controller_test.go         ← go test
  │   └── ...
  └── e2e/                               ← go test E2E
```

## CI Integration

Every PR runs:

| Stage | What | Framework | Platform |
|---|---|---|---|
| 1 | Engine unit + integration tests | cargo test | macOS + Linux |
| 2 | Go unit tests | go test | macOS + Linux |
| 3 | Engine E2E | gRPC harness | Linux (GPU runner) |
| 4 | API E2E | go test | Linux |

Unit-test stages run in parallel. E2E tests run after unit tests pass.
