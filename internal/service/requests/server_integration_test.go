//go:build dev

package requests_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	db "github.com/dashkan/pivox/internal/db/generated"
	assetsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/assets/v1"
	"github.com/dashkan/pivox/internal/service/requests"
	"github.com/dashkan/pivox/internal/testutil"
)

func createTestOrg(t *testing.T, queries *db.Queries, name string) db.Organization {
	t.Helper()
	org, err := queries.CreateOrganization(context.Background(), db.CreateOrganizationParams{
		ID:          uuid.New(),
		Name:        name,
		DisplayName: "Test Org " + name,
		CreatedBy:   "test",
	})
	require.NoError(t, err)
	return org
}

func createTestSpace(t *testing.T, queries *db.Queries, orgID uuid.UUID, name string) db.Space {
	t.Helper()
	space, err := queries.CreateSpace(context.Background(), db.CreateSpaceParams{
		ID:          uuid.New(),
		OrgID:       orgID,
		Name:        name,
		DisplayName: "Test Space " + name,
		Labels:      json.RawMessage("{}"),
		CreatedBy:   "test",
	})
	require.NoError(t, err)
	return space
}

func TestIntegration_Requests_ApproveWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	_, queries, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	conn := testutil.SetupGRPCServer(t, func(s *grpc.Server) {
		assetsv1.RegisterRequestsServer(s, requests.NewRequestsServer(queries))
	})

	client := assetsv1.NewRequestsClient(conn)
	ctx := context.Background()

	// Prerequisite: create org and space directly via DB.
	org := createTestOrg(t, queries, "acme")
	createTestSpace(t, queries, org.ID, "proj1")

	parent := "organizations/acme/spaces/proj1"
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

	_, queries, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	conn := testutil.SetupGRPCServer(t, func(s *grpc.Server) {
		assetsv1.RegisterRequestsServer(s, requests.NewRequestsServer(queries))
	})

	client := assetsv1.NewRequestsClient(conn)
	ctx := context.Background()

	org := createTestOrg(t, queries, "acme")
	createTestSpace(t, queries, org.ID, "proj1")
	parent := "organizations/acme/spaces/proj1"

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
		// Delete one request via cancel workflow.
		listResp, err := client.ListRequests(ctx, &assetsv1.ListRequestsRequest{
			Parent: parent,
		})
		require.NoError(t, err)
		require.NotEmpty(t, listResp.GetRequests())

		_, err = client.CancelRequest(ctx, &assetsv1.CancelRequestRequest{
			Name: listResp.GetRequests()[0].GetName(),
		})
		require.NoError(t, err)

		// Without show_deleted — cancelled requests are NOT soft-deleted, they still show up.
		// But the code path with show_deleted uses a different query.
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

	_, queries, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	conn := testutil.SetupGRPCServer(t, func(s *grpc.Server) {
		assetsv1.RegisterRequestsServer(s, requests.NewRequestsServer(queries))
	})

	client := assetsv1.NewRequestsClient(conn)
	ctx := context.Background()

	org := createTestOrg(t, queries, "acme")
	createTestSpace(t, queries, org.ID, "proj1")

	parent := "organizations/acme/spaces/proj1"

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

	_, queries, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	conn := testutil.SetupGRPCServer(t, func(s *grpc.Server) {
		assetsv1.RegisterRequestsServer(s, requests.NewRequestsServer(queries))
	})

	client := assetsv1.NewRequestsClient(conn)
	ctx := context.Background()

	org := createTestOrg(t, queries, "acme")
	createTestSpace(t, queries, org.ID, "proj1")

	parent := "organizations/acme/spaces/proj1"

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
