package aichat_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	aiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/ai/v1"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
)

// These tests pin the keyset off-by-one across the four AiChat List
// handlers: with exactly pageSize+1 rows under one parent and a page
// size that forces a single boundary crossing, every row must come
// back exactly once — none dropped at the boundary, none duplicated.
// They fail against the old rows[pageSize] cursor (which encodes the
// first UN-returned row and then resumes strictly past it, skipping it)
// and pass once the cursor is the last RETURNED row via filter.Paginate.
//
// All four filters default to `id DESC` with an id-only cursor, so the
// sort column IS the cursor column — the id-only token is aligned and
// the only defect under test is which row gets encoded.

func assertEachOnce[T any](t *testing.T, got []T, total int, key func(T) string) {
	t.Helper()
	assert.Len(t, got, total, "every row returned exactly once across the page boundary (no drop)")
	uniq := make(map[string]struct{}, len(got))
	for _, v := range got {
		uniq[key(v)] = struct{}{}
	}
	assert.Len(t, uniq, total, "no duplicate rows across the page boundary")
}

func TestE2E_ListConversations_KeysetBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithAiChatServer())
	owned := h.SeedOwnedOrg(t, "chat-convs", "Chat Convs", "aichat")
	ctx := context.Background()
	client := aiv1.NewAiChatClient(h.Conn())
	parent := "organizations/" + owned.Slug + "/users/" + owned.Owner.IdentityID.String()

	const pageSize = 3
	const total = pageSize + 1 // exactly one boundary crossing
	for range total {
		h.SeedConversation(t, owned.Row.ID, owned.Owner, "t")
	}

	var got []string
	token := ""
	for range 100 {
		resp, err := client.ListConversations(ctx, &aiv1.ListConversationsRequest{
			Parent:    parent,
			PageSize:  pageSize,
			PageToken: token,
		})
		require.NoError(t, err)
		for _, c := range resp.GetConversations() {
			got = append(got, c.GetName())
		}
		if token = resp.GetNextPageToken(); token == "" {
			break
		}
	}
	assertEachOnce(t, got, total, func(s string) string { return s })
}

func TestE2E_ListMessages_KeysetBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithAiChatServer())
	owned := h.SeedOwnedOrg(t, "chat-msgs", "Chat Msgs", "aichat")
	ctx := context.Background()
	client := aiv1.NewAiChatClient(h.Conn())
	conv := h.SeedConversation(t, owned.Row.ID, owned.Owner, "parent")
	parent := "organizations/" + owned.Slug + "/users/" + owned.Owner.IdentityID.String() + "/conversations/" + conv.Name

	const pageSize = 3
	const total = pageSize + 1
	for i := range total {
		h.SeedMessage(t, conv.ID, int64(i+1))
	}

	var got []string
	token := ""
	for range 100 {
		resp, err := client.ListMessages(ctx, &aiv1.ListMessagesRequest{
			Parent:    parent,
			PageSize:  pageSize,
			PageToken: token,
		})
		require.NoError(t, err)
		for _, m := range resp.GetMessages() {
			got = append(got, m.GetName())
		}
		if token = resp.GetNextPageToken(); token == "" {
			break
		}
	}
	assertEachOnce(t, got, total, func(s string) string { return s })
}

func TestE2E_ListArtifacts_KeysetBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithAiChatServer())
	owned := h.SeedOwnedOrg(t, "chat-arts", "Chat Arts", "aichat")
	ctx := context.Background()
	client := aiv1.NewAiChatClient(h.Conn())
	conv := h.SeedConversation(t, owned.Row.ID, owned.Owner, "parent")
	parent := "organizations/" + owned.Slug + "/users/" + owned.Owner.IdentityID.String() + "/conversations/" + conv.Name

	const pageSize = 3
	const total = pageSize + 1
	for range total {
		h.SeedArtifact(t, conv.ID, "a")
	}

	var got []string
	token := ""
	for range 100 {
		resp, err := client.ListArtifacts(ctx, &aiv1.ListArtifactsRequest{
			Parent:    parent,
			PageSize:  pageSize,
			PageToken: token,
		})
		require.NoError(t, err)
		for _, a := range resp.GetArtifacts() {
			got = append(got, a.GetName())
		}
		if token = resp.GetNextPageToken(); token == "" {
			break
		}
	}
	assertEachOnce(t, got, total, func(s string) string { return s })
}

func TestE2E_ListArtifactVersions_KeysetBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithAiChatServer())
	owned := h.SeedOwnedOrg(t, "chat-vers", "Chat Vers", "aichat")
	ctx := context.Background()
	client := aiv1.NewAiChatClient(h.Conn())
	conv := h.SeedConversation(t, owned.Row.ID, owned.Owner, "parent")
	art := h.SeedArtifact(t, conv.ID, "parent")
	parent := "organizations/" + owned.Slug + "/users/" + owned.Owner.IdentityID.String() +
		"/conversations/" + conv.Name + "/artifacts/" + art.Name

	const pageSize = 3
	const total = pageSize + 1
	for range total {
		h.SeedArtifactVersion(t, art.ID)
	}

	var got []string
	token := ""
	for range 100 {
		resp, err := client.ListArtifactVersions(ctx, &aiv1.ListArtifactVersionsRequest{
			Parent:    parent,
			PageSize:  pageSize,
			PageToken: token,
		})
		require.NoError(t, err)
		for _, v := range resp.GetVersions() {
			got = append(got, v.GetName())
		}
		if token = resp.GetNextPageToken(); token == "" {
			break
		}
	}
	assertEachOnce(t, got, total, func(s string) string { return s })
}
