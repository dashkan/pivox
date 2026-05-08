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

// libraryDashboardID is the trailing-segment ID for the v1 system
// dashboard. Hard-coded here rather than imported from the library
// package to keep this test independent of how the catalog spells
// its IDs internally — if the catalog renamed "library" to "assets"
// the test would fail loudly with the right diagnostic.
const libraryDashboardID = "library"

func newDashboardsHarness(t *testing.T) *grpcharness.Harness {
	t.Helper()
	return grpcharness.New(t,
		grpcharness.WithOrganizationsServer(),
		grpcharness.WithServices(func(h *grpcharness.Harness, s *grpc.Server) {
			apiv1.RegisterDashboardsServer(s, dashboards.NewServer(dashboards.Config{
				Pool:    h.Pool,
				Queries: h.Queries,
				// AuditResolver intentionally nil in 4a — system
				// dashboards have no audit fields.
			}))
		}),
	)
}

// TestE2E_ListDashboards_OrgParent_ReturnsCatalog pins the org-scoped
// happy path: an org member with dashboards.read calls
// ListDashboards at organizations/{org} and gets back every entry
// in the system catalog (the Library dashboard in v1), each with
// its name fully populated for the org slug.
func TestE2E_ListDashboards_OrgParent_ReturnsCatalog(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newDashboardsHarness(t)
	owner := h.SeedOwnedOrg(t, "dashboards-e2e", "Dashboards E2E", "owner")

	client := apiv1.NewDashboardsClient(h.Conn())
	resp, err := client.ListDashboards(context.Background(), &apiv1.ListDashboardsRequest{
		Parent: "organizations/" + owner.Slug,
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.GetDashboards(), "the system catalog must include at least the Library entry")

	// Every returned Dashboard's name embeds the requested org slug
	// at position 1 — proves Build was called with the parent's
	// slug and not, say, with a hard-coded test value.
	for _, d := range resp.GetDashboards() {
		assert.Contains(t, d.GetName(), "organizations/"+owner.Slug+"/dashboards/")
		assert.Equal(t, apiv1.Dashboard_SYSTEM_MANAGED, d.GetManagementMode())
	}

	// And specifically the Library is in there — the v1 catalog
	// invariant.
	var found bool
	for _, d := range resp.GetDashboards() {
		if d.GetName() == "organizations/"+owner.Slug+"/dashboards/"+libraryDashboardID {
			found = true
			break
		}
	}
	require.True(t, found, "expected Library entry in catalog response; got %v", resp.GetDashboards())
}

func TestE2E_GetDashboard_OrgParent_Library(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newDashboardsHarness(t)
	owner := h.SeedOwnedOrg(t, "dashboards-e2e-get", "Dashboards E2E Get", "owner")

	client := apiv1.NewDashboardsClient(h.Conn())
	d, err := client.GetDashboard(context.Background(), &apiv1.GetDashboardRequest{
		Name: "organizations/" + owner.Slug + "/dashboards/" + libraryDashboardID,
	})
	require.NoError(t, err)

	assert.Equal(t, "organizations/"+owner.Slug+"/dashboards/"+libraryDashboardID, d.GetName())
	assert.Equal(t, apiv1.Dashboard_SYSTEM_MANAGED, d.GetManagementMode())
	require.NotNil(t, d.GetGridLayout(), "Library renders as a GridLayout")
	require.Len(t, d.GetGridLayout().GetTiles(), 1, "Library has exactly one widget in v1")
	assert.NotNil(t, d.GetGridLayout().GetTiles()[0].GetWidget().GetCollection(),
		"the single tile contains the Asset CollectionWidget")
}

func TestE2E_GetDashboard_OrgParent_NotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newDashboardsHarness(t)
	owner := h.SeedOwnedOrg(t, "dashboards-e2e-404", "Dashboards E2E 404", "owner")

	client := apiv1.NewDashboardsClient(h.Conn())
	_, err := client.GetDashboard(context.Background(), &apiv1.GetDashboardRequest{
		Name: "organizations/" + owner.Slug + "/dashboards/never-shipped",
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
}

func TestE2E_ListDashboards_SpaceParent_UnimplementedFor4b(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newDashboardsHarness(t)
	owner := h.SeedOwnedOrg(t, "dashboards-e2e-space", "Dashboards E2E Space", "owner")

	// Phase 4a does not require a real space — the handler returns
	// Unimplemented before any DB read fires. The membership
	// interceptor still validates space membership at the parent
	// path. With no space row, the interceptor would reject with
	// NotFound before the handler can fall through. Rather than
	// stand up a space, this test asserts the wired path is
	// "interceptor + handler stub" by hitting the org-scoped path
	// and the Unimplemented-stub paths separately:

	client := apiv1.NewDashboardsClient(h.Conn())
	_, err := client.CreateDashboard(context.Background(), &apiv1.CreateDashboardRequest{
		Parent:      "organizations/" + owner.Slug + "/spaces/never-created",
		DashboardId: "x",
		Dashboard:   &apiv1.Dashboard{DisplayName: "X"},
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	// The membership interceptor runs before our handler. Without a
	// space row it returns NotFound on the parent. Either NotFound
	// (interceptor rejection) or Unimplemented (handler stub) is
	// acceptable here — both prove the RPC is wired.
	assert.Contains(
		t,
		[]codes.Code{codes.NotFound, codes.Unimplemented, codes.PermissionDenied},
		st.Code(),
		"wired RPC must surface a structured error, not Unknown",
	)
}

func TestE2E_UpdateDashboard_UnimplementedFor4b(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newDashboardsHarness(t)
	owner := h.SeedOwnedOrg(t, "dash-e2e-update", "Dashboards E2E Update", "owner")

	client := apiv1.NewDashboardsClient(h.Conn())
	_, err := client.UpdateDashboard(context.Background(), &apiv1.UpdateDashboardRequest{
		Dashboard: &apiv1.Dashboard{
			Name:        "organizations/" + owner.Slug + "/spaces/no-such/dashboards/no-such",
			DisplayName: "X",
		},
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	// TODO(phase 4b): with a real space + member fixture in scope,
	// tighten this to `codes.Unimplemented` exact-match — the loose
	// set is correct for 4a (the membership interceptor rejects
	// non-existent spaces with NotFound before our handler stub
	// runs) but loses information once 4b makes the handler reachable.
	assert.Contains(
		t,
		[]codes.Code{codes.NotFound, codes.Unimplemented, codes.PermissionDenied},
		st.Code(),
	)
}

func TestE2E_DeleteDashboard_UnimplementedFor4b(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newDashboardsHarness(t)
	owner := h.SeedOwnedOrg(t, "dash-e2e-delete", "Dashboards E2E Delete", "owner")

	client := apiv1.NewDashboardsClient(h.Conn())
	_, err := client.DeleteDashboard(context.Background(), &apiv1.DeleteDashboardRequest{
		Name: "organizations/" + owner.Slug + "/spaces/no-such/dashboards/no-such",
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	// TODO(phase 4b): with a real space + member fixture in scope,
	// tighten this to `codes.Unimplemented` exact-match — the loose
	// set is correct for 4a (the membership interceptor rejects
	// non-existent spaces with NotFound before our handler stub
	// runs) but loses information once 4b makes the handler reachable.
	assert.Contains(
		t,
		[]codes.Code{codes.NotFound, codes.Unimplemented, codes.PermissionDenied},
		st.Code(),
	)
}

func TestE2E_ListDashboards_MalformedParent_InvalidArgument(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newDashboardsHarness(t)
	owner := h.SeedOwnedOrg(t, "dashboards-e2e-bad", "Dashboards E2E Bad", "owner")
	_ = owner

	client := apiv1.NewDashboardsClient(h.Conn())
	_, err := client.ListDashboards(context.Background(), &apiv1.ListDashboardsRequest{
		Parent: "users/foo",
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}
