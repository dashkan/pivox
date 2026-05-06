package organizations_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/dashkan/pivox/internal/appkey"
	"github.com/dashkan/pivox/internal/permission"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	iampb "github.com/dashkan/pivox/internal/pkg/gen/pivox/iam/v1"
	"github.com/dashkan/pivox/internal/server"
	"github.com/dashkan/pivox/internal/service/organizations"
	"github.com/dashkan/pivox/internal/service/spaces"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
)

// Member CRUD E2E coverage for both org-scope (Organizations service)
// and space-scope (Spaces service). Round-trips Get/List/Create/
// Update/Delete with a real DB through the production interceptor
// chain. Pins:
//
//   - Pagination round-trip (audit MED #5).
//   - >=1-owner boundary check on UpdateMember (demote-last-owner).
//   - Slug-mismatch defensive rejection (audit MED #4).
//   - CreateMember/DeleteMember happy paths.
//
// Boundary tests reuse the already-implemented system roles
// (owner/admin/editor/viewer) seeded by CreateOrganization.

// TestE2E_OrgMember_CreateGetListDelete pins the standard CRUD
// round-trip on org-scope members.
func TestE2E_OrgMember_CreateGetListDelete(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newMembersHarness(t)
	ctx := context.Background()

	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "owner"})
	h.SetCaller(owner)
	orgClient := apiv1.NewOrganizationsClient(h.Conn())
	createOrg(t, orgClient, "members-org", "Members Org")
	orgID := h.LookupOrgID(t, "members-org")

	// Seed a target identity + per-org users row. CreateMember
	// requires the principal to already exist in the org.
	target := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "target"})
	targetUserID := h.SeedUserMembershipOnly(t, orgID, target)

	// Create the member binding with editor role.
	created, err := orgClient.CreateMember(ctx, &iampb.CreateMemberRequest{
		Parent: "organizations/members-org",
		Member: &iampb.Member{
			Principal: &iampb.Member_User{
				User: "organizations/members-org/users/" + targetUserID.String(),
			},
			Role: "organizations/members-org/roles/editor",
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "organizations/members-org/roles/editor", created.GetRole())
	memberName := created.GetName()

	// Get returns the binding.
	got, err := orgClient.GetMember(ctx, &iampb.GetMemberRequest{Name: memberName})
	require.NoError(t, err)
	assert.Equal(t, "organizations/members-org/roles/editor", got.GetRole())

	// List includes the new binding (plus the founder's owner row).
	list, err := orgClient.ListMembers(ctx, &iampb.ListMembersRequest{
		Parent: "organizations/members-org",
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(list.GetMembers()), 2,
		"list must include founder owner + freshly-created editor")

	// Delete the editor binding.
	_, err = orgClient.DeleteMember(ctx, &iampb.DeleteMemberRequest{Name: memberName})
	require.NoError(t, err)

	// Get-after-delete is NotFound.
	_, err = orgClient.GetMember(ctx, &iampb.GetMemberRequest{Name: memberName})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

// TestE2E_OrgMember_UpdateRoleRejectsLastOwnerDemotion pins the
// >=1-owner boundary on UpdateMember. The org has exactly one owner
// (the founder); demoting them to admin would leave it ownerless,
// so the handler refuses with FailedPrecondition.
func TestE2E_OrgMember_UpdateRoleRejectsLastOwnerDemotion(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newMembersHarness(t)
	ctx := context.Background()

	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "owner"})
	h.SetCaller(owner)
	orgClient := apiv1.NewOrganizationsClient(h.Conn())
	createOrg(t, orgClient, "single-owner", "Single Owner")
	orgID := h.LookupOrgID(t, "single-owner")
	founderUserID := h.LookupOrgUserID(t, orgID, owner.IdentityID)
	memberName := "organizations/single-owner/members/user-" + founderUserID.String()

	_, err := orgClient.UpdateMember(ctx, &iampb.UpdateMemberRequest{
		Member: &iampb.Member{
			Name: memberName,
			Role: "organizations/single-owner/roles/admin",
		},
	})
	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	assert.Contains(t, status.Convert(err).Message(), "owner")
}

// TestE2E_OrgMember_PaginationRoundTrip pins the offset-based
// pagination contract (audit MED #5). Creates enough bindings to
// exceed the default page_size, then walks pages via next_page_token
// and confirms every member appears exactly once across the union of
// pages.
func TestE2E_OrgMember_PaginationRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newMembersHarness(t)
	ctx := context.Background()

	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "owner"})
	h.SetCaller(owner)
	orgClient := apiv1.NewOrganizationsClient(h.Conn())
	createOrg(t, orgClient, "paged-org", "Paged Org")
	orgID := h.LookupOrgID(t, "paged-org")

	// Seed 6 additional members. With pageSize=3 we expect 3 pages
	// (founder + 6 = 7 rows, paged 3/3/1).
	for i := 0; i < 6; i++ {
		uid := "member-" + string(rune('A'+i))
		ident := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: uid})
		h.SeedMembership(t, orgID, ident, grpcharness.RoleEditor)
	}

	seen := map[string]int{}
	pageToken := ""
	pages := 0
	for {
		pages++
		require.LessOrEqual(t, pages, 5, "pagination loop must terminate")
		resp, err := orgClient.ListMembers(ctx, &iampb.ListMembersRequest{
			Parent:    "organizations/paged-org",
			PageSize:  3,
			PageToken: pageToken,
		})
		require.NoError(t, err)
		for _, m := range resp.GetMembers() {
			seen[m.GetName()]++
		}
		if resp.GetNextPageToken() == "" {
			break
		}
		pageToken = resp.GetNextPageToken()
	}
	assert.Equal(t, 7, len(seen), "every distinct member must appear exactly once across pages")
	for name, count := range seen {
		assert.Equal(t, 1, count, "member %q duplicated across pages", name)
	}
	assert.GreaterOrEqual(t, pages, 2, "must have walked at least two pages with page_size=3")
}

// TestE2E_OrgMember_RejectsBadPageToken pins that a malformed page
// token surfaces as InvalidArgument rather than silently resetting
// to the first page.
func TestE2E_OrgMember_RejectsBadPageToken(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newMembersHarness(t)
	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "owner"})
	h.SetCaller(owner)
	orgClient := apiv1.NewOrganizationsClient(h.Conn())
	createOrg(t, orgClient, "bad-token-org", "Bad Token Org")

	_, err := orgClient.ListMembers(context.Background(), &iampb.ListMembersRequest{
		Parent:    "organizations/bad-token-org",
		PageToken: "not-an-int",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

// TestE2E_SpaceMember_CreateListDelete pins CRUD on space-scope
// members. Same shape as the org-scope test but addresses
// `organizations/{org}/spaces/{space}/members/...`.
func TestE2E_SpaceMember_CreateListDelete(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newMembersHarness(t)
	ctx := context.Background()

	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "owner"})
	h.SetCaller(owner)

	orgClient := apiv1.NewOrganizationsClient(h.Conn())
	spacesClient := apiv1.NewSpacesClient(h.Conn())
	createOrg(t, orgClient, "space-mem-org", "Space Members Org")
	orgID := h.LookupOrgID(t, "space-mem-org")

	// Create a space.
	createSpaceOp, err := spacesClient.CreateSpace(ctx, &apiv1.CreateSpaceRequest{
		Parent:  "organizations/space-mem-org",
		SpaceId: "engineering",
		Space:   &apiv1.Space{DisplayName: "Engineering"},
	})
	require.NoError(t, err)
	require.True(t, createSpaceOp.GetDone())

	// Seed a target identity + per-org users row.
	target := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "target"})
	targetUserID := h.SeedUserMembershipOnly(t, orgID, target)

	// Create a space-scope member binding.
	created, err := spacesClient.CreateMember(ctx, &iampb.CreateMemberRequest{
		Parent: "organizations/space-mem-org/spaces/engineering",
		Member: &iampb.Member{
			Principal: &iampb.Member_User{
				User: "organizations/space-mem-org/users/" + targetUserID.String(),
			},
			Role: "organizations/space-mem-org/roles/editor",
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "organizations/space-mem-org/roles/editor", created.GetRole())
	memberName := created.GetName()

	// List should include the binding.
	list, err := spacesClient.ListMembers(ctx, &iampb.ListMembersRequest{
		Parent: "organizations/space-mem-org/spaces/engineering",
	})
	require.NoError(t, err)
	require.NotEmpty(t, list.GetMembers())

	// Delete + verify.
	_, err = spacesClient.DeleteMember(ctx, &iampb.DeleteMemberRequest{Name: memberName})
	require.NoError(t, err)

	_, err = spacesClient.GetMember(ctx, &iampb.GetMemberRequest{Name: memberName})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

// TestE2E_OrgMember_RejectsCrossOrgPath pins the slug-mismatch
// defensive assertion (audit MED #4): a member path whose org slug
// doesn't match the resolved scope is rejected with InvalidArgument
// rather than silently operating on the wrong org.
func TestE2E_OrgMember_RejectsCrossOrgPath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newMembersHarness(t)
	ctx := context.Background()

	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "owner"})
	h.SetCaller(owner)
	orgClient := apiv1.NewOrganizationsClient(h.Conn())
	createOrg(t, orgClient, "real-org", "Real Org")

	// Fabricate a UUID and address a member in a different (non-
	// existent) org. The interceptor first 404s at the slug
	// resolution layer; even if it didn't, the handler's defensive
	// slug check catches the mismatch.
	_, err := orgClient.GetMember(ctx, &iampb.GetMemberRequest{
		Name: "organizations/ghost-org/members/user-00000000-0000-0000-0000-000000000000",
	})
	require.Error(t, err)
	got := status.Code(err)
	assert.True(t, got == codes.NotFound || got == codes.InvalidArgument || got == codes.PermissionDenied,
		"cross-org member lookup must surface NotFound / InvalidArgument / PermissionDenied; got %v", got)
}

// TestE2E_OrgMember_CreateMember_FKRaceReturnsNotFound pins the
// SQLSTATE 23503 → NotFound mapping introduced in #10 against a real
// PG. Sequence:
//
//  1. Founder creates org. CreateMember is reachable through the
//     interceptor chain because the harness wires permResolver
//     (without it CreateMember would have been gated at the perm
//     check long before the FK violation could fire — that gap was
//     the original blocker for landing this test).
//  2. Seed a target identity row, capture its UUID.
//  3. Hard-DELETE the target identity directly via h.Pool, simulating
//     the race window where a caller resolves an identity, then it
//     gets purged before the CreateMember handler's INSERT fires. Hard
//     delete is operator-only — no public RPC takes this path — so we
//     issue raw SQL rather than going through any production handler.
//  4. CreateMember pointing at the now-dangling UUID. Pre-#10 the
//     handler's verifyPrincipalInOrg pre-check would 404 cleanly;
//     with that gone, the FK violation on org_members.user_id flows
//     through `apierr.HandleResourceError`, which (post-#10) maps
//     SQLSTATE 23503 to NotFound rather than Internal.
//
// Asserts NotFound — the load-bearing post-condition. If a future
// change either undoes the apierr mapping or restores
// verifyPrincipalInOrg in a way that masks the FK path, this test
// catches it.
func TestE2E_OrgMember_CreateMember_FKRaceReturnsNotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newMembersHarness(t)
	ctx := context.Background()

	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "owner-fkrace"})
	h.SetCaller(owner)
	orgClient := apiv1.NewOrganizationsClient(h.Conn())
	createOrg(t, orgClient, "fkrace-org", "FK Race Org")

	// Seed a target identity. We intentionally do NOT bind it as a
	// member — we want to exercise the path where the CreateMember
	// handler's INSERT references an identities row that disappears
	// between resolve and INSERT, not a failed update on an
	// existing binding.
	target := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "target-fkrace"})

	// Hard-delete the target identity directly via the pool. The
	// production code path for identity removal is SoftDelete (PII
	// blank + tombstone); hard-delete is operator-only and not
	// exposed as an RPC. Direct SQL is the only way to recreate the
	// race in a test.
	_, err := h.Pool.Exec(ctx,
		`DELETE FROM identities WHERE id = $1`, target.IdentityID)
	require.NoError(t, err, "hard-delete of unbound target identity must succeed")

	// CreateMember now references a dangling identities.id. The FK
	// on org_members.user_id rejects the INSERT with 23503, which
	// the apierr layer maps to NotFound for the principal's resource
	// type (User).
	_, err = orgClient.CreateMember(ctx, &iampb.CreateMemberRequest{
		Parent: "organizations/fkrace-org",
		Member: &iampb.Member{
			Principal: &iampb.Member_User{
				User: "organizations/fkrace-org/users/" + target.IdentityID.String(),
			},
			Role: "organizations/fkrace-org/roles/editor",
		},
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err),
		"FK violation on dangling principal must surface as NotFound (#10)")
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func newMembersHarness(t *testing.T) *grpcharness.Harness {
	return grpcharness.New(t, grpcharness.WithServices(func(h *grpcharness.Harness, s *grpc.Server) {
		callerIdentity := server.NewCallerIdentityResolver(h.Queries)
		permResolver := permission.NewResolver(h.Queries)
		codec, err := appkey.NewFromHex(strings.Repeat("ab", 32))
		require.NoError(t, err)
		apiv1.RegisterOrganizationsServer(s, organizations.NewOrganizationsServer(organizations.Config{
			Pool:       h.Pool,
			Queries:    h.Queries,
			Auth:       h.Auth,
			Codec:      codec,
			ReadUID:    server.AuthenticatedUID,
			Resolver:   permResolver,
			Caller:     callerIdentity,
			LROManager: h.LROManager,
			Encryptor:  h.Encryptor,
		}))
		apiv1.RegisterSpacesServer(s, spaces.NewSpacesServer(spaces.Config{
			Pool:       h.Pool,
			Queries:    h.Queries,
			Codec:      codec,
			Resolver:   permResolver,
			Caller:     callerIdentity,
			LROManager: h.LROManager,
		}))
	}))
}
