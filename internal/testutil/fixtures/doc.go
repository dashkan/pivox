// Package fixtures provides typed factories for `db.X` test rows.
// Each constructor returns a default-populated value with stable
// (deterministic) IDs and timestamps; options compose on top.
//
// Usage:
//
//	fixtures.Org()                                     // default Acme org
//	fixtures.Org(fixtures.OrgID(myUUID))               // with custom ID
//	fixtures.Org(fixtures.OrgName("widgets"),          // composed
//	    fixtures.OrgState(db.ResourceStateDELETEREQUESTED))
//
// Conventions:
//
//   - Constructor name: `<Type>()` — e.g., `Org`, `Operation`.
//   - Option name: `<Field>(value)` — e.g., `OrgID`, `OrgName`.
//   - Defaults stable across calls (no time.Now(), no uuid.New()).
//   - Time default: 2026-01-01T00:00:00Z.
//   - UUID default: well-known per-type, prefix 00000000-0000-7000-8000.
//
// See `internal/testutil/AGENTS.md` for the broader policy. Fixtures
// are a transitional helper for legacy `MockQuerier`-based tests
// during the #71 migration; new service-layer tests should use
// `grpcharness` instead.
package fixtures
