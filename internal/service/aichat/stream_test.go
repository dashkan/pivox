package aichat

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
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
}

func (m *mockLanguageModel) Stream(_ context.Context, _ model.StreamRequest) (model.StreamReader, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &sliceReader{events: m.events}, nil
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

// mockStream implements aiv1.AiChat_StreamServer for tests.
type mockStream struct {
	ctx      context.Context
	recvMsgs []*aiv1.ClientEvent
	recvPos  int
	sent     []*aiv1.ServerEvent
}

func (s *mockStream) Send(ev *aiv1.ServerEvent) error {
	s.sent = append(s.sent, ev)
	return nil
}

func (s *mockStream) Recv() (*aiv1.ClientEvent, error) {
	if s.recvPos >= len(s.recvMsgs) {
		return nil, io.EOF
	}
	msg := s.recvMsgs[s.recvPos]
	s.recvPos++
	return msg, nil
}

func (s *mockStream) Context() context.Context     { return s.ctx }
func (s *mockStream) SetHeader(metadata.MD) error  { return nil }
func (s *mockStream) SendHeader(metadata.MD) error { return nil }
func (s *mockStream) SetTrailer(metadata.MD)       {}
func (s *mockStream) SendMsg(any) error            { return nil }
func (s *mockStream) RecvMsg(any) error            { return nil }

func authenticatedCtx(uid string) context.Context {
	return server.WithAuthenticatedUID(context.Background(), uid)
}

func testOrg() db.Organization {
	return db.Organization{
		ID:   uuid.New(),
		Name: "acme",
	}
}

func testConversation(orgID uuid.UUID, uid string) db.Conversation {
	return db.Conversation{
		ID:         uuid.New(),
		OrgID:      orgID,
		CreatorUid: uid,
		Name:       "conv1",
		CreateTime: time.Now(),
		UpdateTime: time.Now(),
		Etag:       "etag1",
	}
}

func TestStream_HappyPath(t *testing.T) {
	q := new(mocks.MockQuerier)
	llm := &mockLanguageModel{
		events: []model.ModelEvent{
			{Kind: "text_delta", Text: "Hello "},
			{Kind: "text_delta", Text: "world!"},
			{Kind: "finish"},
		},
	}
	srv := NewServer(nil, q, llm, tools.NewRegistry(), slog.Default())

	org := testOrg()
	uid := "user1"
	conv := testConversation(org.ID, uid)
	ctx := authenticatedCtx(uid)

	q.On("GetOrganizationByName", mock.Anything, "acme").Return(org, nil)
	q.On("GetConversationByName", mock.Anything, db.GetConversationByNameParams{
		OrgID: org.ID, Name: "conv1",
	}).Return(conv, nil)
	q.On("GetNextSequenceForConversation", mock.Anything, conv.ID).Return(int32(1), nil)
	q.On("CreateMessage", mock.Anything, mock.Anything).Return(db.Message{}, nil)
	q.On("IncrementConversationMessageCount", mock.Anything, conv.ID).Return(nil)
	q.On("ListMessagesNewestFirst", mock.Anything, mock.Anything).Return([]db.Message{}, nil)

	stream := &mockStream{
		ctx: ctx,
		recvMsgs: []*aiv1.ClientEvent{
			{Event: &aiv1.ClientEvent_Message{Message: &aiv1.UserMessage{
				Conversation: "organizations/acme/conversations/conv1",
				Parts: []*aiv1.MessagePart{
					{Part: &aiv1.MessagePart_Text{Text: &aiv1.TextPart{Text: "Hi"}}},
				},
			}}},
		},
	}

	err := srv.Stream(stream)
	require.NoError(t, err)

	// Verify event sequence: TextStart, TextDelta("Hello "), TextDelta("world!"), TextEnd, Done
	require.GreaterOrEqual(t, len(stream.sent), 4)
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

func TestStream_FirstEventNotUserMessage(t *testing.T) {
	q := new(mocks.MockQuerier)
	llm := &mockLanguageModel{}
	srv := NewServer(nil, q, llm, nil, slog.Default())

	ctx := authenticatedCtx("user1")
	stream := &mockStream{
		ctx: ctx,
		recvMsgs: []*aiv1.ClientEvent{
			{Event: &aiv1.ClientEvent_ToolOutput{ToolOutput: &aiv1.ToolOutput{}}},
		},
	}

	err := srv.Stream(stream)
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestStream_WrongOwner(t *testing.T) {
	q := new(mocks.MockQuerier)
	llm := &mockLanguageModel{}
	srv := NewServer(nil, q, llm, nil, slog.Default())

	org := testOrg()
	conv := testConversation(org.ID, "other-user")
	ctx := authenticatedCtx("user1")

	q.On("GetOrganizationByName", mock.Anything, "acme").Return(org, nil)
	q.On("GetConversationByName", mock.Anything, mock.Anything).Return(conv, nil)

	stream := &mockStream{
		ctx: ctx,
		recvMsgs: []*aiv1.ClientEvent{
			{Event: &aiv1.ClientEvent_Message{Message: &aiv1.UserMessage{
				Conversation: "organizations/acme/conversations/conv1",
				Parts:        []*aiv1.MessagePart{{Part: &aiv1.MessagePart_Text{Text: &aiv1.TextPart{Text: "Hi"}}}},
			}}},
		},
	}

	err := srv.Stream(stream)
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.PermissionDenied, st.Code())
}

func TestStream_ModelError(t *testing.T) {
	q := new(mocks.MockQuerier)
	llm := &mockLanguageModel{err: io.ErrUnexpectedEOF}
	srv := NewServer(nil, q, llm, nil, slog.Default())

	org := testOrg()
	uid := "user1"
	conv := testConversation(org.ID, uid)
	ctx := authenticatedCtx(uid)

	q.On("GetOrganizationByName", mock.Anything, "acme").Return(org, nil)
	q.On("GetConversationByName", mock.Anything, mock.Anything).Return(conv, nil)
	q.On("GetNextSequenceForConversation", mock.Anything, conv.ID).Return(int32(1), nil)
	q.On("CreateMessage", mock.Anything, mock.Anything).Return(db.Message{}, nil)
	q.On("IncrementConversationMessageCount", mock.Anything, conv.ID).Return(nil)
	q.On("ListMessagesNewestFirst", mock.Anything, mock.Anything).Return([]db.Message{}, nil)

	stream := &mockStream{
		ctx: ctx,
		recvMsgs: []*aiv1.ClientEvent{
			{Event: &aiv1.ClientEvent_Message{Message: &aiv1.UserMessage{
				Conversation: "organizations/acme/conversations/conv1",
				Parts:        []*aiv1.MessagePart{{Part: &aiv1.MessagePart_Text{Text: &aiv1.TextPart{Text: "Hi"}}}},
			}}},
		},
	}

	err := srv.Stream(stream)
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
	srv := NewServer(nil, q, nil, nil, slog.Default())

	convID := uuid.New()
	partsJSON, _ := marshalParts([]*aiv1.MessagePart{
		{Part: &aiv1.MessagePart_Text{Text: &aiv1.TextPart{Text: "hello"}}},
	})

	// Create messages totaling well over budget.
	var rows []db.Message
	for i := 0; i < 10; i++ {
		rows = append(rows, db.Message{
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

	row := db.Message{
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
