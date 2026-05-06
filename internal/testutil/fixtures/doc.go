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
// are typed row factories for tests that work directly against the
// DB (filter, lro, anywhere a raw `db.X` is the system under test).
// Service-layer tests go through `grpcharness` and seed via
// harness-level helpers (`SeedOwnedOrg`, `SeedOwnedSpace`, etc.) —
// fixtures are not the right shape there.
package fixtures
