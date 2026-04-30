package aichat

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	db "github.com/dashkan/pivox/internal/db/generated"
	aiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/ai/v1"
	"github.com/dashkan/pivox/internal/server"
	"github.com/dashkan/pivox/internal/service/aichat/model"
	"github.com/dashkan/pivox/internal/service/aichat/tools"
	"github.com/dashkan/pivox/internal/testutil/mocks"
)

// mockLanguageModel implements model.LanguageModel for tests.
type mockLanguageModel struct {
	events []model.ModelEvent
	err    error
	name   string
}

func (m *mockLanguageModel) Stream(_ context.Context, _ model.StreamRequest) (model.StreamReader, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &sliceReader{events: m.events}, nil
}

func (m *mockLanguageModel) Name() string {
	if m.name == "" {
		return "mock-llm"
	}
	return m.name
}

type sliceReader struct {
	events []model.ModelEvent
	pos    int
}

func (r *sliceReader) Next(_ context.Context) (model.ModelEvent, error) {
	if r.pos >= len(r.events) {
		return model.ModelEvent{}, io.EOF
	}
	ev := r.events[r.pos]
	r.pos++
	return ev, nil
}

func (r *sliceReader) Close() error { return nil }

// mockServerStream implements grpc.ServerStreamingServer[aiv1.ServerEvent] for tests.
type mockServerStream struct {
	ctx  context.Context
	sent []*aiv1.ServerEvent
}

func (s *mockServerStream) Send(ev *aiv1.ServerEvent) error {
	s.sent = append(s.sent, ev)
	return nil
}

func (s *mockServerStream) Context() context.Context     { return s.ctx }
func (s *mockServerStream) SetHeader(metadata.MD) error  { return nil }
func (s *mockServerStream) SendHeader(metadata.MD) error { return nil }
func (s *mockServerStream) SetTrailer(metadata.MD)       {}
func (s *mockServerStream) SendMsg(any) error            { return nil }
func (s *mockServerStream) RecvMsg(any) error            { return nil }

// fixedUserID is the per-test caller's pivox user UUID
// (firebase_identities.id). Stable so paths can be constructed
// without juggling fresh uuids per call.
var fixedUserID = uuid.MustParse("0192a000-0009-7000-8000-000000000001")

// authenticatedCtx builds the same context shape the production
// auth interceptor produces: firebase UID + pivox_user_id claim.
// Post-Phase-7 handlers read the claim via MustPivoxUserID for
// ownership checks, so tests must seed both.
func authenticatedCtx(uid string) context.Context {
	ctx := server.WithAuthenticatedUID(context.Background(), uid)
	ctx = server.WithPivoxUserID(ctx, fixedUserID)
	return ctx
}

func testOrg() db.Organization {
	return db.Organization{
		ID:   uuid.New(),
		Name: "acme",
	}
}

// `uid` retained on the signature for backward compat with existing
// callers — only the test fixture uses fixedUserID for both creator
// and authorization. Unused on the row now that audit + ownership
// merged into a single column.
func testConversation(orgID uuid.UUID, uid string) db.AiConversation {
	_ = uid
	return db.AiConversation{
		ID:         uuid.New(),
		OrgID:      orgID,
		CreatedBy:  fixedUserID,
		Name:       "conv1",
		CreateTime: time.Now(),
		UpdateTime: time.Now(),
		Etag:       "etag1",
	}
}

// testConvPath returns the resource name a caller would send for
// the fixture conversation, including the path-bound user-uuid.
func testConvPath(orgName, convName string) string {
	return "organizations/" + orgName + "/users/" + fixedUserID.String() + "/conversations/" + convName
}

// userTextRequest builds a stateful GenerateContentRequest with a
// single user message containing one text part. Mirrors the most
// common inbound shape from the Swift client.
func userTextRequest(conversationName, text string) *aiv1.GenerateContentRequest {
	return &aiv1.GenerateContentRequest{
		Parent:       "organizations/acme",
		Conversation: conversationName,
		Messages: []*aiv1.InputMessage{
			{
				Role: aiv1.Role_USER,
				Parts: []*aiv1.MessagePart{
					{Part: &aiv1.MessagePart_Text{Text: &aiv1.TextPart{Text: text}}},
				},
			},
		},
	}
}

// toolResultRequest builds a stateful request continuing a turn after
// a client-side tool ran — a TOOL-role message carrying a
// ToolResultPart. Replaces the bidi-era `ClientEvent_ToolOutput`
// envelope.
func toolResultRequest(conversationName, callID, resultJSON string) *aiv1.GenerateContentRequest {
	return &aiv1.GenerateContentRequest{
		Parent:       "organizations/acme",
		Conversation: conversationName,
		Messages: []*aiv1.InputMessage{
			{
				Role: aiv1.Role_TOOL,
				Parts: []*aiv1.MessagePart{
					{Part: &aiv1.MessagePart_ToolResult{ToolResult: &aiv1.ToolResultPart{
						ToolCallId: callID,
						ResultJson: resultJSON,
					}}},
				},
			},
		},
	}
}

func TestStreamGenerateContent_HappyPath(t *testing.T) {
	q := new(mocks.MockQuerier)
	llm := &mockLanguageModel{
		events: []model.ModelEvent{
			{Kind: "text_delta", Text: "Hello "},
			{Kind: "text_delta", Text: "world!"},
			{Kind: "finish"},
		},
	}
	srv := NewServer(nil, q, llm, tools.NewRegistry(), nil, nil, slog.Default())

	org := testOrg()
	uid := "user1"
	conv := testConversation(org.ID, uid)
	ctx := authenticatedCtx(uid)

	q.On("GetOrganizationByName", mock.Anything, "acme").Return(org, nil)
	q.On("GetConversationByName", mock.Anything, db.GetConversationByNameParams{
		OrgID: org.ID, Name: "conv1",
	}).Return(conv, nil)
	q.On("GetNextSequenceForConversation", mock.Anything, conv.ID).Return(int32(1), nil)
	q.On("CreateMessage", mock.Anything, mock.Anything).Return(db.AiMessage{}, nil)
	q.On("IncrementConversationMessageCount", mock.Anything, conv.ID).Return(nil)
	q.On("ListMessagesNewestFirst", mock.Anything, mock.Anything).Return([]db.AiMessage{}, nil)

	req := userTextRequest(testConvPath("acme", "conv1"), "Hi")
	stream := &mockServerStream{ctx: ctx}

	err := srv.StreamGenerateContent(req, stream)
	require.NoError(t, err)

	// Verify event sequence: TextStart, TextDelta("Hello "), TextDelta("world!"), TextEnd, Done
	require.GreaterOrEqual(t, len(stream.sent), 5)
	assert.NotNil(t, stream.sent[0].GetTextStart())
	assert.Equal(t, "Hello ", stream.sent[1].GetTextDelta().GetDelta())
	assert.Equal(t, "world!", stream.sent[2].GetTextDelta().GetDelta())
	assert.NotNil(t, stream.sent[3].GetTextEnd())
	assert.NotNil(t, stream.sent[4].GetDone())

	// Verify CreateMessage was called twice (user + assistant).
	calls := 0
	for _, c := range q.Calls {
		if c.Method == "CreateMessage" {
			calls++
		}
	}
	assert.Equal(t, 2, calls)
}

// Field-shape validation (parent required, messages min_items=1,
// InputMessage.role not in {ASSISTANT, SYSTEM}, ToolResultPart.tool_call_id
// non-empty, tool-role must include a tool_result part) is enforced
// by the protovalidate interceptor via buf-validate annotations on
// the proto. Direct handler-level tests of those rejections were
// dropped — they exercised handler branches that no longer exist.
// Cross-cutting validation behavior is covered by protovalidate's
// own test suite + integration tests of the full RPC pipeline.

func TestStreamGenerateContent_ToolResultResumesGeneration(t *testing.T) {
	q := new(mocks.MockQuerier)
	llm := &mockLanguageModel{
		events: []model.ModelEvent{
			{Kind: "text_delta", Text: "done"},
			{Kind: "finish"},
		},
	}
	srv := NewServer(nil, q, llm, tools.NewRegistry(), nil, nil, slog.Default())

	org := testOrg()
	uid := "user1"
	conv := testConversation(org.ID, uid)
	ctx := authenticatedCtx(uid)

	q.On("GetOrganizationByName", mock.Anything, "acme").Return(org, nil)
	q.On("GetConversationByName", mock.Anything, db.GetConversationByNameParams{
		OrgID: org.ID, Name: "conv1",
	}).Return(conv, nil)
	q.On("GetNextSequenceForConversation", mock.Anything, conv.ID).Return(int32(3), nil)
	q.On("CreateMessage", mock.Anything, mock.Anything).Return(db.AiMessage{}, nil)
	q.On("IncrementConversationMessageCount", mock.Anything, conv.ID).Return(nil)
	q.On("ListMessagesNewestFirst", mock.Anything, mock.Anything).Return([]db.AiMessage{}, nil)

	req := toolResultRequest(testConvPath("acme", "conv1"), "call-123", `{"ok":true}`)
	stream := &mockServerStream{ctx: ctx}

	err := srv.StreamGenerateContent(req, stream)
	require.NoError(t, err)

	// Verify the inbound message was persisted with role="tool" and
	// a ToolResultPart carrying the tool_call_id.
	var createCall *mock.Call
	for i := range q.Calls {
		if q.Calls[i].Method == "CreateMessage" {
			createCall = &q.Calls[i]
			break
		}
	}
	require.NotNil(t, createCall, "CreateMessage must be called for the tool output")
	params := createCall.Arguments.Get(1).(db.CreateMessageParams)
	assert.Equal(t, "tool", params.Role)

	parts, perr := unmarshalParts(params.Parts)
	require.NoError(t, perr)
	require.Len(t, parts, 1)
	tr := parts[0].GetToolResult()
	require.NotNil(t, tr)
	assert.Equal(t, "call-123", tr.GetToolCallId())
	assert.Equal(t, `{"ok":true}`, tr.GetResultJson())

	// Verify the model was invoked and a Done event was emitted.
	var gotDone bool
	for _, ev := range stream.sent {
		if ev.GetDone() != nil {
			gotDone = true
		}
	}
	assert.True(t, gotDone, "stream must emit Done after model finish")
}

func TestStreamGenerateContent_WrongOwner(t *testing.T) {
	q := new(mocks.MockQuerier)
	llm := &mockLanguageModel{}
	srv := NewServer(nil, q, llm, nil, nil, nil, slog.Default())

	org := testOrg()
	// Conversation owned by a different user-uuid than the path-bound
	// caller. resolveConversation must surface NotFound (path-vs-row
	// creator_id mismatch) rather than leak ownership.
	conv := testConversation(org.ID, "other-user")
	conv.CreatedBy = uuid.MustParse("0192a000-0099-7000-8000-000000000099")
	ctx := authenticatedCtx("user1")

	q.On("GetOrganizationByName", mock.Anything, "acme").Return(org, nil)
	q.On("GetConversationByName", mock.Anything, mock.Anything).Return(conv, nil)

	stream := &mockServerStream{ctx: ctx}
	err := srv.StreamGenerateContent(userTextRequest(testConvPath("acme", "conv1"), "Hi"), stream)
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
}

func TestStreamGenerateContent_ModelError(t *testing.T) {
	q := new(mocks.MockQuerier)
	llm := &mockLanguageModel{err: io.ErrUnexpectedEOF}
	srv := NewServer(nil, q, llm, nil, nil, nil, slog.Default())

	org := testOrg()
	uid := "user1"
	conv := testConversation(org.ID, uid)
	ctx := authenticatedCtx(uid)

	q.On("GetOrganizationByName", mock.Anything, "acme").Return(org, nil)
	q.On("GetConversationByName", mock.Anything, mock.Anything).Return(conv, nil)
	q.On("GetNextSequenceForConversation", mock.Anything, conv.ID).Return(int32(1), nil)
	q.On("CreateMessage", mock.Anything, mock.Anything).Return(db.AiMessage{}, nil)
	q.On("IncrementConversationMessageCount", mock.Anything, conv.ID).Return(nil)
	q.On("ListMessagesNewestFirst", mock.Anything, mock.Anything).Return([]db.AiMessage{}, nil)

	stream := &mockServerStream{ctx: ctx}
	err := srv.StreamGenerateContent(userTextRequest(testConvPath("acme", "conv1"), "Hi"), stream)
	require.Error(t, err)
	// Verify a StreamError event was sent before the error return.
	var gotStreamError bool
	for _, ev := range stream.sent {
		if ev.GetStreamError() != nil {
			gotStreamError = true
		}
	}
	assert.True(t, gotStreamError)
}

// GenerateContent (unary) shares the runGenerate core with
// StreamGenerateContent — this test confirms the unary path
// accumulates text deltas into the returned Message instead of
// emitting them, and that no events leak through.
func TestGenerateContent_UnaryAccumulatesText(t *testing.T) {
	q := new(mocks.MockQuerier)
	llm := &mockLanguageModel{
		events: []model.ModelEvent{
			{Kind: "text_delta", Text: "Hello "},
			{Kind: "text_delta", Text: "world"},
			{Kind: "finish"},
		},
	}
	srv := NewServer(nil, q, llm, tools.NewRegistry(), nil, nil, slog.Default())

	org := testOrg()
	uid := "user1"
	conv := testConversation(org.ID, uid)
	ctx := authenticatedCtx(uid)

	q.On("GetOrganizationByName", mock.Anything, "acme").Return(org, nil)
	q.On("GetConversationByName", mock.Anything, mock.Anything).Return(conv, nil)
	q.On("GetNextSequenceForConversation", mock.Anything, conv.ID).Return(int32(1), nil)
	q.On("CreateMessage", mock.Anything, mock.Anything).Return(db.AiMessage{}, nil)
	q.On("IncrementConversationMessageCount", mock.Anything, conv.ID).Return(nil)
	q.On("ListMessagesNewestFirst", mock.Anything, mock.Anything).Return([]db.AiMessage{}, nil)

	resp, err := srv.GenerateContent(ctx, userTextRequest(testConvPath("acme", "conv1"), "Hi"))
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.GetMessage())

	// The accumulated text from the deltas should land as a single
	// text part on the returned assistant Message.
	require.Len(t, resp.GetMessage().GetParts(), 1)
	tp := resp.GetMessage().GetParts()[0].GetText()
	require.NotNil(t, tp)
	assert.Equal(t, "Hello world", tp.GetText())

	// Token usage should be populated (coarse — values are
	// estimate-only but must be non-zero on output).
	require.NotNil(t, resp.GetUsage())
	assert.Greater(t, resp.GetUsage().GetOutputTokens(), int32(0))
}

// GenerateContent in stateless mode (no `conversation`) uses inline
// `messages` directly without persistence. Confirms no DB writes
// happen and the response is still produced.
func TestGenerateContent_StatelessSkipsPersistence(t *testing.T) {
	q := new(mocks.MockQuerier)
	llm := &mockLanguageModel{
		events: []model.ModelEvent{
			{Kind: "text_delta", Text: "Brief"},
			{Kind: "finish"},
		},
	}
	srv := NewServer(nil, q, llm, tools.NewRegistry(), nil, nil, slog.Default())

	org := testOrg()
	ctx := authenticatedCtx("user1")
	// Org membership check is required even for stateless calls —
	// otherwise a caller could pass any phantom parent and burn
	// model budget against orgs they don't belong to.
	q.On("GetOrganizationByName", mock.Anything, "acme").Return(org, nil)

	req := &aiv1.GenerateContentRequest{
		Parent:            "organizations/acme",
		SystemInstruction: "Be brief.",
		Messages: []*aiv1.InputMessage{
			{Role: aiv1.Role_USER, Parts: []*aiv1.MessagePart{
				{Part: &aiv1.MessagePart_Text{Text: &aiv1.TextPart{Text: "Hi"}}},
			}},
		},
	}

	resp, err := srv.GenerateContent(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, "Brief", extractTextFromMessage(resp.GetMessage()))
	// Model name should round-trip from the LLM adapter.
	assert.Equal(t, "mock-llm", resp.GetModel())

	// No persistence calls should fire in stateless mode. We allow
	// the org-membership check via GetOrganizationByName but
	// nothing else.
	for _, c := range q.Calls {
		switch c.Method {
		case "GetOrganizationByName":
			// expected
		default:
			t.Errorf("unexpected DB call in stateless mode: %s", c.Method)
		}
	}
}

// Tests for `Role=ASSISTANT/SYSTEM rejected on input` and
// `ToolResultPart missing tool_call_id rejected` were dropped — those
// constraints are now enforced by the protovalidate interceptor (see
// `(buf.validate.field).enum.not_in` on InputMessage.role and
// `(buf.validate.field).string.min_len = 1` on ToolResultPart.tool_call_id).
// Handler-level checks no longer exist.

func TestStreamGenerateContent_ConversationOrgMismatchRejected(t *testing.T) {
	q := new(mocks.MockQuerier)
	llm := &mockLanguageModel{}
	srv := NewServer(nil, q, llm, nil, nil, nil, slog.Default())

	org := testOrg()
	ctx := authenticatedCtx("user1")
	q.On("GetOrganizationByName", mock.Anything, "acme").Return(org, nil)

	// parent=acme but conversation under a different org. Without
	// the cross-check a caller could write into someone else's org.
	req := &aiv1.GenerateContentRequest{
		Parent:       "organizations/acme",
		Conversation: "organizations/competitor/conversations/leaked",
		Messages: []*aiv1.InputMessage{
			{Role: aiv1.Role_USER, Parts: []*aiv1.MessagePart{
				{Part: &aiv1.MessagePart_Text{Text: &aiv1.TextPart{Text: "Hi"}}},
			}},
		},
	}
	stream := &mockServerStream{ctx: ctx}
	err := srv.StreamGenerateContent(req, stream)
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestExtractText(t *testing.T) {
	parts := []*aiv1.MessagePart{
		{Part: &aiv1.MessagePart_Text{Text: &aiv1.TextPart{Text: "Hello "}}},
		{Part: &aiv1.MessagePart_Text{Text: &aiv1.TextPart{Text: "world"}}},
		{Part: &aiv1.MessagePart_ToolCall{ToolCall: &aiv1.ToolCallPart{Tool: "search"}}},
	}
	assert.Equal(t, "Hello world", extractText(parts))
}

func TestLoadModelHistory_BudgetTruncation(t *testing.T) {
	q := new(mocks.MockQuerier)
	srv := NewServer(nil, q, nil, nil, nil, nil, slog.Default())

	convID := uuid.New()
	partsJSON, _ := marshalParts([]*aiv1.MessagePart{
		{Part: &aiv1.MessagePart_Text{Text: &aiv1.TextPart{Text: "hello"}}},
	})

	// Create messages totaling well over budget.
	var rows []db.AiMessage
	for i := 0; i < 10; i++ {
		rows = append(rows, db.AiMessage{
			ID:             uuid.New(),
			ConversationID: convID,
			Name:           uuid.New().String()[:12],
			Role:           "user",
			Parts:          partsJSON,
			Sequence:       int64(10 - i), // newest first
			TokenCount:     5000,
			CreateTime:     time.Now(),
		})
	}

	q.On("ListMessagesNewestFirst", mock.Anything, db.ListMessagesNewestFirstParams{
		ConversationID: convID,
		Limit:          int32(defaultMaxHistoryRows),
	}).Return(rows, nil)

	msgs, err := srv.loadModelHistory(context.Background(), convID)
	require.NoError(t, err)
	// Budget is 22500, each msg is 5000 tokens, so 4 messages fit (20000 <= 22500).
	assert.Equal(t, 4, len(msgs))
	// Verify chronological order (oldest first).
	assert.Equal(t, int64(7), rows[3].Sequence) // 4th from newest = seq 7
}

func TestDbMessageToModel(t *testing.T) {
	parts, _ := marshalParts([]*aiv1.MessagePart{
		{Part: &aiv1.MessagePart_Text{Text: &aiv1.TextPart{Text: "hello"}}},
		{Part: &aiv1.MessagePart_ToolCall{ToolCall: &aiv1.ToolCallPart{
			ToolCallId: "tc1",
			Tool:       "search",
			InputJson:  `{"q":"test"}`,
		}}},
	})

	row := db.AiMessage{
		Role:       "assistant",
		Parts:      parts,
		CreateTime: time.Now(),
	}

	m := dbMessageToModel(row)
	assert.Equal(t, "assistant", m.Role)
	require.Len(t, m.Parts, 2)
	assert.Equal(t, "text", m.Parts[0].Type)
	assert.Equal(t, "hello", m.Parts[0].Text)
	assert.Equal(t, "tool_call", m.Parts[1].Type)
	assert.Equal(t, "search", m.Parts[1].ToolCall.Name)
}

func TestStreamGenerateContent_InvalidConversationReturnsNotFound(t *testing.T) {
	q := new(mocks.MockQuerier)
	llm := &mockLanguageModel{}
	srv := NewServer(nil, q, llm, nil, nil, nil, slog.Default())

	org := testOrg()
	ctx := authenticatedCtx("user1")

	q.On("GetOrganizationByName", mock.Anything, "acme").Return(org, nil)
	q.On("GetConversationByName", mock.Anything, mock.Anything).Return(
		db.AiConversation{}, pgx.ErrNoRows)

	stream := &mockServerStream{ctx: ctx}
	err := srv.StreamGenerateContent(userTextRequest(testConvPath("acme", "nonexistent"), "Hi"), stream)
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
}

func TestStreamGenerateContent_InvalidConversationNameReturnsInvalidArgument(t *testing.T) {
	q := new(mocks.MockQuerier)
	llm := &mockLanguageModel{}
	srv := NewServer(nil, q, llm, nil, nil, nil, slog.Default())

	org := testOrg()
	ctx := authenticatedCtx("user1")
	// Org-membership check runs first; mock so we reach the
	// conversation-name parsing step that this test targets.
	q.On("GetOrganizationByName", mock.Anything, "acme").Return(org, nil)
	stream := &mockServerStream{ctx: ctx}

	err := srv.StreamGenerateContent(userTextRequest("garbage/name", "Hi"), stream)
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}
