//go:build dev

package requests_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	assetsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/assets/v1"
	"github.com/dashkan/pivox/internal/service/requests"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
)

// newRequestsHarness wires the harness with Organizations + Spaces +
// Requests servers, seeds an owned org+space, and returns the
// harness ready for request-level subtests.
//
// Each top-level test gets its own harness (and its own DB) — the
// alternative would be sharing one harness across tests, but
// per-test isolation matches the rest of the integration suite and
// keeps subtest-level retry/parallelism simple.
func newRequestsHarness(t *testing.T, orgSlug, spaceSlug string) (*grpcharness.Harness, string) {
	t.Helper()
	h := grpcharness.New(t,
		grpcharness.WithOrganizationsServer(),
		grpcharness.WithSpacesServer(),
		grpcharness.WithServices(func(h *grpcharness.Harness, s *grpc.Server) {
			assetsv1.RegisterRequestsServer(s, requests.NewRequestsServer(requests.Config{
				Pool: h.Pool, Queries: h.Queries,
			}))
		}))
	h.SeedOwnedOrg(t, orgSlug, "Acme", "requests")
	h.SeedOwnedSpace(t, orgSlug, spaceSlug, "Project")
	return h, "organizations/" + orgSlug + "/spaces/" + spaceSlug
}

func TestIntegration_Requests_ApproveWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h, parent := newRequestsHarness(t, "acme", "proj1")
	client := assetsv1.NewRequestsClient(h.Conn())
	ctx := context.Background()

	var requestName string

	t.Run("Create", func(t *testing.T) {
		op, err := client.CreateRequest(ctx, &assetsv1.CreateRequestRequest{
			Parent: parent,
			Request: &assetsv1.Request{
				DisplayName: "Need Hero Image",
				Description: "Full bleed hero for homepage",
				LineItems: []*assetsv1.LineItem{
					{DisplayName: "Hero Image 1920x1080"},
					{DisplayName: "Hero Image Mobile"},
				},
			},
		})
		require.NoError(t, err)
		assert.True(t, op.GetDone())

		var req assetsv1.Request
		require.NoError(t, op.GetResponse().UnmarshalTo(&req))
		assert.Equal(t, assetsv1.Request_DRAFT, req.GetState())
		assert.NotEmpty(t, req.GetName())
		requestName = req.GetName()
	})

	t.Run("GetRequest_WithLineItems", func(t *testing.T) {
		resp, err := client.GetRequest(ctx, &assetsv1.GetRequestRequest{
			Name: requestName,
		})
		require.NoError(t, err)
		assert.Equal(t, requestName, resp.GetName())
		assert.Equal(t, assetsv1.Request_DRAFT, resp.GetState())
		assert.Equal(t, int32(2), resp.GetLineItemCount())
		assert.Len(t, resp.GetLineItems(), 2)
	})

	t.Run("Submit", func(t *testing.T) {
		resp, err := client.SubmitRequest(ctx, &assetsv1.SubmitRequestRequest{
			Name: requestName,
		})
		require.NoError(t, err)
		assert.Equal(t, assetsv1.Request_OPEN, resp.GetState())
	})

	t.Run("Assign", func(t *testing.T) {
		resp, err := client.AssignRequest(ctx, &assetsv1.AssignRequestRequest{
			Name:     requestName,
			Assignee: "users/jane",
		})
		require.NoError(t, err)
		assert.Equal(t, assetsv1.Request_IN_PROGRESS, resp.GetState())
		assert.Equal(t, "users/jane", resp.GetAssignee())
	})

	t.Run("Deliver", func(t *testing.T) {
		resp, err := client.DeliverRequest(ctx, &assetsv1.DeliverRequestRequest{
			Name: requestName,
		})
		require.NoError(t, err)
		assert.Equal(t, assetsv1.Request_DELIVERED, resp.GetState())
	})

	t.Run("Approve", func(t *testing.T) {
		resp, err := client.ApproveRequest(ctx, &assetsv1.ApproveRequestRequest{
			Name: requestName,
		})
		require.NoError(t, err)
		assert.Equal(t, assetsv1.Request_APPROVED, resp.GetState())
	})
}

func TestIntegration_Requests_ListRequests(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h, parent := newRequestsHarness(t, "acme", "proj1")
	client := assetsv1.NewRequestsClient(h.Conn())
	ctx := context.Background()

	// Create multiple requests.
	for i := range 3 {
		op, err := client.CreateRequest(ctx, &assetsv1.CreateRequestRequest{
			Parent: parent,
			Request: &assetsv1.Request{
				DisplayName: fmt.Sprintf("Request %d", i),
			},
		})
		require.NoError(t, err)
		assert.True(t, op.GetDone())
	}

	t.Run("list_all", func(t *testing.T) {
		resp, err := client.ListRequests(ctx, &assetsv1.ListRequestsRequest{
			Parent: parent,
		})
		require.NoError(t, err)
		assert.Len(t, resp.GetRequests(), 3)
	})

	t.Run("list_with_show_deleted", func(t *testing.T) {
		// Cancel one request — cancelled requests are NOT
		// soft-deleted, but the show_deleted code path uses a
		// different query so we still exercise it here.
		listResp, err := client.ListRequests(ctx, &assetsv1.ListRequestsRequest{
			Parent: parent,
		})
		require.NoError(t, err)
		require.NotEmpty(t, listResp.GetRequests())

		_, err = client.CancelRequest(ctx, &assetsv1.CancelRequestRequest{
			Name: listResp.GetRequests()[0].GetName(),
		})
		require.NoError(t, err)

		withDeleted, err := client.ListRequests(ctx, &assetsv1.ListRequestsRequest{
			Parent:      parent,
			ShowDeleted: true,
		})
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(withDeleted.GetRequests()), 3)
	})

	t.Run("list_pagination", func(t *testing.T) {
		resp, err := client.ListRequests(ctx, &assetsv1.ListRequestsRequest{
			Parent:   parent,
			PageSize: 1,
		})
		require.NoError(t, err)
		assert.Len(t, resp.GetRequests(), 1)
		assert.NotEmpty(t, resp.GetNextPageToken())
	})
}

func TestIntegration_Requests_RejectWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h, parent := newRequestsHarness(t, "acme", "proj1")
	client := assetsv1.NewRequestsClient(h.Conn())
	ctx := context.Background()

	// Create and drive to DELIVERED, then reject.
	op, err := client.CreateRequest(ctx, &assetsv1.CreateRequestRequest{
		Parent: parent,
		Request: &assetsv1.Request{
			DisplayName: "Reject Test",
			LineItems:   []*assetsv1.LineItem{{DisplayName: "Item"}},
		},
	})
	require.NoError(t, err)
	var req assetsv1.Request
	require.NoError(t, op.GetResponse().UnmarshalTo(&req))
	name := req.GetName()

	_, err = client.SubmitRequest(ctx, &assetsv1.SubmitRequestRequest{Name: name})
	require.NoError(t, err)
	_, err = client.AssignRequest(ctx, &assetsv1.AssignRequestRequest{Name: name, Assignee: "users/bob"})
	require.NoError(t, err)
	_, err = client.DeliverRequest(ctx, &assetsv1.DeliverRequestRequest{Name: name})
	require.NoError(t, err)

	resp, err := client.RejectRequest(ctx, &assetsv1.RejectRequestRequest{Name: name})
	require.NoError(t, err)
	assert.Equal(t, assetsv1.Request_REJECTED, resp.GetState())
}

func TestIntegration_Requests_CancelWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h, parent := newRequestsHarness(t, "acme", "proj1")
	client := assetsv1.NewRequestsClient(h.Conn())
	ctx := context.Background()

	op, err := client.CreateRequest(ctx, &assetsv1.CreateRequestRequest{
		Parent: parent,
		Request: &assetsv1.Request{
			DisplayName: "Cancel Test",
		},
	})
	require.NoError(t, err)
	var req assetsv1.Request
	require.NoError(t, op.GetResponse().UnmarshalTo(&req))

	resp, err := client.CancelRequest(ctx, &assetsv1.CancelRequestRequest{
		Name: req.GetName(),
	})
	require.NoError(t, err)
	assert.Equal(t, assetsv1.Request_CANCELLED, resp.GetState())
}
