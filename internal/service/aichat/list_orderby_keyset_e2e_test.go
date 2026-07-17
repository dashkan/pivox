package aichat_test

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	aiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/ai/v1"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
)

// createTimeScramble is a fixed permutation of minute offsets applied to a row's
// create_time in seed (== id) order. It is neither ascending nor descending, so
// order_by=createTime walks a sequence that differs from BOTH the default id
// order AND id-reversed order. That makes the createTime boundary tests genuine
// ordering-correctness tests (an implementation that ignored order_by and fell
// back to id order would return a different sequence and fail), while still
// exercising the keyset boundary (each row exactly once).
var createTimeScramble = []int{2, 5, 0, 3, 6, 1, 4}

// expectedByScramble returns names reordered into createTime-ascending order:
// names[i] carries create_time offset createTimeScramble[i], so the sorted
// sequence is names indexed by the ascending-offset permutation.
func expectedByScramble(names []string) []string {
	type row struct {
		minute int
		name   string
	}
	rows := make([]row, len(names))
	for i, n := range names {
		rows[i] = row{minute: createTimeScramble[i], name: n}
	}
	sort.Slice(rows, func(a, b int) bool { return rows[a].minute < rows[b].minute })
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.name
	}
	return out
}

// reversed returns a copy of s in reverse order — the expected newest-first
// (DefaultOrder "id desc") sequence for rows seeded in creation (id-ascending)
// order under uuidv7.
func reversed(s []string) []string {
	out := slices.Clone(s)
	slices.Reverse(out)
	return out
}

// These tests pin the compound-cursor keyset migration of the four AiChat List
// handlers off the legacy id-only filter.Query path. Each handler now exposes
// non-id sortable columns (title / createTime) whose order can disagree with id
// order; the legacy path emitted `ORDER BY <col>` but resumed with an id-only
// cursor (`id < $cursor`, CursorDirection DESC), so sort and keyset disagreed
// and rows dropped/duplicated across page boundaries. The compound (col, id)
// cursor keeps the resume predicate aligned with the ORDER BY.
//
// The DefaultOrder knob restores the pre-migration newest-first default: with no
// order_by the list resolves rf.DefaultOrder ("id desc") to the id-only DESC
// keyset. The *DefaultOrderNewestFirst tests below are the regression guard the
// whole change exists to protect — they must stay green (an ASC-default
// regression, as an intermediate migration once shipped, would flip them red).
//
// Empirical red for the *KeysetBoundary tests: with the production handlers on
// filter.Query, each returns a set that is short (rows dropped) and/or contains
// duplicates. The mirror is spaces_list_e2e_test.go.

// drainConvNames follows next_page_token to completion for ListConversations.
func drainConvNames(t *testing.T, ctx context.Context, client aiv1.AiChatClient, req *aiv1.ListConversationsRequest) []string {
	t.Helper()
	var names []string
	token := ""
	for range 100 {
		req.PageToken = token
		resp, err := client.ListConversations(ctx, req)
		require.NoError(t, err)
		for _, c := range resp.GetConversations() {
			names = append(names, c.GetName())
		}
		if token = resp.GetNextPageToken(); token == "" {
			return names
		}
	}
	t.Fatal("pagination did not terminate within 100 pages")
	return nil
}

// drainConvTitles follows next_page_token to completion, returning titles in
// page order (which, since pages are contiguous and each page is sorted, is the
// globally sorted sequence).
func drainConvTitles(t *testing.T, ctx context.Context, client aiv1.AiChatClient, req *aiv1.ListConversationsRequest) []string {
	t.Helper()
	var titles []string
	token := ""
	for range 100 {
		req.PageToken = token
		resp, err := client.ListConversations(ctx, req)
		require.NoError(t, err)
		for _, c := range resp.GetConversations() {
			titles = append(titles, c.GetTitle())
		}
		if token = resp.GetNextPageToken(); token == "" {
			return titles
		}
	}
	t.Fatal("pagination did not terminate within 100 pages")
	return nil
}

func assertUnique(t *testing.T, got []string, total int) {
	t.Helper()
	assert.Len(t, got, total, "every row returned exactly once across the boundary (no drop)")
	uniq := make(map[string]struct{}, len(got))
	for _, v := range got {
		uniq[v] = struct{}{}
	}
	assert.Len(t, uniq, total, "no duplicate rows across the boundary")
}

// TestE2E_ListConversations_OrderByTitle pins that a custom sort actually sorts:
// conversations created out of alphabetical order come back ordered by title,
// ascending and descending.
func TestE2E_ListConversations_OrderByTitle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithAiChatServer())
	owned := h.SeedOwnedOrg(t, "cv-order", "Cv Order", "aichat")
	ctx := context.Background()
	client := aiv1.NewAiChatClient(h.Conn())
	parent := "organizations/" + owned.Slug + "/users/" + owned.Owner.IdentityID.String()

	// Create in a non-alphabetical order so title order differs from id order.
	for _, title := range []string{"charlie", "alpha", "bravo"} {
		h.SeedConversation(t, owned.Row.ID, owned.Owner, title)
	}

	got := drainConvTitles(t, ctx, client, &aiv1.ListConversationsRequest{Parent: parent, OrderBy: "title"})
	assert.Equal(t, []string{"alpha", "bravo", "charlie"}, got)

	got = drainConvTitles(t, ctx, client, &aiv1.ListConversationsRequest{Parent: parent, OrderBy: "title desc"})
	assert.Equal(t, []string{"charlie", "bravo", "alpha"}, got)
}

// TestE2E_ListConversations_OrderByTitleKeysetBoundary is THE test this
// migration exists for. Titles are created in ASCENDING order (so title order
// equals id order), then paginated with order_by=title at a page size that
// forces boundary crossings. The legacy id-only cursor resumes with
// `id < $cursor` (CursorDirection DESC) while ORDER BY title ASC walks ids
// ascending — the two disagree, so page 2 re-returns already-seen rows and drops
// unseen ones. The compound (title, id) cursor keeps them aligned.
func TestE2E_ListConversations_OrderByTitleKeysetBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithAiChatServer())
	owned := h.SeedOwnedOrg(t, "cv-obkb", "Cv OBKB", "aichat")
	ctx := context.Background()
	client := aiv1.NewAiChatClient(h.Conn())
	parent := "organizations/" + owned.Slug + "/users/" + owned.Owner.IdentityID.String()

	const pageSize = 3
	titles := []string{"aa", "bb", "cc", "dd", "ee", "ff", "gg"} // ascending == id order
	require.Greater(t, len(titles), pageSize, "must cross at least one page boundary")
	for _, tl := range titles {
		h.SeedConversation(t, owned.Row.ID, owned.Owner, tl)
	}

	got := drainConvTitles(t, ctx, client, &aiv1.ListConversationsRequest{
		Parent: parent, OrderBy: "title", PageSize: pageSize,
	})
	assert.Equal(t, titles, got,
		"compound cursor returns every conversation once, in title order, across boundaries")
	assertUnique(t, got, len(titles))
}

// TestE2E_ListConversations_DefaultOrderNewestFirst is the DefaultOrder ("id
// desc") regression guard: with NO order_by, conversations seeded in creation
// order come back newest-first (reverse of seed order), and pagination across a
// boundary returns every row exactly once. A regression to an id-ASC default
// (oldest-first) — which an intermediate migration once shipped — flips this
// red.
func TestE2E_ListConversations_DefaultOrderNewestFirst(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithAiChatServer())
	owned := h.SeedOwnedOrg(t, "cv-def", "Cv Def", "aichat")
	ctx := context.Background()
	client := aiv1.NewAiChatClient(h.Conn())
	parent := "organizations/" + owned.Slug + "/users/" + owned.Owner.IdentityID.String()

	const pageSize = 3
	titles := []string{"aa", "bb", "cc", "dd", "ee", "ff", "gg"} // seeded oldest→newest
	for _, tl := range titles {
		h.SeedConversation(t, owned.Row.ID, owned.Owner, tl)
	}

	got := drainConvTitles(t, ctx, client, &aiv1.ListConversationsRequest{Parent: parent, PageSize: pageSize})
	assert.Equal(t, reversed(titles), got,
		"no order_by defaults to newest-first (id desc), every row once across boundaries")
	assertUnique(t, got, len(titles))
}

// TestE2E_ListConversations_OrderByDuplicateTitleKeysetBoundary stresses the id
// tiebreaker: every conversation shares the SAME title, so the compound cursor
// must fall through to id to avoid dropping or repeating rows at a boundary.
func TestE2E_ListConversations_OrderByDuplicateTitleKeysetBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithAiChatServer())
	owned := h.SeedOwnedOrg(t, "cv-dupkey", "Cv DupKey", "aichat")
	ctx := context.Background()
	client := aiv1.NewAiChatClient(h.Conn())
	parent := "organizations/" + owned.Slug + "/users/" + owned.Owner.IdentityID.String()

	const n = 8
	for range n {
		h.SeedConversation(t, owned.Row.ID, owned.Owner, "same-title")
	}

	got := drainConvNames(t, ctx, client, &aiv1.ListConversationsRequest{
		Parent: parent, OrderBy: "title", PageSize: 3,
	})
	assertUnique(t, got, n)
}

// TestE2E_ListArtifacts_OrderByTitleKeysetBoundary mirrors the conversation
// title-boundary test for artifacts (parent = conversation).
func TestE2E_ListArtifacts_OrderByTitleKeysetBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithAiChatServer())
	owned := h.SeedOwnedOrg(t, "art-obkb", "Art OBKB", "aichat")
	ctx := context.Background()
	client := aiv1.NewAiChatClient(h.Conn())
	conv := h.SeedConversation(t, owned.Row.ID, owned.Owner, "parent")
	parent := "organizations/" + owned.Slug + "/users/" + owned.Owner.IdentityID.String() + "/conversations/" + conv.Name

	const pageSize = 3
	titles := []string{"aa", "bb", "cc", "dd", "ee", "ff", "gg"} // ascending == id order
	require.Greater(t, len(titles), pageSize, "must cross at least one page boundary")
	for _, tl := range titles {
		h.SeedArtifact(t, conv.ID, tl)
	}

	got := drainArtifactTitles(t, ctx, client, &aiv1.ListArtifactsRequest{Parent: parent, OrderBy: "title", PageSize: pageSize})
	assert.Equal(t, titles, got,
		"compound cursor returns every artifact once, in title order, across boundaries")
	assertUnique(t, got, len(titles))
}

// TestE2E_ListArtifacts_DefaultOrderNewestFirst is the DefaultOrder guard for
// artifacts: no order_by → newest-first (reverse of seed order).
func TestE2E_ListArtifacts_DefaultOrderNewestFirst(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithAiChatServer())
	owned := h.SeedOwnedOrg(t, "art-def", "Art Def", "aichat")
	ctx := context.Background()
	client := aiv1.NewAiChatClient(h.Conn())
	conv := h.SeedConversation(t, owned.Row.ID, owned.Owner, "parent")
	parent := "organizations/" + owned.Slug + "/users/" + owned.Owner.IdentityID.String() + "/conversations/" + conv.Name

	const pageSize = 3
	titles := []string{"aa", "bb", "cc", "dd", "ee", "ff", "gg"}
	for _, tl := range titles {
		h.SeedArtifact(t, conv.ID, tl)
	}

	got := drainArtifactTitles(t, ctx, client, &aiv1.ListArtifactsRequest{Parent: parent, PageSize: pageSize})
	assert.Equal(t, reversed(titles), got, "no order_by defaults to newest-first (id desc)")
	assertUnique(t, got, len(titles))
}

// drainArtifactTitles follows next_page_token to completion for ListArtifacts.
func drainArtifactTitles(t *testing.T, ctx context.Context, client aiv1.AiChatClient, req *aiv1.ListArtifactsRequest) []string {
	t.Helper()
	var titles []string
	token := ""
	for range 100 {
		req.PageToken = token
		resp, err := client.ListArtifacts(ctx, req)
		require.NoError(t, err)
		for _, a := range resp.GetArtifacts() {
			titles = append(titles, a.GetTitle())
		}
		if token = resp.GetNextPageToken(); token == "" {
			return titles
		}
	}
	t.Fatal("pagination did not terminate within 100 pages")
	return nil
}

// setCreateTime rewrites a row's create_time so createTime order can be made to
// disagree with id order deterministically (the message / artifact-version
// tables expose only createTime as a sortable, with no user-settable text
// column to lean on like title). Uses the exported harness pool directly — no
// sqlc query exists for backdating an audit timestamp, and none should.
func setCreateTime(t *testing.T, h *grpcharness.Harness, table string, id uuid.UUID, ts time.Time) {
	t.Helper()
	_, err := h.Pool.Exec(context.Background(),
		fmt.Sprintf("UPDATE %s SET create_time = $1 WHERE id = $2", table), ts, id)
	require.NoError(t, err)
}

// TestE2E_ListMessages_OrderByCreateTimeKeysetBoundary exercises the compound
// cursor on messages, whose only non-id sortable is createTime. create_time is
// scrambled relative to id order (createTimeScramble), so order_by=createTime
// walks a sequence that differs from BOTH the default id order (proving the sort
// is honored, not ignored) and the legacy id-only DESC cursor's resume order
// (proving the boundary is correct). The legacy path drops rows here; the
// compound (create_time, id) cursor returns every row once, in create_time order.
func TestE2E_ListMessages_OrderByCreateTimeKeysetBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithAiChatServer())
	owned := h.SeedOwnedOrg(t, "msg-obkb", "Msg OBKB", "aichat")
	ctx := context.Background()
	client := aiv1.NewAiChatClient(h.Conn())
	conv := h.SeedConversation(t, owned.Row.ID, owned.Owner, "parent")
	parent := "organizations/" + owned.Slug + "/users/" + owned.Owner.IdentityID.String() + "/conversations/" + conv.Name

	const pageSize = 3
	total := len(createTimeScramble)
	base := time.Now().Add(-time.Hour)
	seededNames := make([]string, total) // in id (seed) order
	for i := range total {
		m := h.SeedMessage(t, conv.ID, int64(i+1))
		seededNames[i] = parent + "/messages/" + m.Name
		setCreateTime(t, h, "ai_messages", m.ID, base.Add(time.Duration(createTimeScramble[i])*time.Minute))
	}

	got := drainMessageNames(t, ctx, client, &aiv1.ListMessagesRequest{Parent: parent, OrderBy: "createTime", PageSize: pageSize})
	assert.Equal(t, expectedByScramble(seededNames), got,
		"compound cursor returns every message once, in create_time order, across boundaries")
	assertUnique(t, got, total)
}

// TestE2E_ListMessages_DefaultOrderNewestFirst is the DefaultOrder guard for
// messages: no order_by → newest-first (reverse of seed order).
func TestE2E_ListMessages_DefaultOrderNewestFirst(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithAiChatServer())
	owned := h.SeedOwnedOrg(t, "msg-def", "Msg Def", "aichat")
	ctx := context.Background()
	client := aiv1.NewAiChatClient(h.Conn())
	conv := h.SeedConversation(t, owned.Row.ID, owned.Owner, "parent")
	parent := "organizations/" + owned.Slug + "/users/" + owned.Owner.IdentityID.String() + "/conversations/" + conv.Name

	const pageSize = 3
	const total = 7
	seededNames := make([]string, total)
	for i := range total {
		m := h.SeedMessage(t, conv.ID, int64(i+1))
		seededNames[i] = parent + "/messages/" + m.Name
	}

	got := drainMessageNames(t, ctx, client, &aiv1.ListMessagesRequest{Parent: parent, PageSize: pageSize})
	assert.Equal(t, reversed(seededNames), got, "no order_by defaults to newest-first (id desc)")
	assertUnique(t, got, total)
}

// drainMessageNames follows next_page_token to completion for ListMessages.
func drainMessageNames(t *testing.T, ctx context.Context, client aiv1.AiChatClient, req *aiv1.ListMessagesRequest) []string {
	t.Helper()
	var names []string
	token := ""
	for range 100 {
		req.PageToken = token
		resp, err := client.ListMessages(ctx, req)
		require.NoError(t, err)
		for _, m := range resp.GetMessages() {
			names = append(names, m.GetName())
		}
		if token = resp.GetNextPageToken(); token == "" {
			return names
		}
	}
	t.Fatal("pagination did not terminate within 100 pages")
	return nil
}

// TestE2E_ListArtifactVersions_OrderByCreateTimeKeysetBoundary mirrors the
// message createTime-boundary test for artifact versions (parent = artifact).
func TestE2E_ListArtifactVersions_OrderByCreateTimeKeysetBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithAiChatServer())
	owned := h.SeedOwnedOrg(t, "ver-obkb", "Ver OBKB", "aichat")
	ctx := context.Background()
	client := aiv1.NewAiChatClient(h.Conn())
	conv := h.SeedConversation(t, owned.Row.ID, owned.Owner, "parent")
	art := h.SeedArtifact(t, conv.ID, "parent")
	parent := "organizations/" + owned.Slug + "/users/" + owned.Owner.IdentityID.String() +
		"/conversations/" + conv.Name + "/artifacts/" + art.Name

	const pageSize = 3
	total := len(createTimeScramble)
	base := time.Now().Add(-time.Hour)
	seededNames := make([]string, total) // in id (seed) order
	for i := range total {
		v := h.SeedArtifactVersion(t, art.ID)
		seededNames[i] = parent + "/versions/" + v.Name
		setCreateTime(t, h, "ai_artifact_versions", v.ID, base.Add(time.Duration(createTimeScramble[i])*time.Minute))
	}

	got := drainVersionNames(t, ctx, client, &aiv1.ListArtifactVersionsRequest{Parent: parent, OrderBy: "createTime", PageSize: pageSize})
	assert.Equal(t, expectedByScramble(seededNames), got,
		"compound cursor returns every version once, in create_time order, across boundaries")
	assertUnique(t, got, total)
}

// TestE2E_ListArtifactVersions_DefaultOrderNewestFirst is the DefaultOrder guard
// for artifact versions: no order_by → newest version first (reverse of seed
// order).
func TestE2E_ListArtifactVersions_DefaultOrderNewestFirst(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithAiChatServer())
	owned := h.SeedOwnedOrg(t, "ver-def", "Ver Def", "aichat")
	ctx := context.Background()
	client := aiv1.NewAiChatClient(h.Conn())
	conv := h.SeedConversation(t, owned.Row.ID, owned.Owner, "parent")
	art := h.SeedArtifact(t, conv.ID, "parent")
	parent := "organizations/" + owned.Slug + "/users/" + owned.Owner.IdentityID.String() +
		"/conversations/" + conv.Name + "/artifacts/" + art.Name

	const pageSize = 3
	const total = 7
	seededNames := make([]string, total)
	for i := range total {
		v := h.SeedArtifactVersion(t, art.ID)
		seededNames[i] = parent + "/versions/" + v.Name
	}

	got := drainVersionNames(t, ctx, client, &aiv1.ListArtifactVersionsRequest{Parent: parent, PageSize: pageSize})
	assert.Equal(t, reversed(seededNames), got, "no order_by defaults to newest-version-first (id desc)")
	assertUnique(t, got, total)
}

// drainVersionNames follows next_page_token to completion for ListArtifactVersions.
func drainVersionNames(t *testing.T, ctx context.Context, client aiv1.AiChatClient, req *aiv1.ListArtifactVersionsRequest) []string {
	t.Helper()
	var names []string
	token := ""
	for range 100 {
		req.PageToken = token
		resp, err := client.ListArtifactVersions(ctx, req)
		require.NoError(t, err)
		for _, v := range resp.GetVersions() {
			names = append(names, v.GetName())
		}
		if token = resp.GetNextPageToken(); token == "" {
			return names
		}
	}
	t.Fatal("pagination did not terminate within 100 pages")
	return nil
}

// TestE2E_ListConversations_ScopeIsolationByCreatedBy pins the security-relevant
// half of the ListConversations base scope: the `created_by = $pathUser`
// predicate. Two members of the same org each own conversations; listing under
// one user's parent must return only that user's rows, never the peer's — even
// though both live under the same org_id. This guards against a regression that
// dropped the created_by predicate (which would leak peers' conversations to any
// org member).
func TestE2E_ListConversations_ScopeIsolationByCreatedBy(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithAiChatServer())
	owned := h.SeedOwnedOrg(t, "cv-iso", "Cv Iso", "aichat")
	ctx := context.Background()
	client := aiv1.NewAiChatClient(h.Conn())

	// A second org member (peer) with their own conversations.
	peer := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "cv-iso-peer"})
	h.SeedMembership(t, owned.Row.ID, peer, grpcharness.RoleEditor)

	h.SeedConversation(t, owned.Row.ID, owned.Owner, "owner-a")
	h.SeedConversation(t, owned.Row.ID, owned.Owner, "owner-b")
	h.SeedConversation(t, owned.Row.ID, peer, "peer-a")

	// Caller is the owner (SeedOwnedOrg set it); list under the owner's parent.
	ownerParent := "organizations/" + owned.Slug + "/users/" + owned.Owner.IdentityID.String()
	got := drainConvTitles(t, ctx, client, &aiv1.ListConversationsRequest{Parent: ownerParent, OrderBy: "title"})
	assert.Equal(t, []string{"owner-a", "owner-b"}, got,
		"owner sees only their own conversations, never the peer's")

	// The peer, listing under their own parent, sees only their conversation.
	h.SetCaller(peer)
	peerParent := "organizations/" + owned.Slug + "/users/" + peer.IdentityID.String()
	gotPeer := drainConvTitles(t, ctx, client, &aiv1.ListConversationsRequest{Parent: peerParent, OrderBy: "title"})
	assert.Equal(t, []string{"peer-a"}, gotPeer, "peer sees only their own conversation")
}

// TestE2E_ListConversations_RejectsBadOrderByAndPageToken pins the InvalidArgument
// surface added by the migration: an unknown order_by field is rejected by
// PlanOrderBy, and a tampered page_token is rejected by DecodeCursor. Both map to
// codes.InvalidArgument (not Internal, not a silent empty page).
func TestE2E_ListConversations_RejectsBadOrderByAndPageToken(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithAiChatServer())
	owned := h.SeedOwnedOrg(t, "cv-rej", "Cv Rej", "aichat")
	ctx := context.Background()
	client := aiv1.NewAiChatClient(h.Conn())
	parent := "organizations/" + owned.Slug + "/users/" + owned.Owner.IdentityID.String()

	_, err := client.ListConversations(ctx, &aiv1.ListConversationsRequest{Parent: parent, OrderBy: "bogusField"})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err), "unknown order_by field → InvalidArgument")

	// lastMessageTime is filterable-only now (nullable column, demoted from
	// Sortable) — sorting on it must be rejected, not silently broken.
	_, err = client.ListConversations(ctx, &aiv1.ListConversationsRequest{Parent: parent, OrderBy: "lastMessageTime"})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err), "demoted nullable sortable → InvalidArgument")

	_, err = client.ListConversations(ctx, &aiv1.ListConversationsRequest{Parent: parent, PageToken: "not-a-real-token"})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err), "tampered page_token → InvalidArgument")
}
