package requests_test

import (
	"context"
	"fmt"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	assetsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/assets/v1"
)

// mkRequest creates a request with the given display name under parent and
// returns its resource name.
func mkRequest(t *testing.T, ctx context.Context, client assetsv1.RequestsClient, parent, displayName string) string {
	t.Helper()
	op, err := client.CreateRequest(ctx, &assetsv1.CreateRequestRequest{
		Parent:  parent,
		Request: &assetsv1.Request{DisplayName: displayName},
	})
	require.NoError(t, err)
	require.True(t, op.GetDone())
	var r assetsv1.Request
	require.NoError(t, op.GetResponse().UnmarshalTo(&r))
	return r.GetName()
}

// drainRequests follows page tokens to completion, returning every request
// resource name across all pages, and fails if the page loop runs away (which
// is exactly the pre-migration bug: a never-decoded token re-serves page 1).
func drainRequests(t *testing.T, ctx context.Context, client assetsv1.RequestsClient, req *assetsv1.ListRequestsRequest) []string {
	t.Helper()
	var names []string
	token := ""
	for i := 0; i < 100; i++ {
		req.PageToken = token
		resp, err := client.ListRequests(ctx, req)
		require.NoError(t, err)
		for _, r := range resp.GetRequests() {
			names = append(names, r.GetName())
		}
		token = resp.GetNextPageToken()
		if token == "" {
			return names
		}
	}
	t.Fatal("pagination did not terminate within 100 pages")
	return nil
}

// TestE2E_ListRequests_KeysetDrain_DefaultID pins the core fix: draining every
// page via next_page_token under the default (id) order returns each of
// 2*pageSize+1 rows exactly once AND terminates. The pre-migration handler
// re-served page 1 forever (never-decoded token), which this catches as either
// a runaway loop or duplicate rows.
func TestE2E_ListRequests_KeysetDrain_DefaultID(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h, parent := newRequestsHarness(t, "reqkda", "spone")
	client := assetsv1.NewRequestsClient(h.Conn())
	ctx := context.Background()

	const pageSize = 3
	const total = 2*pageSize + 1 // 7
	for i := range total {
		mkRequest(t, ctx, client, parent, fmt.Sprintf("req-%02d", i))
	}

	got := drainRequests(t, ctx, client, &assetsv1.ListRequestsRequest{Parent: parent, PageSize: pageSize})
	assert.Len(t, got, total, "every row returned exactly once across pages")
	uniq := map[string]struct{}{}
	for _, n := range got {
		uniq[n] = struct{}{}
	}
	assert.Len(t, uniq, total, "no duplicate rows across page boundaries (token advances)")
}

// TestE2E_ListRequests_KeysetDrain_OrderByDisplayName pins the compound-cursor
// path: draining under a non-id sort (displayName), created out of order, must
// still cover every row once and stay sorted.
func TestE2E_ListRequests_KeysetDrain_OrderByDisplayName(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h, parent := newRequestsHarness(t, "reqkdo", "spone")
	client := assetsv1.NewRequestsClient(h.Conn())
	ctx := context.Background()

	// Names deliberately out of creation order so displayName order != id order.
	names := []string{"gg", "aa", "ee", "cc", "bb", "ff", "dd"}
	for _, n := range names {
		mkRequest(t, ctx, client, parent, n)
	}

	got := drainDisplayNames(t, ctx, client, &assetsv1.ListRequestsRequest{
		Parent: parent, OrderBy: "displayName", PageSize: 3,
	})
	assert.Equal(t, []string{"aa", "bb", "cc", "dd", "ee", "ff", "gg"}, got,
		"compound (displayName,id) cursor covers every row once, in sorted order")
}

// drainDisplayNames drains all pages and returns the display names in page order.
func drainDisplayNames(t *testing.T, ctx context.Context, client assetsv1.RequestsClient, req *assetsv1.ListRequestsRequest) []string {
	t.Helper()
	var out []string
	token := ""
	for i := 0; i < 100; i++ {
		req.PageToken = token
		resp, err := client.ListRequests(ctx, req)
		require.NoError(t, err)
		for _, r := range resp.GetRequests() {
			out = append(out, r.GetDisplayName())
		}
		token = resp.GetNextPageToken()
		if token == "" {
			return out
		}
	}
	t.Fatal("pagination did not terminate within 100 pages")
	return nil
}

// TestE2E_ListRequests_Filter pins AIP-160 filtering through the transpiler.
func TestE2E_ListRequests_Filter(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h, parent := newRequestsHarness(t, "reqflt", "spone")
	client := assetsv1.NewRequestsClient(h.Conn())
	ctx := context.Background()

	mkRequest(t, ctx, client, parent, "Hero Image")
	mkRequest(t, ctx, client, parent, "Hero Banner")
	mkRequest(t, ctx, client, parent, "Footer Logo")

	// Substring match on displayName.
	resp, err := client.ListRequests(ctx, &assetsv1.ListRequestsRequest{
		Parent: parent, Filter: `displayName : "Hero"`,
	})
	require.NoError(t, err)
	got := displayNamesOf(resp.GetRequests())
	sort.Strings(got)
	assert.Equal(t, []string{"Hero Banner", "Hero Image"}, got)

	// state = DRAFT matches all (new requests start DRAFT).
	resp, err = client.ListRequests(ctx, &assetsv1.ListRequestsRequest{
		Parent: parent, Filter: `state = DRAFT`,
	})
	require.NoError(t, err)
	assert.Len(t, resp.GetRequests(), 3)
}

func displayNamesOf(rs []*assetsv1.Request) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.GetDisplayName())
	}
	return out
}

// TestE2E_ListRequests_Rejections pins that bad inputs are InvalidArgument, not
// silent empty results.
func TestE2E_ListRequests_Rejections(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h, parent := newRequestsHarness(t, "reqrej", "spone")
	client := assetsv1.NewRequestsClient(h.Conn())
	ctx := context.Background()
	mkRequest(t, ctx, client, parent, "a")

	// Unknown filter field.
	_, err := client.ListRequests(ctx, &assetsv1.ListRequestsRequest{Parent: parent, Filter: `secret = "x"`})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))

	// Unknown order_by field — due_time is nullable, so it is deliberately NOT
	// sortable (registered filterable-only) and must be rejected here.
	_, err = client.ListRequests(ctx, &assetsv1.ListRequestsRequest{Parent: parent, OrderBy: "dueTime"})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))

	// Garbage page token.
	_, err = client.ListRequests(ctx, &assetsv1.ListRequestsRequest{Parent: parent, PageToken: "not-a-token"})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}
