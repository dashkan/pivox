package permission

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestRoleGrants_MatchMatrix guards the generated RoleGrants slice-map
// against drift from the matrix bool-map. Both are emitted by
// cmd/gen-permissions from permissions.yaml, but they feed two different
// resolution paths: the interceptor resolves in-process via Has()/matrix,
// while org bootstrap + the dev seed materialize RoleGrants into the
// role_permissions table for SQL-side checks (e.g. the membership-scoped
// operations list). If the two ever disagree, those paths would grant
// different permissions for the same role.
func TestRoleGrants_MatchMatrix(t *testing.T) {
	assert.Len(t, RoleGrants, len(matrix), "RoleGrants and matrix cover different role sets")

	for role, perms := range matrix {
		want := make([]string, 0, len(perms))
		for p := range perms {
			want = append(want, p)
		}
		sort.Strings(want)

		got := append([]string(nil), RoleGrants[role]...)
		sort.Strings(got)

		assert.Equal(t, want, got, "role %q grants drifted between matrix and RoleGrants", role)
	}
}
