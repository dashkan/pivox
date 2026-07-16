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
