package organizations_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
)

// AIP-160/132 filter + order_by + pagination + show_deleted coverage for
// ListOrganizations, on the shared filter.BuildListQuery keyset engine.
// The base scope is "orgs the caller is a member of" — non-negotiable;
// filter/order_by narrow within it.

func orgDisplayNames(resp *apiv1.ListOrganizationsResponse) []string {
	out := make([]string, 0, len(resp.GetOrganizations()))
	for _, o := range resp.GetOrganizations() {
		out = append(out, o.GetDisplayName())
	}
	return out
}

// TestE2E_ListOrganizations_OrderByDisplayName pins that order_by is
// honored (the pre-migration handler discarded it, returning id order).
func TestE2E_ListOrganizations_OrderByDisplayName(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := newLifecycleHarness(t)
	ctx := context.Background()

	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "owner"})
	h.SetCaller(owner)
	client := apiv1.NewOrganizationsClient(h.Conn())

	// Create in non-alphabetical order so displayName order differs
	// from creation (id) order.
	createOrg(t, client, "org-c", "charlie")
	createOrg(t, client, "org-a", "alpha")
	createOrg(t, client, "org-b", "bravo")

	asc, err := client.ListOrganizations(ctx, &apiv1.ListOrganizationsRequest{OrderBy: "displayName"})
	require.NoError(t, err)
	assert.Equal(t, []string{"alpha", "bravo", "charlie"}, orgDisplayNames(asc))

	desc, err := client.ListOrganizations(ctx, &apiv1.ListOrganizationsRequest{OrderBy: "displayName desc"})
	require.NoError(t, err)
	assert.Equal(t, []string{"charlie", "bravo", "alpha"}, orgDisplayNames(desc))
	assert.NotEqual(t, orgDisplayNames(asc), orgDisplayNames(desc))
}

// TestE2E_ListOrganizations_FilterByDisplayName pins that filter narrows
// the caller's org list.
func TestE2E_ListOrganizations_FilterByDisplayName(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := newLifecycleHarness(t)
	ctx := context.Background()

	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "owner"})
	h.SetCaller(owner)
	client := apiv1.NewOrganizationsClient(h.Conn())

	createOrg(t, client, "flt-a", "Acme Corp")
	createOrg(t, client, "flt-b", "Beta LLC")
	createOrg(t, client, "flt-c", "Acme Labs")

	// Exact match.
	resp, err := client.ListOrganizations(ctx, &apiv1.ListOrganizationsRequest{
		Filter: `displayName = "Beta LLC"`,
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"Beta LLC"}, orgDisplayNames(resp))

	// Substring — both Acme rows.
	resp, err = client.ListOrganizations(ctx, &apiv1.ListOrganizationsRequest{
		Filter: `displayName : "Acme"`, OrderBy: "displayName",
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"Acme Corp", "Acme Labs"}, orgDisplayNames(resp))
}

// TestE2E_ListOrganizations_ShowDeleted pins the show_deleted flag: a
// soft-deleted org is hidden by default and revealed only with
// show_deleted=true (the pre-migration handler ignored the flag and
// always returned tombstones).
func TestE2E_ListOrganizations_ShowDeleted(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := newLifecycleHarness(t)
	ctx := context.Background()

	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "owner"})
	h.SetCaller(owner)
	client := apiv1.NewOrganizationsClient(h.Conn())

	createOrg(t, client, "live-org", "Live Org")
	createOrg(t, client, "gone-org", "Gone Org")
	goneID := h.LookupOrgID(t, "gone-org")

	// Soft-delete gone-org directly (the DeleteOrganization LRO path is
	// exercised elsewhere; here we only need the tombstone state).
	_, err := h.Pool.Exec(ctx,
		`UPDATE organizations SET state = 'DELETE_REQUESTED', delete_time = now() WHERE id = $1`,
		goneID)
	require.NoError(t, err)

	// Default: tombstone excluded.
	resp, err := client.ListOrganizations(ctx, &apiv1.ListOrganizationsRequest{})
	require.NoError(t, err)
	assert.Equal(t, []string{"Live Org"}, orgDisplayNames(resp),
		"soft-deleted org must be hidden by default")

	// show_deleted=true: tombstone included.
	resp, err = client.ListOrganizations(ctx, &apiv1.ListOrganizationsRequest{
		ShowDeleted: true, OrderBy: "displayName",
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"Gone Org", "Live Org"}, orgDisplayNames(resp),
		"show_deleted=true must include the tombstone")
}

// TestE2E_ListOrganizations_PaginationRoundTrip pins that the keyset
// cursor round-trips: creating more orgs than the page size and walking
// next_page_token returns every org exactly once.
func TestE2E_ListOrganizations_PaginationRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := newLifecycleHarness(t)
	ctx := context.Background()

	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "owner"})
	h.SetCaller(owner)
	client := apiv1.NewOrganizationsClient(h.Conn())

	const n = 7
	for i := range n {
		slug := fmt.Sprintf("page-org-%d", i)
		createOrg(t, client, slug, fmt.Sprintf("Page Org %d", i))
	}

	seen := map[string]int{}
	token := ""
	pages := 0
	for {
		pages++
		require.LessOrEqual(t, pages, 10, "pagination must terminate")
		resp, err := client.ListOrganizations(ctx, &apiv1.ListOrganizationsRequest{
			PageSize: 3, PageToken: token, OrderBy: "displayName",
		})
		require.NoError(t, err)
		for _, o := range resp.GetOrganizations() {
			seen[o.GetName()]++
		}
		token = resp.GetNextPageToken()
		if token == "" {
			break
		}
	}
	assert.Equal(t, n, len(seen), "every org appears exactly once across pages")
	for name, count := range seen {
		assert.Equal(t, 1, count, "org %q duplicated across pages", name)
	}
	assert.GreaterOrEqual(t, pages, 2, "must have walked multiple pages at page_size=3")
}

// TestE2E_ListOrganizations_MembershipScopeCrossTenant pins that the
// membership base scope holds under a filter: caller A never sees org B
// (a non-member org), even when a filter would match it.
func TestE2E_ListOrganizations_MembershipScopeCrossTenant(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := newLifecycleHarness(t)
	ctx := context.Background()

	client := apiv1.NewOrganizationsClient(h.Conn())

	alice := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "alice"})
	bob := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "bob"})

	h.SetCaller(alice)
	createOrg(t, client, "shared-alice", "Shared Name")

	h.SetCaller(bob)
	createOrg(t, client, "shared-bob", "Shared Name")

	// Alice, filtering on the shared display name, sees ONLY her org —
	// the filter cannot widen past her membership scope.
	h.SetCaller(alice)
	resp, err := client.ListOrganizations(ctx, &apiv1.ListOrganizationsRequest{
		Filter: `displayName = "Shared Name"`,
	})
	require.NoError(t, err)
	names := make([]string, 0)
	for _, o := range resp.GetOrganizations() {
		names = append(names, o.GetName())
	}
	assert.Equal(t, []string{"organizations/shared-alice"}, names,
		"filter cannot widen past the caller's membership scope")
}

// TestE2E_ListOrganizations_RejectsUnknownFields pins whitelist
// enforcement + tampered page tokens.
func TestE2E_ListOrganizations_RejectsUnknownFields(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := newLifecycleHarness(t)
	ctx := context.Background()

	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "owner"})
	h.SetCaller(owner)
	client := apiv1.NewOrganizationsClient(h.Conn())
	createOrg(t, client, "rej-org", "Rej Org")

	_, err := client.ListOrganizations(ctx, &apiv1.ListOrganizationsRequest{
		Filter: `secretColumn = "x"`,
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))

	_, err = client.ListOrganizations(ctx, &apiv1.ListOrganizationsRequest{
		OrderBy: "slug",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))

	_, err = client.ListOrganizations(ctx, &apiv1.ListOrganizationsRequest{
		PageToken: "not-a-real-token",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}
