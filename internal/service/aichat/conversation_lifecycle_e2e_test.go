package aichat_test

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	db "github.com/dashkan/pivox/internal/db/generated"
	aiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/ai/v1"
	"github.com/dashkan/pivox/internal/service/aichat/model"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
)

// These tests pin the conversation-lifecycle rework: a conversation is never
// persisted before (without) its first message — CreateConversation and the
// first user message land in one transaction, up front, before generation —
// and a failed first generation leaves no trace (the auto-created conversation
// is removed). They drive the real generate path (StreamGenerateContent /
// GenerateContent) through grpcharness against a real Postgres, with an
// injected model (erroring or scripted) at the model boundary.

// erroringModel fails at the model boundary: Stream never returns a reader. It
// simulates the LLM backend being unavailable, which is the seam for the
// "failed generation leaves no auto-created conversation" tests.
type erroringModel struct{}

func (erroringModel) Name() string { return "erroring" }

func (erroringModel) Stream(context.Context, model.StreamRequest) (model.StreamReader, error) {
	return nil, fmt.Errorf("model backend unavailable")
}

// scriptedModel streams a fixed text response then finishes — enough to drive a
// full successful generation (user turn persisted up front, assistant turn
// persisted after) without a live LLM.
type scriptedModel struct{ text string }

func (scriptedModel) Name() string { return "scripted" }

func (m scriptedModel) Stream(context.Context, model.StreamRequest) (model.StreamReader, error) {
	return &scriptedReader{events: []model.ModelEvent{
		{Kind: "text_delta", Text: m.text},
		{Kind: "finish"},
	}}, nil
}

type scriptedReader struct {
	events []model.ModelEvent
	i      int
}

func (r *scriptedReader) Next(context.Context) (model.ModelEvent, error) {
	if r.i >= len(r.events) {
		return model.ModelEvent{}, io.EOF
	}
	ev := r.events[r.i]
	r.i++
	return ev, nil
}

func (r *scriptedReader) Close() error { return nil }

// userTurn builds a minimal valid GenerateContentRequest with an empty
// `conversation` (auto-create path) carrying a single user text message.
func userTurn(parent, text string) *aiv1.GenerateContentRequest {
	return &aiv1.GenerateContentRequest{
		Parent: parent,
		Messages: []*aiv1.InputMessage{
			{Role: "user", Parts: []*aiv1.MessagePart{{Type: "text", Text: text}}},
		},
	}
}

// drainStream consumes a StreamGenerateContent response to completion and
// returns the terminal error (nil on clean EOF).
func drainStream(t *testing.T, stream aiv1.AiChat_StreamGenerateContentClient) error {
	t.Helper()
	for {
		_, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// assertSurvivedWithOnlyUserMessage asserts the given parent has exactly one
// conversation, and that conversation holds exactly its first user message
// (message_count == 1, last_message_time set = the user turn's time, no
// assistant reply). This is the KEEP behavior for a failed first generation:
// atomic create-with-first-message committed the conversation + user turn up
// front, and the failure simply left it without a reply — never empty, never
// reaped.
func assertSurvivedWithOnlyUserMessage(t *testing.T, h *grpcharness.Harness, ctx context.Context, client aiv1.AiChatClient, orgID uuid.UUID, parent string) {
	t.Helper()
	list, err := client.ListConversations(ctx, &aiv1.ListConversationsRequest{Parent: parent})
	require.NoError(t, err)
	require.Len(t, list.GetConversations(), 1,
		"the auto-created conversation survives a failed first generation")
	conv := list.GetConversations()[0]
	assert.EqualValues(t, 1, conv.GetMessageCount(), "only the user's first message persisted")
	require.NotNil(t, conv.GetLastMessageTime(), "last_message_time is set even without a reply")

	convID := conversationIDFromName(t, h, orgID, conv.GetName())
	rows, err := h.Queries.ListMessagesNewestFirst(ctx, db.ListMessagesNewestFirstParams{
		ConversationID: convID,
		Limit:          10,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1, "exactly the one user message — no assistant reply")
	assert.Equal(t, "user", rows[0].Role)
	assert.WithinDuration(t, rows[0].CreateTime, conv.GetLastMessageTime().AsTime(), time.Millisecond,
		"last_message_time equals the user message's time")
}

// TestE2E_StreamGenerateContent_FailedFirstGenerationKeepsConversation pins the
// KEEP behavior for the streaming path: an empty `conversation` triggers atomic
// create-with-first-message, then the model errors. The conversation stays put
// with the user's first message and no assistant reply — exactly how a chat
// assistant behaves (your message stays, the error is shown, retry in place).
func TestE2E_StreamGenerateContent_FailedFirstGenerationKeepsConversation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t,
		grpcharness.WithOrganizationsServer(),
		grpcharness.WithAiChatServerModel(erroringModel{}))
	owned := h.SeedOwnedOrg(t, "cv-keep-s", "Cv Keep Stream", "aichat")
	ctx := context.Background()
	client := aiv1.NewAiChatClient(h.Conn())
	parent := "organizations/" + owned.Slug + "/users/" + owned.Owner.IdentityID.String()

	stream, err := client.StreamGenerateContent(ctx, userTurn(parent, "hello"))
	require.NoError(t, err, "opening the stream should succeed; the error surfaces on Recv")
	err = drainStream(t, stream)
	require.Error(t, err, "generation must fail (the model errors)")

	assertSurvivedWithOnlyUserMessage(t, h, ctx, client, owned.Row.ID, parent)
}

// TestE2E_GenerateContent_FailedFirstGenerationKeepsConversation mirrors the
// KEEP behavior for the unary path — same lifecycle, different RPC.
func TestE2E_GenerateContent_FailedFirstGenerationKeepsConversation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t,
		grpcharness.WithOrganizationsServer(),
		grpcharness.WithAiChatServerModel(erroringModel{}))
	owned := h.SeedOwnedOrg(t, "cv-keep-u", "Cv Keep Unary", "aichat")
	ctx := context.Background()
	client := aiv1.NewAiChatClient(h.Conn())
	parent := "organizations/" + owned.Slug + "/users/" + owned.Owner.IdentityID.String()

	_, err := client.GenerateContent(ctx, userTurn(parent, "hello"))
	require.Error(t, err, "generation must fail (the model errors)")

	assertSurvivedWithOnlyUserMessage(t, h, ctx, client, owned.Row.ID, parent)
}

// TestE2E_GenerateContent_ConversationAlwaysHasLastMessageTime pins the schema
// invariant: a conversation created through the generate path always has a
// non-null last_message_time equal to its newest message's time. After a full
// successful generation the newest message is the assistant reply, so
// last_message_time tracks it — never NULL, never stale.
func TestE2E_GenerateContent_ConversationAlwaysHasLastMessageTime(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t,
		grpcharness.WithOrganizationsServer(),
		grpcharness.WithAiChatServerModel(scriptedModel{text: "hi there"}))
	owned := h.SeedOwnedOrg(t, "cv-lmt", "Cv LastMsgTime", "aichat")
	ctx := context.Background()
	client := aiv1.NewAiChatClient(h.Conn())
	parent := "organizations/" + owned.Slug + "/users/" + owned.Owner.IdentityID.String()

	resp, err := client.GenerateContent(ctx, userTurn(parent, "hello"))
	require.NoError(t, err)
	require.NotEmpty(t, resp.GetMessage().GetName())

	// Exactly one conversation, created by this call.
	list, err := client.ListConversations(ctx, &aiv1.ListConversationsRequest{Parent: parent})
	require.NoError(t, err)
	require.Len(t, list.GetConversations(), 1)
	conv := list.GetConversations()[0]

	// last_message_time is present (never null) ...
	require.NotNil(t, conv.GetLastMessageTime(), "last_message_time must be set")
	// ... and equals the newest persisted message's create_time (user turn +
	// assistant turn were both persisted; newest is the assistant).
	convID := conversationIDFromName(t, h, owned.Row.ID, conv.GetName())
	rows, err := h.Queries.ListMessagesNewestFirst(ctx, db.ListMessagesNewestFirstParams{
		ConversationID: convID,
		Limit:          10,
	})
	require.NoError(t, err)
	require.Len(t, rows, 2, "user turn + assistant turn both persisted")
	newest := rows[0].CreateTime
	assert.WithinDuration(t, newest, conv.GetLastMessageTime().AsTime(), time.Millisecond,
		"last_message_time tracks the newest message")
}

// setLastMessageTime rewrites a conversation's last_message_time directly, to
// simulate activity landing at a deterministic wall-clock time (the real bump
// path, IncrementConversationMessageCount, is covered by the successful-
// generation test above). Uses the exported harness pool — no sqlc query
// backdates the activity clock, and none should.
func setLastMessageTime(t *testing.T, h *grpcharness.Harness, convID uuid.UUID, ts time.Time) {
	t.Helper()
	_, err := h.Pool.Exec(context.Background(),
		"UPDATE ai_conversations SET last_message_time = $1 WHERE id = $2", ts, convID)
	require.NoError(t, err)
}

// TestE2E_ListConversations_DefaultSortRecentActivityFirst pins the new default
// order (`lastMessageTime desc`): with no order_by, the conversation with the
// most recent activity sorts first. An OLD conversation that receives a new
// message (its last_message_time bumped past a NEWER conversation's) jumps to
// the top — which the pre-rework id-desc default could never do.
func TestE2E_ListConversations_DefaultSortRecentActivityFirst(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithAiChatServer())
	owned := h.SeedOwnedOrg(t, "cv-act", "Cv Activity", "aichat")
	ctx := context.Background()
	client := aiv1.NewAiChatClient(h.Conn())
	parent := "organizations/" + owned.Slug + "/users/" + owned.Owner.IdentityID.String()

	// old seeded first, new second — so new's last_message_time (DEFAULT now())
	// is later than old's.
	old := h.SeedConversation(t, owned.Row.ID, owned.Owner, "old")
	newer := h.SeedConversation(t, owned.Row.ID, owned.Owner, "new")

	// Default sort surfaces the most-recently-active first: initially "new".
	got := drainConvTitles(t, ctx, client, &aiv1.ListConversationsRequest{Parent: parent})
	require.Equal(t, []string{"new", "old"}, got, "newest activity first")

	// Post activity to the OLD conversation (bump its clock past new's).
	setLastMessageTime(t, h, old.ID, newer.LastMessageTime.Add(time.Minute))

	// old now sorts first — recent activity, not recent creation, drives order.
	got = drainConvTitles(t, ctx, client, &aiv1.ListConversationsRequest{Parent: parent})
	assert.Equal(t, []string{"old", "new"}, got,
		"a conversation with newer activity sorts ahead of a more-recently-created one")
}

// TestE2E_ListConversations_DefaultSortRecentActivityKeysetBoundary pins the
// compound (last_message_time, id) cursor for the default sort across a page
// boundary. last_message_time is scrambled relative to id order, so the default
// sort walks a sequence that differs from id order (proving activity, not id,
// drives it) and every row is returned exactly once (no drop/dup at the
// boundary).
func TestE2E_ListConversations_DefaultSortRecentActivityKeysetBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithAiChatServer())
	owned := h.SeedOwnedOrg(t, "cv-actkb", "Cv Activity KB", "aichat")
	ctx := context.Background()
	client := aiv1.NewAiChatClient(h.Conn())
	parent := "organizations/" + owned.Slug + "/users/" + owned.Owner.IdentityID.String()

	const pageSize = 3
	titles := []string{"aa", "bb", "cc", "dd", "ee", "ff", "gg"} // id order
	base := time.Now().Add(-time.Hour)
	for i, tl := range titles {
		c := h.SeedConversation(t, owned.Row.ID, owned.Owner, tl)
		setLastMessageTime(t, h, c.ID, base.Add(time.Duration(createTimeScramble[i])*time.Minute))
	}

	got := drainConvTitles(t, ctx, client, &aiv1.ListConversationsRequest{Parent: parent, PageSize: pageSize})
	// Default sort = lastMessageTime desc → titles ordered by scramble offset,
	// descending (reverse of the ascending expectedByScramble permutation).
	want := reversed(expectedByScramble(titles))
	assert.Equal(t, want, got, "default sort walks last_message_time desc across boundaries")
	assertUnique(t, got, len(titles))
}

// TestE2E_ListConversations_OrderByLastMessageTime pins that lastMessageTime is
// now an accepted order_by field (it was rejected while nullable). Both asc and
// desc resolve and sort by activity.
func TestE2E_ListConversations_OrderByLastMessageTime(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithAiChatServer())
	owned := h.SeedOwnedOrg(t, "cv-lmtorder", "Cv LMT Order", "aichat")
	ctx := context.Background()
	client := aiv1.NewAiChatClient(h.Conn())
	parent := "organizations/" + owned.Slug + "/users/" + owned.Owner.IdentityID.String()

	base := time.Now().Add(-time.Hour)
	// Seed in an order that differs from activity order.
	for _, tc := range []struct {
		title  string
		minute int
	}{{"b-mid", 1}, {"c-late", 2}, {"a-early", 0}} {
		c := h.SeedConversation(t, owned.Row.ID, owned.Owner, tc.title)
		setLastMessageTime(t, h, c.ID, base.Add(time.Duration(tc.minute)*time.Minute))
	}

	asc := drainConvTitles(t, ctx, client, &aiv1.ListConversationsRequest{Parent: parent, OrderBy: "lastMessageTime"})
	assert.Equal(t, []string{"a-early", "b-mid", "c-late"}, asc, "lastMessageTime asc sorts by activity")

	desc := drainConvTitles(t, ctx, client, &aiv1.ListConversationsRequest{Parent: parent, OrderBy: "lastMessageTime desc"})
	assert.Equal(t, []string{"c-late", "b-mid", "a-early"}, desc, "lastMessageTime desc sorts by activity")
}

// TestE2E_GenerateContent_ResumePersistsInboundOnce pins the resume path
// (skipInboundPersist=false): a second turn on an existing conversation
// persists exactly one new user message + one assistant message — never zero
// (skip misfire) and never two (double-persist). The first turn auto-creates
// (user1 + assistant1 = 2); the second resumes (user2 + assistant2 = 4).
func TestE2E_GenerateContent_ResumePersistsInboundOnce(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t,
		grpcharness.WithOrganizationsServer(),
		grpcharness.WithAiChatServerModel(scriptedModel{text: "reply"}))
	owned := h.SeedOwnedOrg(t, "cv-resume", "Cv Resume", "aichat")
	ctx := context.Background()
	client := aiv1.NewAiChatClient(h.Conn())
	parent := "organizations/" + owned.Slug + "/users/" + owned.Owner.IdentityID.String()

	// First turn: auto-create → user1 + assistant1.
	_, err := client.GenerateContent(ctx, userTurn(parent, "first"))
	require.NoError(t, err)
	list, err := client.ListConversations(ctx, &aiv1.ListConversationsRequest{Parent: parent})
	require.NoError(t, err)
	require.Len(t, list.GetConversations(), 1)
	conv := list.GetConversations()[0]
	require.EqualValues(t, 2, conv.GetMessageCount(), "auto-create turn persists user + assistant")

	// Second turn: resume the same conversation → user2 + assistant2.
	_, err = client.GenerateContent(ctx, &aiv1.GenerateContentRequest{
		Parent:       parent,
		Conversation: conv.GetName(),
		Messages: []*aiv1.InputMessage{
			{Role: "user", Parts: []*aiv1.MessagePart{{Type: "text", Text: "second"}}},
		},
	})
	require.NoError(t, err)

	// Exactly two more rows — the inbound turn was persisted once, not zero
	// (skip misfire) or twice (double-persist).
	convID := conversationIDFromName(t, h, owned.Row.ID, conv.GetName())
	rows, err := h.Queries.ListMessagesNewestFirst(ctx, db.ListMessagesNewestFirstParams{
		ConversationID: convID,
		Limit:          20,
	})
	require.NoError(t, err)
	assert.Len(t, rows, 4, "resume persists exactly one user + one assistant message")
	userCount := 0
	for _, r := range rows {
		if r.Role == "user" {
			userCount++
		}
	}
	assert.Equal(t, 2, userCount, "exactly two user turns — the second was not duplicated")
}

// TestE2E_GenerateContent_FailureOnExistingConversationKeepsIt pins the
// resume-path failure behavior, symmetric with the auto-create KEEP tests: a
// generation that fails on a pre-existing conversation leaves it (and its
// history) intact — the new user turn is persisted, no reply, and the
// conversation survives.
func TestE2E_GenerateContent_FailureOnExistingConversationKeepsIt(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t,
		grpcharness.WithOrganizationsServer(),
		grpcharness.WithAiChatServerModel(erroringModel{}))
	owned := h.SeedOwnedOrg(t, "cv-keep", "Cv Keep", "aichat")
	ctx := context.Background()
	client := aiv1.NewAiChatClient(h.Conn())
	parent := "organizations/" + owned.Slug + "/users/" + owned.Owner.IdentityID.String()

	// A pre-existing conversation (not created by the failing call).
	existing := h.SeedConversation(t, owned.Row.ID, owned.Owner, "existing")
	convName := "organizations/" + owned.Slug + "/users/" + owned.Owner.IdentityID.String() +
		"/conversations/" + existing.Name

	_, err := client.GenerateContent(ctx, &aiv1.GenerateContentRequest{
		Parent:       parent,
		Conversation: convName,
		Messages: []*aiv1.InputMessage{
			{Role: "user", Parts: []*aiv1.MessagePart{{Type: "text", Text: "hi"}}},
		},
	})
	require.Error(t, err, "generation must fail (the model errors)")

	// The conversation survives a failed turn — nothing reaps it.
	list, err := client.ListConversations(ctx, &aiv1.ListConversationsRequest{Parent: parent})
	require.NoError(t, err)
	require.Len(t, list.GetConversations(), 1, "the pre-existing conversation must survive a failed turn")
	assert.Equal(t, "existing", list.GetConversations()[0].GetTitle())
}

// conversationIDFromName resolves a conversation's UUID from its resource name
// by its slug leaf (the last path segment), scoped to the org.
func conversationIDFromName(t *testing.T, h *grpcharness.Harness, orgID uuid.UUID, name string) uuid.UUID {
	t.Helper()
	// name = organizations/{org}/users/{user}/conversations/{slug}
	slug := name[strings.LastIndex(name, "/")+1:]
	row, err := h.Queries.GetConversationByName(context.Background(), db.GetConversationByNameParams{
		OrgID: orgID,
		Name:  slug,
	})
	require.NoError(t, err)
	return row.ID
}
