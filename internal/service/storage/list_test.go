package storage_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	storagev1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/storage/v1"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
)

// TestE2E_ListStorageGateways covers the connector "agent" dropdown's first
// hop: enumerating an org's storage gateways. Exercises the happy path, keyset
// pagination full-coverage, and cross-org isolation — all through the real
// interceptor chain (auth + membership + permission).
func TestE2E_ListStorageGateways(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t,
		grpcharness.WithOrganizationsServer(),
		grpcharness.WithStorageGatewaysServer())
	owned := h.SeedOwnedOrg(t, "gw-list", "GW List", "storage")
	ctx := context.Background()
	client := storagev1.NewStorageGatewaysClient(h.Conn())

	// Seed three gateways in the owned org.
	want := map[string]bool{
		"organizations/gw-list/storageGateways/alpha":   true,
		"organizations/gw-list/storageGateways/bravo":   true,
		"organizations/gw-list/storageGateways/charlie": true,
	}
	for _, name := range []string{"alpha", "bravo", "charlie"} {
		h.SeedStorageGateway(t, owned.Row.ID, name)
	}

	t.Run("lists all gateways under the org", func(t *testing.T) {
		resp, err := client.ListStorageGateways(ctx, &storagev1.ListStorageGatewaysRequest{
			Parent: "organizations/" + owned.Slug,
		})
		require.NoError(t, err)
		require.Len(t, resp.GetStorageGateways(), 3)
		got := map[string]bool{}
		for _, gw := range resp.GetStorageGateways() {
			got[gw.GetName()] = true
		}
		assert.Equal(t, want, got)
		assert.Empty(t, resp.GetNextPageToken(), "single full page has no next token")
	})

	t.Run("paginates with full coverage and no duplicates", func(t *testing.T) {
		// page_size=1 forces three pages + a terminal empty-token page.
		got := map[string]bool{}
		var token string
		pages := 0
		for {
			resp, err := client.ListStorageGateways(ctx, &storagev1.ListStorageGatewaysRequest{
				Parent:    "organizations/" + owned.Slug,
				PageSize:  1,
				PageToken: token,
			})
			require.NoError(t, err)
			pages++
			require.LessOrEqual(t, len(resp.GetStorageGateways()), 1)
			for _, gw := range resp.GetStorageGateways() {
				require.False(t, got[gw.GetName()], "gateway %s returned twice across pages", gw.GetName())
				got[gw.GetName()] = true
			}
			token = resp.GetNextPageToken()
			if token == "" {
				break
			}
			require.LessOrEqual(t, pages, 10, "pagination did not terminate")
		}
		// Every gateway appears exactly once — proves the keyset cursor uses the
		// last RETURNED row (no off-by-one dropping a row per page boundary).
		assert.Equal(t, want, got)
	})

	t.Run("excludes other orgs' gateways", func(t *testing.T) {
		// Same owner creates a second org and seeds a gateway there.
		op, err := apiv1.NewOrganizationsClient(h.Conn()).CreateOrganization(ctx,
			&apiv1.CreateOrganizationRequest{
				OrganizationId: "gw-list-b",
				Organization:   &apiv1.Organization{DisplayName: "GW List B"},
			})
		require.NoError(t, err)
		require.True(t, op.GetDone())
		orgB := h.LookupOrgID(t, "gw-list-b")
		h.SeedStorageGateway(t, orgB, "beta-only")

		// Listing org A must not surface org B's gateway.
		resp, err := client.ListStorageGateways(ctx, &storagev1.ListStorageGatewaysRequest{
			Parent: "organizations/" + owned.Slug,
		})
		require.NoError(t, err)
		for _, gw := range resp.GetStorageGateways() {
			assert.NotContains(t, gw.GetName(), "beta-only", "org A's list leaked org B's gateway")
		}
		assert.Len(t, resp.GetStorageGateways(), 3)

		// And org B's list surfaces only its own.
		respB, err := client.ListStorageGateways(ctx, &storagev1.ListStorageGatewaysRequest{
			Parent: "organizations/gw-list-b",
		})
		require.NoError(t, err)
		require.Len(t, respB.GetStorageGateways(), 1)
		assert.Equal(t, "organizations/gw-list-b/storageGateways/beta-only", respB.GetStorageGateways()[0].GetName())
	})
}

// TestE2E_ListStorageGateways_FilterAndOrder proves the migrated handler honors
// AIP-160 filter and AIP-132 order_by (both ignored before the filter-engine
// migration). Seeds three gateways whose display names equal their slugs so
// alphabetical order is well-defined.
func TestE2E_ListStorageGateways_FilterAndOrder(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t,
		grpcharness.WithOrganizationsServer(),
		grpcharness.WithStorageGatewaysServer())
	owned := h.SeedOwnedOrg(t, "gw-fo", "GW FO", "storage")
	ctx := context.Background()
	client := storagev1.NewStorageGatewaysClient(h.Conn())
	parent := "organizations/" + owned.Slug

	// Insertion order alpha,bravo,charlie == uuidv7/id order, so the pre-migration
	// id-ordered handler returns this same sequence for BOTH asc and desc — which
	// is exactly why the desc≠asc assertion below discriminates.
	for _, name := range []string{"alpha", "bravo", "charlie"} {
		h.SeedStorageGateway(t, owned.Row.ID, name)
	}

	displayNames := func(gws []*storagev1.StorageGateway) []string {
		out := make([]string, 0, len(gws))
		for _, gw := range gws {
			out = append(out, gw.GetDisplayName())
		}
		return out
	}

	t.Run("order_by displayName asc differs from desc", func(t *testing.T) {
		asc, err := client.ListStorageGateways(ctx, &storagev1.ListStorageGatewaysRequest{
			Parent: parent, OrderBy: "displayName",
		})
		require.NoError(t, err)
		desc, err := client.ListStorageGateways(ctx, &storagev1.ListStorageGatewaysRequest{
			Parent: parent, OrderBy: "displayName desc",
		})
		require.NoError(t, err)

		ascNames := displayNames(asc.GetStorageGateways())
		descNames := displayNames(desc.GetStorageGateways())
		assert.Equal(t, []string{"alpha", "bravo", "charlie"}, ascNames)
		assert.Equal(t, []string{"charlie", "bravo", "alpha"}, descNames)
		assert.NotEqual(t, ascNames, descNames, "desc order must differ from asc")
	})

	t.Run("filter narrows to a single gateway", func(t *testing.T) {
		resp, err := client.ListStorageGateways(ctx, &storagev1.ListStorageGatewaysRequest{
			Parent: parent, Filter: `displayName = "bravo"`,
		})
		require.NoError(t, err)
		require.Len(t, resp.GetStorageGateways(), 1)
		assert.Equal(t, "bravo", resp.GetStorageGateways()[0].GetDisplayName())
	})
}

// TestE2E_ListEndpoints proves the migrated ListEndpoints handler: happy-path
// enumeration, AIP order_by, AIP filter, and keyset pagination (endpoints had NO
// pagination before the migration). Endpoints are staged via the harness seed
// helper (they can be created via the API, but seeding keeps the test focused on
// the read path).
func TestE2E_ListEndpoints(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t,
		grpcharness.WithOrganizationsServer(),
		grpcharness.WithEndpointsServer())
	owned := h.SeedOwnedOrg(t, "ep-list", "EP List", "storage")
	ctx := context.Background()
	client := storagev1.NewEndpointsClient(h.Conn())

	gw := h.SeedStorageGateway(t, owned.Row.ID, "gw-1")
	parent := "organizations/" + owned.Slug + "/storageGateways/gw-1"
	for _, name := range []string{"e-alpha", "e-bravo", "e-charlie"} {
		h.SeedStorageEndpoint(t, gw.ID, name)
	}

	displayNames := func(eps []*storagev1.Endpoint) []string {
		out := make([]string, 0, len(eps))
		for _, ep := range eps {
			out = append(out, ep.GetDisplayName())
		}
		return out
	}

	t.Run("lists all endpoints under the gateway", func(t *testing.T) {
		resp, err := client.ListEndpoints(ctx, &storagev1.ListEndpointsRequest{Parent: parent})
		require.NoError(t, err)
		require.Len(t, resp.GetEndpoints(), 3)
	})

	t.Run("order_by displayName asc differs from desc", func(t *testing.T) {
		asc, err := client.ListEndpoints(ctx, &storagev1.ListEndpointsRequest{
			Parent: parent, OrderBy: "displayName",
		})
		require.NoError(t, err)
		desc, err := client.ListEndpoints(ctx, &storagev1.ListEndpointsRequest{
			Parent: parent, OrderBy: "displayName desc",
		})
		require.NoError(t, err)
		assert.Equal(t, []string{"e-alpha", "e-bravo", "e-charlie"}, displayNames(asc.GetEndpoints()))
		assert.Equal(t, []string{"e-charlie", "e-bravo", "e-alpha"}, displayNames(desc.GetEndpoints()))
	})

	t.Run("filter narrows to a single endpoint", func(t *testing.T) {
		resp, err := client.ListEndpoints(ctx, &storagev1.ListEndpointsRequest{
			Parent: parent, Filter: `displayName = "e-bravo"`,
		})
		require.NoError(t, err)
		require.Len(t, resp.GetEndpoints(), 1)
		assert.Equal(t, "e-bravo", resp.GetEndpoints()[0].GetDisplayName())
	})

	t.Run("paginates with full coverage and no duplicates", func(t *testing.T) {
		got := map[string]bool{}
		var token string
		pages := 0
		for {
			resp, err := client.ListEndpoints(ctx, &storagev1.ListEndpointsRequest{
				Parent: parent, PageSize: 1, PageToken: token,
			})
			require.NoError(t, err)
			pages++
			require.LessOrEqual(t, len(resp.GetEndpoints()), 1, "page_size=1 must cap the page at one row")
			for _, ep := range resp.GetEndpoints() {
				require.False(t, got[ep.GetDisplayName()], "endpoint %s returned twice", ep.GetDisplayName())
				got[ep.GetDisplayName()] = true
			}
			token = resp.GetNextPageToken()
			if token == "" {
				break
			}
			require.LessOrEqual(t, pages, 10, "pagination did not terminate")
		}
		assert.Equal(t, map[string]bool{"e-alpha": true, "e-bravo": true, "e-charlie": true}, got)
	})
}

// TestE2E_ListAgents covers the connector dropdown's second hop: enumerating a
// gateway's agents. Agents self-register (no create API), so they're staged via
// the harness seed helper. Pins that a gateway's list returns its own agents
// and excludes another gateway's.
func TestE2E_ListAgents(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t,
		grpcharness.WithOrganizationsServer(),
		grpcharness.WithAgentsServer())
	owned := h.SeedOwnedOrg(t, "ag-list", "Agent List", "storage")
	ctx := context.Background()
	client := storagev1.NewAgentsClient(h.Conn())

	gwA := h.SeedStorageGateway(t, owned.Row.ID, "gw-a")
	gwB := h.SeedStorageGateway(t, owned.Row.ID, "gw-b")

	h.SeedStorageAgent(t, gwA.ID, "node-a1", "10.1.0.1")
	h.SeedStorageAgent(t, gwA.ID, "node-a2", "10.1.0.2")
	h.SeedStorageAgent(t, gwB.ID, "node-b1", "10.2.0.1")

	t.Run("returns the gateway's agents", func(t *testing.T) {
		resp, err := client.ListAgents(ctx, &storagev1.ListAgentsRequest{
			Parent: "organizations/" + owned.Slug + "/storageGateways/gw-a",
		})
		require.NoError(t, err)
		require.Len(t, resp.GetAgents(), 2)
		hosts := map[string]bool{}
		for _, a := range resp.GetAgents() {
			hosts[a.GetHostname()] = true
			assert.Contains(t, a.GetName(), "organizations/"+owned.Slug+"/storageGateways/gw-a/agents/")
		}
		assert.Equal(t, map[string]bool{"node-a1": true, "node-a2": true}, hosts)
	})

	t.Run("excludes another gateway's agents", func(t *testing.T) {
		resp, err := client.ListAgents(ctx, &storagev1.ListAgentsRequest{
			Parent: "organizations/" + owned.Slug + "/storageGateways/gw-b",
		})
		require.NoError(t, err)
		require.Len(t, resp.GetAgents(), 1)
		assert.Equal(t, "node-b1", resp.GetAgents()[0].GetHostname())
	})
}

// TestE2E_ListAgents_FilterOrderPage proves the migrated ListAgents handler
// honors AIP order_by, AIP filter, and keyset pagination (agents had NO
// pagination before the migration).
func TestE2E_ListAgents_FilterOrderPage(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t,
		grpcharness.WithOrganizationsServer(),
		grpcharness.WithAgentsServer())
	owned := h.SeedOwnedOrg(t, "ag-fop", "Agent FOP", "storage")
	ctx := context.Background()
	client := storagev1.NewAgentsClient(h.Conn())

	gw := h.SeedStorageGateway(t, owned.Row.ID, "gw-1")
	parent := "organizations/" + owned.Slug + "/storageGateways/gw-1"
	// Insertion order == join_time/id order, so the pre-migration join_time-ordered
	// handler returns node-a1,node-a2,node-a3 for BOTH asc and desc — the desc≠asc
	// assertion discriminates.
	h.SeedStorageAgent(t, gw.ID, "node-a1", "10.1.0.1")
	h.SeedStorageAgent(t, gw.ID, "node-a2", "10.1.0.2")
	h.SeedStorageAgent(t, gw.ID, "node-a3", "10.1.0.3")

	hostnames := func(agents []*storagev1.Agent) []string {
		out := make([]string, 0, len(agents))
		for _, a := range agents {
			out = append(out, a.GetHostname())
		}
		return out
	}

	t.Run("order_by hostname asc differs from desc", func(t *testing.T) {
		asc, err := client.ListAgents(ctx, &storagev1.ListAgentsRequest{
			Parent: parent, OrderBy: "hostname",
		})
		require.NoError(t, err)
		desc, err := client.ListAgents(ctx, &storagev1.ListAgentsRequest{
			Parent: parent, OrderBy: "hostname desc",
		})
		require.NoError(t, err)
		assert.Equal(t, []string{"node-a1", "node-a2", "node-a3"}, hostnames(asc.GetAgents()))
		assert.Equal(t, []string{"node-a3", "node-a2", "node-a1"}, hostnames(desc.GetAgents()))
	})

	t.Run("filter narrows to a single agent", func(t *testing.T) {
		resp, err := client.ListAgents(ctx, &storagev1.ListAgentsRequest{
			Parent: parent, Filter: `hostname = "node-a2"`,
		})
		require.NoError(t, err)
		require.Len(t, resp.GetAgents(), 1)
		assert.Equal(t, "node-a2", resp.GetAgents()[0].GetHostname())
	})

	t.Run("paginates with full coverage and no duplicates", func(t *testing.T) {
		got := map[string]bool{}
		var token string
		pages := 0
		for {
			resp, err := client.ListAgents(ctx, &storagev1.ListAgentsRequest{
				Parent: parent, PageSize: 1, PageToken: token,
			})
			require.NoError(t, err)
			pages++
			require.LessOrEqual(t, len(resp.GetAgents()), 1, "page_size=1 must cap the page at one row")
			for _, a := range resp.GetAgents() {
				require.False(t, got[a.GetHostname()], "agent %s returned twice", a.GetHostname())
				got[a.GetHostname()] = true
			}
			token = resp.GetNextPageToken()
			if token == "" {
				break
			}
			require.LessOrEqual(t, pages, 10, "pagination did not terminate")
		}
		assert.Equal(t, map[string]bool{"node-a1": true, "node-a2": true, "node-a3": true}, got)
	})
}
