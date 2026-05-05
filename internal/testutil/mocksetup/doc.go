// Package mocksetup wraps the highest-frequency MockQuerier
// configurations behind named helpers. Goal: when sqlc regenerates
// and changes a method's signature, you update the helper here, not
// every test that mocked it.
//
// Usage:
//
//	q := new(mocks.MockQuerier)
//	mocksetup.ExpectGetOrgByName(q, "acme", fixtures.Org())
//	mocksetup.ExpectGetOrgByNameNotFound(q, "missing")
//
// Conventions:
//
//   - Helper name: `Expect<Method>` for the happy path.
//   - `Expect<Method>NotFound` for the `pgx.ErrNoRows` branch.
//   - `Expect<Method>Error(..., err)` for arbitrary errors.
//   - First arg is always `*mocks.MockQuerier`; subsequent args mirror
//     the production call (the params struct or its individual fields).
//   - ctx is matched with `mock.Anything` — tests that need strict ctx
//     matching should set up the mock inline.
//
// **This package exists only to reduce churn during the migration in
// #71.** New service-layer tests should use `grpcharness` (real gRPC
// stack + real interceptor chain + real DB) instead of `MockQuerier`.
// As services migrate off the legacy pattern, the helpers they used
// get deleted alongside the tests.
//
// See `internal/testutil/AGENTS.md` for the broader policy.
package mocksetup
