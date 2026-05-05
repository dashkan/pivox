# Test rebuild — #71 Phase 2

Specs for the grpcharness-based rewrite of every `MockQuerier`-using
package. Each file lists the *behaviors* the deleted tests intended
to verify, distilled into a checklist for the rewrite. Pure call-
shape garbage gets struck through and dropped.

This folder is **temporary** — delete it once #71 closes.

## Status snapshot

| Package | Spec | MockQuerier tests | grpcharness tests | Rewrite status |
|---|---|---|---|---|
| `internal/service/organizations` | [organizations.md](organizations.md) | 7 files | 5 files | spec drafted |
| `internal/service/spaces` | [spaces.md](spaces.md) | 2 files | 1 file | spec drafted |
| `internal/service/apikeys` | [apikeys.md](apikeys.md) | 1 file | 1 file | spec drafted |
| `internal/service/assets` | [assets.md](assets.md) | 1 file | 1 file | spec drafted |
| `internal/service/requests` | [requests.md](requests.md) | 1 file | 1 file | spec drafted |
| `internal/service/operations` | [operations.md](operations.md) | 1 file | 0 files | spec drafted |
| `internal/service/iam` | [iam.md](iam.md) | 2 files | 1 file | spec drafted |
| `internal/service/storage` | [storage.md](storage.md) | 4 files | 1 file | spec drafted |
| `internal/storageagent` | [storageagent.md](storageagent.md) | 1 file | 0 files | spec drafted |
| `internal/workers` | [workers.md](workers.md) | 5 files | 0 files | spec drafted |

`internal/service/tags` is tracked separately under #73 — handlers
need rewriting before tests can be restored.

## Per-spec format

- `[x]` — already covered by an existing grpcharness file
- `[ ]` — needs new test in the rewrite
- `~~strikethrough~~` — old test was call-shape garbage, deliberately
  dropped

The bar is "catches real bugs in production-shape code." Coverage %
is not the goal. When in doubt: write fewer tests, not more.

## Shared rules

- Every new test goes through `grpcharness.New(t, ...)` (or pure-
  function tests for parsers/validators/marshallers). No new
  MockQuerier code.
- Use the harness helpers: `WithOrganizationsServer`,
  `WithSpacesServer`, `SeedOwnedOrg`, `SeedOwnedSpace`. Add new
  helpers to grpcharness when the (N+1)th copy threatens.
- Each rewrite includes the doc update where the new pattern
  exposes a rule worth pinning (CLAUDE.md or the per-stack
  AGENTS.md).
