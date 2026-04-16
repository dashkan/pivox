//go:build dev

package aichat_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	db "github.com/dashkan/pivox/internal/db/generated"
	aiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/ai/v1"
	"github.com/dashkan/pivox/internal/server"
	"github.com/dashkan/pivox/internal/service/aichat"
	"github.com/dashkan/pivox/internal/service/aichat/model"
	"github.com/dashkan/pivox/internal/service/aichat/tools"
	"github.com/dashkan/pivox/internal/testutil"
)

const testUID = "test-user-1"

// setupIntegration spins up a real Postgres + in-process gRPC with auth.
func setupIntegration(t *testing.T) (aiv1.AiChatClient, *db.Queries, func()) {
	t.Helper()

	pool, queries, dbCleanup := testutil.SetupTestDB(t)

	// Mock model that returns a fixed response.
	llm := &fixedModel{text: "Hello from integration test!"}
	srv := aichat.NewServer(pool, queries, llm, tools.NewRegistry(), slog.Default())

	// gRPC server with a fake auth interceptor that injects testUID.
	lis := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(fakeAuthUnary),
		grpc.ChainStreamInterceptor(fakeAuthStream),
	)
	aiv1.RegisterAiChatServer(grpcServer, srv)

	go func() { _ = grpcServer.Serve(lis) }()

	conn, err := grpc.NewClient(
		"passthrough:///bufconn",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)

	client := aiv1.NewAiChatClient(conn)
	cleanup := func() {
		conn.Close()
		grpcServer.GracefulStop()
		dbCleanup()
	}
	return client, queries, cleanup
}

// fakeAuthUnary injects testUID into every unary call.
func fakeAuthUnary(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	return handler(server.WithAuthenticatedUID(ctx, testUID), req)
}

// fakeAuthStream injects testUID into every streaming call.
func fakeAuthStream(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	return handler(srv, &authStream{ServerStream: ss})
}

type authStream struct {
	grpc.ServerStream
}

func (s *authStream) Context() context.Context {
	return server.WithAuthenticatedUID(s.ServerStream.Context(), testUID)
}

func createTestOrg(t *testing.T, queries *db.Queries) db.Organization {
	t.Helper()
	org, err := queries.CreateOrganization(context.Background(), db.CreateOrganizationParams{
		ID:          uuid.New(),
		Name:        "integ-org-" + uuid.New().String()[:8],
		DisplayName: "Integration Test Org",
		CreatedBy:   testUID,
	})
	require.NoError(t, err)
	return org
}

func createTestConversation(t *testing.T, queries *db.Queries, orgID uuid.UUID) db.Conversation {
	t.Helper()
	conv, err := queries.CreateConversation(context.Background(), db.CreateConversationParams{
		OrgID:      orgID,
		CreatorUid: testUID,
		Name:       "conv-" + uuid.New().String()[:8],
		Title:      "Test Conversation",
		CreatedBy:  testUID,
	})
	require.NoError(t, err)
	return conv
}

// --- Tests ---

func TestIntegration_ConversationLifecycle(t *testing.T) {
	client, queries, cleanup := setupIntegration(t)
	defer cleanup()

	org := createTestOrg(t, queries)
	ctx := context.Background()

	// Create
	created, err := client.CreateConversation(ctx, &aiv1.CreateConversationRequest{
		Parent:       "organizations/" + org.Name,
		Conversation: &aiv1.Conversation{Title: "My Chat"},
	})
	require.NoError(t, err)
	assert.Equal(t, "My Chat", created.GetTitle())
	assert.Contains(t, created.GetName(), "organizations/"+org.Name+"/conversations/")

	// Get
	got, err := client.GetConversation(ctx, &aiv1.GetConversationRequest{Name: created.GetName()})
	require.NoError(t, err)
	assert.Equal(t, created.GetName(), got.GetName())
	assert.Equal(t, "My Chat", got.GetTitle())

	// List
	listed, err := client.ListConversations(ctx, &aiv1.ListConversationsRequest{
		Parent: "organizations/" + org.Name,
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(listed.GetConversations()), 1)

	// Update
	updated, err := client.UpdateConversation(ctx, &aiv1.UpdateConversationRequest{
		Conversation: &aiv1.Conversation{
			Name:  created.GetName(),
			Title: "Renamed Chat",
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "Renamed Chat", updated.GetTitle())

	// Delete (soft)
	_, err = client.DeleteConversation(ctx, &aiv1.DeleteConversationRequest{Name: created.GetName()})
	require.NoError(t, err)

	// Get after delete → not found
	_, err = client.GetConversation(ctx, &aiv1.GetConversationRequest{Name: created.GetName()})
	require.Error(t, err)
}

func TestIntegration_StreamAndMessages(t *testing.T) {
	client, queries, cleanup := setupIntegration(t)
	defer cleanup()

	org := createTestOrg(t, queries)
	conv := createTestConversation(t, queries, org.ID)
	ctx := context.Background()
	convName := "organizations/" + org.Name + "/conversations/" + conv.Name

	// Stream a message
	stream, err := client.Stream(ctx)
	require.NoError(t, err)

	err = stream.Send(&aiv1.ClientEvent{
		Event: &aiv1.ClientEvent_Message{Message: &aiv1.UserMessage{
			Conversation: convName,
			Parts: []*aiv1.MessagePart{
				{Part: &aiv1.MessagePart_Text{Text: &aiv1.TextPart{Text: "Hello AI"}}},
			},
		}},
	})
	require.NoError(t, err)
	require.NoError(t, stream.CloseSend())

	// Collect server events.
	var events []*aiv1.ServerEvent
	for {
		ev, err := stream.Recv()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		events = append(events, ev)
	}

	// Should have: TextStart, TextDelta(s), TextEnd, Done
	require.GreaterOrEqual(t, len(events), 4)
	assert.NotNil(t, events[0].GetTextStart())
	assert.NotNil(t, events[len(events)-2].GetTextEnd())
	assert.NotNil(t, events[len(events)-1].GetDone())

	// Verify messages persisted
	msgs, err := client.ListMessages(ctx, &aiv1.ListMessagesRequest{Parent: convName})
	require.NoError(t, err)
	assert.Equal(t, 2, len(msgs.GetMessages())) // user + assistant

	// Verify message_count updated
	got, err := client.GetConversation(ctx, &aiv1.GetConversationRequest{Name: convName})
	require.NoError(t, err)
	assert.Equal(t, int32(2), got.GetMessageCount())
}

func TestIntegration_ContentHandler_InlineArtifact(t *testing.T) {
	_, queries, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	org := createTestOrg(t, queries)
	conv := createTestConversation(t, queries, org.ID)

	// Create artifact + inline version directly via DB.
	art, err := queries.CreateArtifact(context.Background(), db.CreateArtifactParams{
		ConversationID: conv.ID,
		Name:           "art1",
		Type:           "code",
		Title:          "main.py",
	})
	require.NoError(t, err)

	ver, err := queries.CreateInlineArtifactVersion(context.Background(), db.CreateInlineArtifactVersionParams{
		ArtifactID:        art.ID,
		Name:              "v1",
		InlineData:        []byte("print('hello')"),
		InlineContentType: pgtype.Text{String: "text/x-python", Valid: true},
		InlineSizeBytes:   pgtype.Int8{Int64: 14, Valid: true},
	})
	require.NoError(t, err)
	require.NotEmpty(t, ver.ID)

	// Serve via content handler
	h := aichat.NewContentHandler(queries, slog.Default())
	path := "/v1/organizations/" + org.Name + "/conversations/" + conv.Name + "/artifacts/art1/versions/v1:content"
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req = req.WithContext(server.WithAuthenticatedUID(req.Context(), testUID))
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "text/x-python", w.Header().Get("Content-Type"))
	assert.Equal(t, "print('hello')", w.Body.String())
}

func TestIntegration_DeleteConversationCascade(t *testing.T) {
	client, queries, cleanup := setupIntegration(t)
	defer cleanup()

	org := createTestOrg(t, queries)
	conv := createTestConversation(t, queries, org.ID)
	ctx := context.Background()
	convName := "organizations/" + org.Name + "/conversations/" + conv.Name

	// Stream to create messages.
	stream, err := client.Stream(ctx)
	require.NoError(t, err)
	err = stream.Send(&aiv1.ClientEvent{
		Event: &aiv1.ClientEvent_Message{Message: &aiv1.UserMessage{
			Conversation: convName,
			Parts:        []*aiv1.MessagePart{{Part: &aiv1.MessagePart_Text{Text: &aiv1.TextPart{Text: "test"}}}},
		}},
	})
	require.NoError(t, err)
	require.NoError(t, stream.CloseSend())
	for {
		_, err := stream.Recv()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
	}

	// Verify messages exist.
	msgs, err := client.ListMessages(ctx, &aiv1.ListMessagesRequest{Parent: convName})
	require.NoError(t, err)
	assert.Equal(t, 2, len(msgs.GetMessages()))

	// Delete conversation.
	_, err = client.DeleteConversation(ctx, &aiv1.DeleteConversationRequest{Name: convName})
	require.NoError(t, err)

	// Messages should be gone (conversation soft-deleted, query filters by delete_time).
	_, err = client.GetConversation(ctx, &aiv1.GetConversationRequest{Name: convName})
	require.Error(t, err)
}

func TestIntegration_StreamWrongOwner(t *testing.T) {
	_, queries, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	org := createTestOrg(t, queries)

	// Create conversation owned by a different user.
	conv, err := queries.CreateConversation(context.Background(), db.CreateConversationParams{
		OrgID:      org.ID,
		CreatorUid: "other-user",
		Name:       "conv-" + uuid.New().String()[:8],
		Title:      "Other's Chat",
		CreatedBy:  "other-user",
	})
	require.NoError(t, err)

	llm := &fixedModel{text: "should not reach here"}
	srv := aichat.NewServer(nil, queries, llm, tools.NewRegistry(), slog.Default())

	lis := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer(
		grpc.ChainStreamInterceptor(fakeAuthStream),
	)
	aiv1.RegisterAiChatServer(grpcServer, srv)
	go func() { _ = grpcServer.Serve(lis) }()
	defer grpcServer.GracefulStop()

	conn, err := grpc.NewClient(
		"passthrough:///bufconn",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	defer conn.Close()

	client := aiv1.NewAiChatClient(conn)
	stream, err := client.Stream(context.Background())
	require.NoError(t, err)

	convName := "organizations/" + org.Name + "/conversations/" + conv.Name
	err = stream.Send(&aiv1.ClientEvent{
		Event: &aiv1.ClientEvent_Message{Message: &aiv1.UserMessage{
			Conversation: convName,
			Parts:        []*aiv1.MessagePart{{Part: &aiv1.MessagePart_Text{Text: &aiv1.TextPart{Text: "Hi"}}}},
		}},
	})
	require.NoError(t, err)

	// Should get an error on Recv.
	_, err = stream.Recv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PermissionDenied")
}

// fixedModel returns a fixed text response for integration tests.
type fixedModel struct {
	text string
}

func (m *fixedModel) Stream(_ context.Context, _ model.StreamRequest) (model.StreamReader, error) {
	return &fixedReader{text: m.text}, nil
}

type fixedReader struct {
	text string
	pos  int
}

func (r *fixedReader) Next(_ context.Context) (model.ModelEvent, error) {
	switch r.pos {
	case 0:
		r.pos++
		return model.ModelEvent{Kind: "text_delta", Text: r.text}, nil
	case 1:
		r.pos++
		return model.ModelEvent{Kind: "finish"}, nil
	default:
		return model.ModelEvent{}, io.EOF
	}
}

func (r *fixedReader) Close() error { return nil }
