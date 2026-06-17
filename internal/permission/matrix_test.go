package permission

import (
	"bufio"
	"bytes"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

// Smoke tests for the role↔permission grants. The grant data is a flat
// data structure but it's load-bearing — it encodes the entire access-
// control surface of the v1 product. Each test pins a permission to a
// specific role's column so casual edits to the grants get caught.

// roleGrants reports whether a system role grants a permission per the
// generated RoleGrants data — the source that seeds role_permissions.
func roleGrants(role, perm string) bool {
	return slices.Contains(RoleGrants[role], perm)
}

func TestMatrix_OwnerHasDestructionClassPermissions(t *testing.T) {
	// Owner is the destruction-class tier. Permissions only owner has,
	// not admin: org delete, transfer ownership, SSO update, user delete.
	cases := []string{
		OrganizationsDelete,
		OrganizationsTransferOwnership,
		OrganizationsSsoConfigUpdate,
		UsersDelete,
	}
	for _, p := range cases {
		t.Run(p, func(t *testing.T) {
			if !roleGrants(RoleOwner, p) {
				t.Errorf("owner should have %q", p)
			}
			if roleGrants(RoleAdmin, p) {
				t.Errorf("admin should NOT have %q (destruction-class, owner-only)", p)
			}
		})
	}
}

func TestMatrix_AdminHasDayToDayManagement(t *testing.T) {
	// Admin can do most management ops short of destruction-class,
	// including approval/assignment workflow verbs that editors must
	// not have (admin-only request gating is a real RBAC invariant).
	cases := []string{
		OrganizationsUpdate,
		SpacesCreate,
		DomainsCreate,
		DomainsRead,
		DomainsDelete,
		OrganizationsSsoConfigRead,
		MembersCreate,
		MembersUpdate,
		MembersDelete,
		GroupsCreate,
		GroupsUpdate,
		GroupsDelete,
		ApiKeysCreate,
		InvitationsCreate,
		StorageGatewaysCreate,
		StorageGatewaysUpgrade,
		StorageAgentsDrain,
		StorageAgentsRemove,
		StorageEndpointsCreate,
		AssetsRequestsAssign,
		AssetsRequestsApprove,
		AssetsRequestsReject,
		AssetsRequestsCancel,
	}
	for _, p := range cases {
		t.Run(p, func(t *testing.T) {
			if !roleGrants(RoleAdmin, p) {
				t.Errorf("admin should have %q", p)
			}
		})
	}
}

func TestMatrix_EditorCanMutateContentNotIam(t *testing.T) {
	// Editor: read all + mutate content (assets, requests, line items).
	// No IAM, no domains, no SSO, no API keys, no storage management.
	canDo := []string{
		AssetsAssetsCreate,
		AssetsAssetsUpdate,
		AssetsAssetsDelete,
		AssetsAssetsImport,
		AssetsRequestsCreate,
		AssetsRequestsUpdate,
		AssetsRequestsDeliver,
		AssetsLineItemsCreate,
		AiConversationsCreate,
	}
	cantDo := []string{
		OrganizationsUpdate,
		SpacesCreate,
		DomainsCreate,
		MembersCreate,
		GroupsCreate,
		ApiKeysCreate,
		InvitationsCreate,
		StorageGatewaysCreate,
		OrganizationsSsoConfigRead,
		// Workflow gating: editors create/update content but admins
		// own assignment + approval lifecycle.
		AssetsRequestsAssign,
		AssetsRequestsApprove,
		AssetsRequestsReject,
		AssetsRequestsCancel,
	}
	for _, p := range canDo {
		t.Run("can/"+p, func(t *testing.T) {
			if !roleGrants(RoleEditor, p) {
				t.Errorf("editor should have %q", p)
			}
		})
	}
	for _, p := range cantDo {
		t.Run("cant/"+p, func(t *testing.T) {
			if roleGrants(RoleEditor, p) {
				t.Errorf("editor should NOT have %q", p)
			}
		})
	}
}

func TestMatrix_ViewerIsReadOnly(t *testing.T) {
	canDo := []string{
		OrganizationsRead,
		UsersRead,
		GroupsRead,
		RolesRead,
		AssetsAssetsRead,
		AiConversationsRead,
	}
	cantDo := []string{
		OrganizationsUpdate,
		OrganizationsDelete,
		SpacesCreate,
		DomainsCreate,
		MembersCreate,
		AssetsAssetsCreate,
		AssetsAssetsUpdate,
		AssetsAssetsDelete,
		// `ai.conversations.create/update/delete` are NOT in cantDo
		// post-Phase-7: AI chat is personal (path-bound by user-uuid;
		// handler enforces creator-only). A viewer still gets a
		// personal AI chat experience for THEIR OWN conversations.
		// The `*All` audit perms below ARE in cantDo — viewers
		// cannot reach peers' chats.
		AiConversationsReadAll,
		AiConversationsDeleteAll,
	}
	for _, p := range canDo {
		t.Run("can/"+p, func(t *testing.T) {
			if !roleGrants(RoleViewer, p) {
				t.Errorf("viewer should have %q", p)
			}
		})
	}
	for _, p := range cantDo {
		t.Run("cant/"+p, func(t *testing.T) {
			if roleGrants(RoleViewer, p) {
				t.Errorf("viewer should NOT have %q", p)
			}
		})
	}
}

func TestMatrix_UnknownRoleDeniesEverything(t *testing.T) {
	if roleGrants("rando", OrganizationsRead) {
		t.Error("unknown role must not grant any permission")
	}
	if roleGrants("", OrganizationsRead) {
		t.Error("empty role must not grant any permission")
	}
}

func TestMatrix_UnknownPermissionDeniesForKnownRole(t *testing.T) {
	if roleGrants(RoleOwner, "fake.permission") {
		t.Error("owner must not grant unknown permission")
	}
}

// TestMatrix_AllConstantsCoveredByMatrix guards against drift: every
// permission constant in `All` must appear in the matrix for at least
// one role. A constant that no role grants is a dead permission —
// any handler check against it always denies.
func TestMatrix_AllConstantsCoveredByMatrix(t *testing.T) {
	for _, p := range All {
		t.Run(p, func(t *testing.T) {
			covered := roleGrants(RoleOwner, p) || roleGrants(RoleAdmin, p) ||
				roleGrants(RoleEditor, p) || roleGrants(RoleViewer, p)
			if !covered {
				t.Errorf("permission constant %q is in `All` but no role grants it", p)
			}
		})
	}
}

// TestPermissions_MigrationMatchesConstants is the critical drift-guard
// for the entire permission system. It parses the migration's
// `INSERT INTO permissions` blocks and asserts the seeded set is
// exactly equal to the `All` set in permissions.go.
//
// Failure modes this catches:
//   - Permission added to migration but no Go constant defined → handler
//     can't reference it without a typo.
//   - Permission constant defined but never seeded → handler check
//     against it always fails the org_members → role_permissions join.
//   - Typo in constant value vs migration row.
//
// If this test fails, fix one side or the other. Both must agree.
func TestPermissions_MigrationMatchesConstants(t *testing.T) {
	const migrationPath = "../db/migrations/000001_init.up.sql"

	body, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}

	// Line-based scan: track whether we're inside an INSERT INTO
	// permissions ... VALUES block, and pluck the first single-quoted
	// value off any row that looks like `('id.foo', 'display', ...)`.
	// Block ends when a line ends with `;` (statement terminator).
	//
	// Naive `(?s).+?;` regex on the whole file fails when a description
	// string contains an embedded semicolon (e.g. "Delete user (LRO;
	// cascades)") — the `;` inside the SQL literal terminates the
	// non-greedy match. Line-based parsing sidesteps the SQL-string
	// quoting problem entirely.
	rowRe := regexp.MustCompile(`^\s*\(\s*'([a-zA-Z0-9._]+)'\s*,`)
	inBlock := false
	seeded := map[string]struct{}{}
	scanner := bufio.NewScanner(bytes.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "INSERT INTO permissions") {
			inBlock = true
			continue
		}
		if !inBlock {
			continue
		}
		// Skip SQL comment lines so a commented-out row like
		// `-- ('foo.bar', ...)` doesn't get picked up as seeded.
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		if m := rowRe.FindStringSubmatch(line); m != nil {
			seeded[m[1]] = struct{}{}
		}
		if strings.HasSuffix(strings.TrimRight(line, " \t"), ";") {
			inBlock = false
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan migration: %v", err)
	}

	if len(seeded) == 0 {
		t.Fatal("parser found 0 seeded permissions — regex broken or migration moved")
	}

	declared := map[string]struct{}{}
	for _, p := range All {
		declared[p] = struct{}{}
	}

	missingFromConstants := diff(seeded, declared)
	missingFromMigration := diff(declared, seeded)

	sort.Strings(missingFromConstants)
	sort.Strings(missingFromMigration)

	if len(missingFromConstants) > 0 {
		t.Errorf("seeded in migration but no constant in permissions.go: %v", missingFromConstants)
	}
	if len(missingFromMigration) > 0 {
		t.Errorf("declared in permissions.go but not seeded in migration: %v", missingFromMigration)
	}
}

func diff(have, want map[string]struct{}) []string {
	var out []string
	for k := range have {
		if _, ok := want[k]; !ok {
			out = append(out, k)
		}
	}
	return out
}
