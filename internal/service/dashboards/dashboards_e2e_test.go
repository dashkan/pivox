// Copyright 2025 Pivox
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package dashboards_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	"github.com/dashkan/pivox/internal/service/dashboards"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
)

// Import chain that registers the Asset template:
//   dashboards (via NewServer's validateRegistries call) →
//   internal/dashboard/system → internal/dashboard/system/library
//     → blank-imports internal/service/assets/dashtemplate → init().
// Importing the dashboards package transitively triggers all of
// the above, so this test file does not blank-import the library
// or the dashtemplate package directly.

const libraryDashboardID = "library"

func newDashboardsHarness(t *testing.T) *grpcharness.Harness {
	t.Helper()
	return grpcharness.New(t,
		grpcharness.WithOrganizationsServer(),
		grpcharness.WithSpacesServer(),
		grpcharness.WithServices(func(h *grpcharness.Harness, s *grpc.Server) {
			apiv1.RegisterDashboardsServer(s, dashboards.NewServer(dashboards.Config{
				Pool:    h.Pool,
				Queries: h.Queries,
				Codec:   grpcharness.TestAppCodec(),
			}))
		}),
	)
}

// seededFixture bundles an org + space + handy resource-name strings
// so the per-test setup boilerplate stays one line.
type seededFixture struct {
	owner       grpcharness.OwnedOrg
	space       grpcharness.OwnedSpace
	spaceParent string // organizations/{org}/spaces/{space}
}

func seedFixture(t *testing.T, h *grpcharness.Harness, slug string) seededFixture {
	t.Helper()
	owner := h.SeedOwnedOrg(t, slug, "Dashboards Test", "owner")
	space := h.SeedOwnedSpace(t, owner.Slug, "main", "Main")
	return seededFixture{
		owner:       owner,
		space:       space,
		spaceParent: "organizations/" + owner.Slug + "/spaces/" + space.Slug,
	}
}

// ============================================================================
// Org-scoped read path (Phase 4a behavior, retained)
// ============================================================================

func TestE2E_ListDashboards_OrgParent_ReturnsCatalog(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newDashboardsHarness(t)
	owner := h.SeedOwnedOrg(t, "dash-list-org", "Dash List Org", "owner")

	client := apiv1.NewDashboardsClient(h.Conn())
	resp, err := client.ListDashboards(context.Background(), &apiv1.ListDashboardsRequest{
		Parent: "organizations/" + owner.Slug,
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.GetDashboards())

	var found bool
	for _, d := range resp.GetDashboards() {
		assert.Equal(t, apiv1.Dashboard_SYSTEM_MANAGED, d.GetManagementMode())
		if d.GetName() == "organizations/"+owner.Slug+"/dashboards/"+libraryDashboardID {
			found = true
		}
	}
	require.True(t, found, "expected Library entry in catalog response")
}

func TestE2E_GetDashboard_OrgParent_Library(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newDashboardsHarness(t)
	owner := h.SeedOwnedOrg(t, "dash-get-org", "Dash Get Org", "owner")

	client := apiv1.NewDashboardsClient(h.Conn())
	d, err := client.GetDashboard(context.Background(), &apiv1.GetDashboardRequest{
		Name: "organizations/" + owner.Slug + "/dashboards/" + libraryDashboardID,
	})
	require.NoError(t, err)

	assert.Equal(t, apiv1.Dashboard_SYSTEM_MANAGED, d.GetManagementMode())
	require.NotNil(t, d.GetGridLayout())
	require.Len(t, d.GetGridLayout().GetTiles(), 1)
}

func TestE2E_GetDashboard_OrgParent_NotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newDashboardsHarness(t)
	owner := h.SeedOwnedOrg(t, "dash-org-404", "Dash Org 404", "owner")

	client := apiv1.NewDashboardsClient(h.Conn())
	_, err := client.GetDashboard(context.Background(), &apiv1.GetDashboardRequest{
		Name: "organizations/" + owner.Slug + "/dashboards/never-shipped",
	})
	requireGRPCCode(t, err, codes.NotFound)
}

func TestE2E_ListDashboards_MalformedParent_InvalidArgument(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newDashboardsHarness(t)
	_ = h.SeedOwnedOrg(t, "dash-bad-parent", "Dash Bad Parent", "owner")

	client := apiv1.NewDashboardsClient(h.Conn())
	_, err := client.ListDashboards(context.Background(), &apiv1.ListDashboardsRequest{
		Parent: "users/foo",
	})
	requireGRPCCode(t, err, codes.InvalidArgument)
}

// ============================================================================
// Space-scoped CRUD (Phase 4b)
// ============================================================================

func TestE2E_CreateDashboard_HappyPath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newDashboardsHarness(t)
	fx := seedFixture(t, h, "dash-cr-happy")

	client := apiv1.NewDashboardsClient(h.Conn())
	d, err := client.CreateDashboard(context.Background(), &apiv1.CreateDashboardRequest{
		Parent:      fx.spaceParent,
		DashboardId: "sprint-view",
		Dashboard: &apiv1.Dashboard{
			DisplayName: "Sprint View",
			Description: "Per-sprint cards.",
		},
	})
	require.NoError(t, err)
	assert.Equal(t, fx.spaceParent+"/dashboards/sprint-view", d.GetName())
	assert.Equal(t, "Sprint View", d.GetDisplayName())
	assert.Equal(t, "Per-sprint cards.", d.GetDescription())
	assert.Equal(t, apiv1.Dashboard_USER_MANAGED, d.GetManagementMode())
	assert.NotEmpty(t, d.GetEtag(), "Create must return an etag")
	assert.NotZero(t, d.GetCreateTime().AsTime())
}

func TestE2E_CreateDashboard_RejectsBadSlug(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newDashboardsHarness(t)
	fx := seedFixture(t, h, "dash-cr-bad")

	client := apiv1.NewDashboardsClient(h.Conn())
	_, err := client.CreateDashboard(context.Background(), &apiv1.CreateDashboardRequest{
		Parent:      fx.spaceParent,
		DashboardId: "BadSlug", // upper-case rejected by the slug regex
		Dashboard:   &apiv1.Dashboard{DisplayName: "X"},
	})
	requireGRPCCode(t, err, codes.InvalidArgument)
}

func TestE2E_CreateDashboard_RequiresDashboardID(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newDashboardsHarness(t)
	fx := seedFixture(t, h, "dash-cr-noid")

	client := apiv1.NewDashboardsClient(h.Conn())
	_, err := client.CreateDashboard(context.Background(), &apiv1.CreateDashboardRequest{
		Parent:    fx.spaceParent,
		Dashboard: &apiv1.Dashboard{DisplayName: "X"},
	})
	requireGRPCCode(t, err, codes.InvalidArgument)
}

func TestE2E_CreateDashboard_DuplicateSlug_AlreadyExists(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newDashboardsHarness(t)
	fx := seedFixture(t, h, "dash-cr-dup")

	client := apiv1.NewDashboardsClient(h.Conn())
	_, err := client.CreateDashboard(context.Background(), &apiv1.CreateDashboardRequest{
		Parent:      fx.spaceParent,
		DashboardId: "dup-slug",
		Dashboard:   &apiv1.Dashboard{DisplayName: "First"},
	})
	require.NoError(t, err)

	_, err = client.CreateDashboard(context.Background(), &apiv1.CreateDashboardRequest{
		Parent:      fx.spaceParent,
		DashboardId: "dup-slug",
		Dashboard:   &apiv1.Dashboard{DisplayName: "Second"},
	})
	requireGRPCCode(t, err, codes.AlreadyExists)
}

func TestE2E_CreateDashboard_IgnoresClientManagementMode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newDashboardsHarness(t)
	fx := seedFixture(t, h, "dash-cr-mgmt")

	// AIP convention: OUTPUT_ONLY fields supplied on input are
	// silently discarded. Even if the caller asks for SYSTEM_MANAGED,
	// the persisted row is USER_MANAGED.
	client := apiv1.NewDashboardsClient(h.Conn())
	d, err := client.CreateDashboard(context.Background(), &apiv1.CreateDashboardRequest{
		Parent:      fx.spaceParent,
		DashboardId: "client-asks-system",
		Dashboard: &apiv1.Dashboard{
			DisplayName:    "X",
			ManagementMode: apiv1.Dashboard_SYSTEM_MANAGED,
		},
	})
	require.NoError(t, err)
	assert.Equal(t, apiv1.Dashboard_USER_MANAGED, d.GetManagementMode())
}

func TestE2E_GetDashboard_SpaceScoped_RoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newDashboardsHarness(t)
	fx := seedFixture(t, h, "dash-get-rt")

	client := apiv1.NewDashboardsClient(h.Conn())
	created, err := client.CreateDashboard(context.Background(), &apiv1.CreateDashboardRequest{
		Parent:      fx.spaceParent,
		DashboardId: "rt-dash",
		Dashboard:   &apiv1.Dashboard{DisplayName: "Round-trip"},
	})
	require.NoError(t, err)

	got, err := client.GetDashboard(context.Background(), &apiv1.GetDashboardRequest{
		Name: created.GetName(),
	})
	require.NoError(t, err)
	assert.Equal(t, created.GetName(), got.GetName())
	assert.Equal(t, "Round-trip", got.GetDisplayName())
	assert.Equal(t, created.GetEtag(), got.GetEtag())
}

func TestE2E_GetDashboard_SpaceScoped_NotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newDashboardsHarness(t)
	fx := seedFixture(t, h, "dash-get-404")

	client := apiv1.NewDashboardsClient(h.Conn())
	_, err := client.GetDashboard(context.Background(), &apiv1.GetDashboardRequest{
		Name: fx.spaceParent + "/dashboards/never-created",
	})
	requireGRPCCode(t, err, codes.NotFound)
}

func TestE2E_ListDashboards_SpaceScoped_ReturnsAll(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newDashboardsHarness(t)
	fx := seedFixture(t, h, "dash-list-sp")

	client := apiv1.NewDashboardsClient(h.Conn())
	for _, slug := range []string{"alpha", "beta", "gamma"} {
		_, err := client.CreateDashboard(context.Background(), &apiv1.CreateDashboardRequest{
			Parent:      fx.spaceParent,
			DashboardId: slug,
			Dashboard:   &apiv1.Dashboard{DisplayName: slug},
		})
		require.NoError(t, err)
	}

	resp, err := client.ListDashboards(context.Background(), &apiv1.ListDashboardsRequest{
		Parent: fx.spaceParent,
	})
	require.NoError(t, err)
	assert.Len(t, resp.GetDashboards(), 3)
	for _, d := range resp.GetDashboards() {
		assert.Equal(t, apiv1.Dashboard_USER_MANAGED, d.GetManagementMode())
	}
}

// TestE2E_ListDashboards_SpaceScoped_FilterNarrows proves the AIP-160 filter
// is wired for the space branch: an exact-match filter returns only the
// matching dashboard. Before the BuildListQuery migration the space branch
// rejected any filter with InvalidArgument, so this failed.
func TestE2E_ListDashboards_SpaceScoped_FilterNarrows(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newDashboardsHarness(t)
	fx := seedFixture(t, h, "dash-list-flt")

	client := apiv1.NewDashboardsClient(h.Conn())
	for slug, name := range map[string]string{"alpha": "Alpha", "beta": "Beta", "gamma": "Gamma"} {
		_, err := client.CreateDashboard(context.Background(), &apiv1.CreateDashboardRequest{
			Parent:      fx.spaceParent,
			DashboardId: slug,
			Dashboard:   &apiv1.Dashboard{DisplayName: name},
		})
		require.NoError(t, err)
	}

	resp, err := client.ListDashboards(context.Background(), &apiv1.ListDashboardsRequest{
		Parent: fx.spaceParent,
		Filter: `displayName = "Beta"`,
	})
	require.NoError(t, err)
	require.Len(t, resp.GetDashboards(), 1, "filter must narrow to the single match")
	assert.Equal(t, "Beta", resp.GetDashboards()[0].GetDisplayName())
}

// TestE2E_ListDashboards_SpaceScoped_OrderBy proves order_by is wired and that
// ascending and descending produce opposite orders. Before the migration the
// space branch rejected order_by with InvalidArgument.
func TestE2E_ListDashboards_SpaceScoped_OrderBy(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newDashboardsHarness(t)
	fx := seedFixture(t, h, "dash-list-ord")

	client := apiv1.NewDashboardsClient(h.Conn())
	for slug, name := range map[string]string{"aaaa": "Aaa", "bbbb": "Bbb", "cccc": "Ccc"} {
		_, err := client.CreateDashboard(context.Background(), &apiv1.CreateDashboardRequest{
			Parent:      fx.spaceParent,
			DashboardId: slug,
			Dashboard:   &apiv1.Dashboard{DisplayName: name},
		})
		require.NoError(t, err)
	}

	names := func(order string) []string {
		resp, err := client.ListDashboards(context.Background(), &apiv1.ListDashboardsRequest{
			Parent:  fx.spaceParent,
			OrderBy: order,
		})
		require.NoError(t, err)
		out := make([]string, 0, len(resp.GetDashboards()))
		for _, d := range resp.GetDashboards() {
			out = append(out, d.GetDisplayName())
		}
		return out
	}

	asc := names("displayName asc")
	desc := names("displayName desc")
	assert.Equal(t, []string{"Aaa", "Bbb", "Ccc"}, asc)
	assert.Equal(t, []string{"Ccc", "Bbb", "Aaa"}, desc)
	assert.NotEqual(t, asc, desc, "asc and desc must differ")
}

// TestE2E_ListDashboards_SpaceScoped_Pagination proves keyset pagination
// round-trips past the first page. Before the migration the offset stub
// ignored page_token and never emitted a next_page_token.
func TestE2E_ListDashboards_SpaceScoped_Pagination(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newDashboardsHarness(t)
	fx := seedFixture(t, h, "dash-list-pg")

	client := apiv1.NewDashboardsClient(h.Conn())
	want := map[string]bool{}
	for _, slug := range []string{"pgone", "pgtwo", "pgthree"} {
		_, err := client.CreateDashboard(context.Background(), &apiv1.CreateDashboardRequest{
			Parent:      fx.spaceParent,
			DashboardId: slug,
			Dashboard:   &apiv1.Dashboard{DisplayName: slug},
		})
		require.NoError(t, err)
		want[fx.spaceParent+"/dashboards/"+slug] = true
	}

	page1, err := client.ListDashboards(context.Background(), &apiv1.ListDashboardsRequest{
		Parent:   fx.spaceParent,
		PageSize: 2,
	})
	require.NoError(t, err)
	require.Len(t, page1.GetDashboards(), 2)
	require.NotEmpty(t, page1.GetNextPageToken())

	page2, err := client.ListDashboards(context.Background(), &apiv1.ListDashboardsRequest{
		Parent:    fx.spaceParent,
		PageSize:  2,
		PageToken: page1.GetNextPageToken(),
	})
	require.NoError(t, err)
	require.Len(t, page2.GetDashboards(), 1)

	seen := map[string]bool{}
	for _, d := range append(page1.GetDashboards(), page2.GetDashboards()...) {
		assert.False(t, seen[d.GetName()], "no dashboard may repeat across pages")
		seen[d.GetName()] = true
	}
	assert.Equal(t, want, seen, "union of pages must be exactly the three dashboards")
}

func TestE2E_UpdateDashboard_HappyPath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newDashboardsHarness(t)
	fx := seedFixture(t, h, "dash-up-happy")

	client := apiv1.NewDashboardsClient(h.Conn())
	created, err := client.CreateDashboard(context.Background(), &apiv1.CreateDashboardRequest{
		Parent:      fx.spaceParent,
		DashboardId: "up-target",
		Dashboard:   &apiv1.Dashboard{DisplayName: "Original"},
	})
	require.NoError(t, err)

	updated, err := client.UpdateDashboard(context.Background(), &apiv1.UpdateDashboardRequest{
		Dashboard: &apiv1.Dashboard{
			Name:        created.GetName(),
			DisplayName: "Updated",
			Description: "Now with description",
			Etag:        created.GetEtag(),
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "Updated", updated.GetDisplayName())
	assert.Equal(t, "Now with description", updated.GetDescription())
	assert.NotEqual(t, created.GetEtag(), updated.GetEtag(),
		"every update must rotate the etag")
}

func TestE2E_UpdateDashboard_RequiresEtag(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newDashboardsHarness(t)
	fx := seedFixture(t, h, "dash-up-noetag")

	client := apiv1.NewDashboardsClient(h.Conn())
	created, err := client.CreateDashboard(context.Background(), &apiv1.CreateDashboardRequest{
		Parent:      fx.spaceParent,
		DashboardId: "noetag-target",
		Dashboard:   &apiv1.Dashboard{DisplayName: "X"},
	})
	require.NoError(t, err)

	// Empty etag is rejected per AIP-154 — omitting it would silently
	// disable optimistic concurrency.
	_, err = client.UpdateDashboard(context.Background(), &apiv1.UpdateDashboardRequest{
		Dashboard: &apiv1.Dashboard{
			Name:        created.GetName(),
			DisplayName: "Updated without etag",
		},
	})
	requireGRPCCode(t, err, codes.InvalidArgument)
}

func TestE2E_UpdateDashboard_FieldMaskRejectsManagementMode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newDashboardsHarness(t)
	fx := seedFixture(t, h, "dash-up-mask")

	client := apiv1.NewDashboardsClient(h.Conn())
	created, err := client.CreateDashboard(context.Background(), &apiv1.CreateDashboardRequest{
		Parent:      fx.spaceParent,
		DashboardId: "mask-target",
		Dashboard:   &apiv1.Dashboard{DisplayName: "X"},
	})
	require.NoError(t, err)

	_, err = client.UpdateDashboard(context.Background(), &apiv1.UpdateDashboardRequest{
		Dashboard: &apiv1.Dashboard{
			Name:           created.GetName(),
			DisplayName:    "Renamed",
			ManagementMode: apiv1.Dashboard_SYSTEM_MANAGED,
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"display_name", "management_mode"}},
	})
	requireGRPCCode(t, err, codes.InvalidArgument)
}

func TestE2E_UpdateDashboard_OrgScopedName_FailedPrecondition(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newDashboardsHarness(t)
	owner := h.SeedOwnedOrg(t, "dash-up-cat", "Dash Up Catalog", "owner")

	// Catalog dashboards (org-scoped name pattern) are SYSTEM_MANAGED
	// and reject mutation regardless of URL path. Trying to update
	// the Library entry should fail at the data-driven guard.
	client := apiv1.NewDashboardsClient(h.Conn())
	_, err := client.UpdateDashboard(context.Background(), &apiv1.UpdateDashboardRequest{
		Dashboard: &apiv1.Dashboard{
			Name:        "organizations/" + owner.Slug + "/dashboards/" + libraryDashboardID,
			DisplayName: "Pwned",
		},
	})
	requireGRPCCode(t, err, codes.FailedPrecondition)
}

func TestE2E_UpdateDashboard_StaleEtag_Aborted(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newDashboardsHarness(t)
	fx := seedFixture(t, h, "dash-up-etag")

	client := apiv1.NewDashboardsClient(h.Conn())
	created, err := client.CreateDashboard(context.Background(), &apiv1.CreateDashboardRequest{
		Parent:      fx.spaceParent,
		DashboardId: "etag-target",
		Dashboard:   &apiv1.Dashboard{DisplayName: "X"},
	})
	require.NoError(t, err)

	// First update succeeds — rotates the etag.
	_, err = client.UpdateDashboard(context.Background(), &apiv1.UpdateDashboardRequest{
		Dashboard: &apiv1.Dashboard{
			Name:        created.GetName(),
			DisplayName: "First update",
			Etag:        created.GetEtag(),
		},
	})
	require.NoError(t, err)

	// Second update with the original (now-stale) etag must fail.
	_, err = client.UpdateDashboard(context.Background(), &apiv1.UpdateDashboardRequest{
		Dashboard: &apiv1.Dashboard{
			Name:        created.GetName(),
			DisplayName: "Stale update",
			Etag:        created.GetEtag(),
		},
	})
	requireGRPCCode(t, err, codes.Aborted)
}

func TestE2E_UpdateDashboard_SystemManagedRow_FailedPrecondition(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newDashboardsHarness(t)
	fx := seedFixture(t, h, "dash-up-sysmgmt")

	// Insert a SYSTEM_MANAGED row directly via SQL — there's no
	// public path that creates one, but the mutation guard MUST
	// reject Updates against any such row that operators may
	// import in the future.
	insertSystemManagedDashboard(t, h, fx, "imported-sys")

	client := apiv1.NewDashboardsClient(h.Conn())
	_, err := client.UpdateDashboard(context.Background(), &apiv1.UpdateDashboardRequest{
		Dashboard: &apiv1.Dashboard{
			Name:        fx.spaceParent + "/dashboards/imported-sys",
			DisplayName: "Pwned",
		},
	})
	requireGRPCCode(t, err, codes.FailedPrecondition)
}

func TestE2E_DeleteDashboard_HappyPath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newDashboardsHarness(t)
	fx := seedFixture(t, h, "dash-del-happy")

	client := apiv1.NewDashboardsClient(h.Conn())
	created, err := client.CreateDashboard(context.Background(), &apiv1.CreateDashboardRequest{
		Parent:      fx.spaceParent,
		DashboardId: "del-target",
		Dashboard:   &apiv1.Dashboard{DisplayName: "Goodbye"},
	})
	require.NoError(t, err)

	deleted, err := client.DeleteDashboard(context.Background(), &apiv1.DeleteDashboardRequest{
		Name: created.GetName(),
	})
	require.NoError(t, err)
	assert.Equal(t, created.GetName(), deleted.GetName())

	// Soft-deleted: subsequent Get returns NotFound.
	_, err = client.GetDashboard(context.Background(), &apiv1.GetDashboardRequest{
		Name: created.GetName(),
	})
	requireGRPCCode(t, err, codes.NotFound)
}

func TestE2E_DeleteDashboard_OrgScopedName_FailedPrecondition(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newDashboardsHarness(t)
	owner := h.SeedOwnedOrg(t, "dash-del-cat", "Dash Del Catalog", "owner")

	client := apiv1.NewDashboardsClient(h.Conn())
	_, err := client.DeleteDashboard(context.Background(), &apiv1.DeleteDashboardRequest{
		Name: "organizations/" + owner.Slug + "/dashboards/" + libraryDashboardID,
	})
	requireGRPCCode(t, err, codes.FailedPrecondition)
}

func TestE2E_DeleteDashboard_SystemManagedRow_FailedPrecondition(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newDashboardsHarness(t)
	fx := seedFixture(t, h, "dash-del-sysmgmt")

	insertSystemManagedDashboard(t, h, fx, "del-imported-sys")

	client := apiv1.NewDashboardsClient(h.Conn())
	_, err := client.DeleteDashboard(context.Background(), &apiv1.DeleteDashboardRequest{
		Name: fx.spaceParent + "/dashboards/del-imported-sys",
	})
	requireGRPCCode(t, err, codes.FailedPrecondition)
}

// ============================================================================
// Helpers
// ============================================================================

// requireGRPCCode asserts that err is a gRPC error with the given code.
func requireGRPCCode(t *testing.T, err error, want codes.Code) {
	t.Helper()
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok, "expected gRPC status error, got %T: %v", err, err)
	assert.Equal(t, want, st.Code(), "unexpected gRPC code: full status = %v", st)
}

// insertSystemManagedDashboard sidesteps the gRPC handler (which
// always inserts USER_MANAGED) to seed a SYSTEM_MANAGED row in the
// dashboards table. Used to exercise the data-driven mutation
// guard — there's no public path that creates such a row today,
// but the guard must defend against future imports.
func insertSystemManagedDashboard(t *testing.T, h *grpcharness.Harness, fx seededFixture, slug string) {
	t.Helper()
	_, err := h.Pool.Exec(context.Background(), `
		INSERT INTO dashboards (
			space_id, name, display_name, management_mode, payload
		) VALUES ($1, $2, $3, 'SYSTEM_MANAGED', $4::jsonb)
	`, fx.space.Row.ID, slug, "Imported System Dashboard",
		`{"display_name":"Imported System Dashboard","management_mode":"SYSTEM_MANAGED"}`)
	require.NoError(t, err)
}

// TestE2E_CreateDashboard_ValidateOnly pins the AIP validate_only contract:
// a dry-run runs the same validation a live request would (so a would-fail
// request still fails) but persists nothing — the would-be dashboard is not
// gettable and its slug is reusable.
func TestE2E_CreateDashboard_ValidateOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newDashboardsHarness(t)
	fx := seedFixture(t, h, "dash-vo")
	client := apiv1.NewDashboardsClient(h.Conn())
	ctx := context.Background()

	// A dry-run Create returns the would-be resource but writes nothing.
	dry, err := client.CreateDashboard(ctx, &apiv1.CreateDashboardRequest{
		Parent:       fx.spaceParent,
		DashboardId:  "vo-dash",
		Dashboard:    &apiv1.Dashboard{DisplayName: "Dry"},
		ValidateOnly: true,
	})
	require.NoError(t, err)
	require.Equal(t, fx.spaceParent+"/dashboards/vo-dash", dry.GetName())

	// Not persisted → the would-be dashboard is not gettable.
	_, err = client.GetDashboard(ctx, &apiv1.GetDashboardRequest{Name: dry.GetName()})
	requireGRPCCode(t, err, codes.NotFound)

	// A real Create can reuse the same slug.
	_, err = client.CreateDashboard(ctx, &apiv1.CreateDashboardRequest{
		Parent:      fx.spaceParent,
		DashboardId: "vo-dash",
		Dashboard:   &apiv1.Dashboard{DisplayName: "Real"},
	})
	require.NoError(t, err, "validate_only must not have persisted the dashboard")

	// A dry-run that WOULD fail live (duplicate slug now exists) fails.
	_, err = client.CreateDashboard(ctx, &apiv1.CreateDashboardRequest{
		Parent:       fx.spaceParent,
		DashboardId:  "vo-dash",
		Dashboard:    &apiv1.Dashboard{DisplayName: "Dup"},
		ValidateOnly: true,
	})
	requireGRPCCode(t, err, codes.AlreadyExists)
}
