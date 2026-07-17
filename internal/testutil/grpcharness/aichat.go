package grpcharness

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/dashkan/pivox/internal/appkey"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/permission"
	aiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/ai/v1"
	"github.com/dashkan/pivox/internal/service/aichat"
	"github.com/dashkan/pivox/internal/service/aichat/model"
)

// WithAiChatServer registers the AiChat service on the harness's gRPC
// server, wired to the harness's Pool/Queries and a real permission
// Resolver (so the `*All` audit-bypass path is production-shaped). The
// model is a no-op: the List/Get RPCs this option exists to exercise
// never call the model, so any invocation is a test bug and fails loud.
func WithAiChatServer() Option {
	return func(c *config) {
		c.registerServices = append(c.registerServices, func(h *Harness, s *grpc.Server) {
			registerAiChatServerWithModel(h, s, noopModel{})
		})
	}
}

// WithAiChatServerModel is WithAiChatServer with a caller-supplied language
// model, for tests that exercise the generate path (StreamGenerateContent /
// GenerateContent) end-to-end against a real DB — e.g. a model that errors to
// prove failed generations leave no auto-created conversation, or a scripted
// model that streams text to prove the persistence + activity-clock flow.
func WithAiChatServerModel(m model.LanguageModel) Option {
	return func(c *config) {
		c.registerServices = append(c.registerServices, func(h *Harness, s *grpc.Server) {
			registerAiChatServerWithModel(h, s, m)
		})
	}
}

func registerAiChatServerWithModel(h *Harness, s *grpc.Server, m model.LanguageModel) {
	codec, err := appkey.NewFromHex(strings.Repeat("ab", 32))
	if err != nil {
		panic("grpcharness: hard-coded test app key is malformed: " + err.Error())
	}
	aiv1.RegisterAiChatServer(s, aichat.NewServer(aichat.Config{
		Pool:     h.Pool,
		Queries:  h.Queries,
		Model:    m,
		Codec:    codec,
		Resolver: permission.NewResolver(h.Queries),
		Logger:   SilentLogger(),
	}))
}

// noopModel is a LanguageModel that never streams. The AiChat List/Get
// handlers don't touch the model, so a Stream call here means a test
// wandered into a generate path it didn't set up — surface it.
type noopModel struct{}

func (noopModel) Name() string { return "noop" }

func (noopModel) Stream(context.Context, model.StreamRequest) (model.StreamReader, error) {
	return nil, fmt.Errorf("grpcharness: noopModel.Stream called; the AiChat test server is List/Get-only")
}

// SeedConversation inserts an ai_conversations row owned by `owner`
// under `orgID`, via the same CreateConversation query the handler
// uses. The returned row's .ID is the parent id for SeedMessage /
// SeedArtifact; its .Name is the slug leaf for the resource path.
func (h *Harness) SeedConversation(t *testing.T, orgID uuid.UUID, owner *Caller, title string) db.AiConversation {
	t.Helper()
	row, err := h.Queries.CreateConversation(context.Background(), db.CreateConversationParams{
		OrgID:     orgID,
		Name:      uuid.New().String()[:12],
		Title:     title,
		CreatedBy: owner.IdentityID,
	})
	require.NoError(t, err)
	return row
}

// SeedMessage inserts an ai_messages row under conversationID. Parts is
// left empty ("[]") — the boundary tests care about row identity and
// count, not content.
//
// NOTE: this inserts the message row directly and does NOT bump the parent
// conversation's message_count / last_message_time (unlike the production
// persist path, which calls IncrementConversationMessageCount). Tests that
// seed messages and then order conversations by lastMessageTime must set
// last_message_time explicitly (see the lifecycle tests' setLastMessageTime).
func (h *Harness) SeedMessage(t *testing.T, conversationID uuid.UUID, sequence int64) db.AiMessage {
	t.Helper()
	row, err := h.Queries.CreateMessage(context.Background(), db.CreateMessageParams{
		ConversationID: conversationID,
		Name:           uuid.New().String()[:12],
		Role:           "user",
		Parts:          json.RawMessage("[]"),
		Sequence:       sequence,
	})
	require.NoError(t, err)
	return row
}

// SeedArtifact inserts an ai_artifacts row under conversationID. The
// returned row's .ID is the parent id for SeedArtifactVersion.
func (h *Harness) SeedArtifact(t *testing.T, conversationID uuid.UUID, title string) db.AiArtifact {
	t.Helper()
	row, err := h.Queries.CreateArtifact(context.Background(), db.CreateArtifactParams{
		ConversationID: conversationID,
		Name:           uuid.New().String()[:12],
		Type:           "text/markdown",
		Title:          title,
	})
	require.NoError(t, err)
	return row
}

// SeedArtifactVersion inserts an inline ai_artifact_versions row under
// artifactID. Sequence is derived by the query from the current max.
func (h *Harness) SeedArtifactVersion(t *testing.T, artifactID uuid.UUID) db.AiArtifactVersion {
	t.Helper()
	row, err := h.Queries.CreateInlineArtifactVersion(context.Background(), db.CreateInlineArtifactVersionParams{
		ArtifactID:        artifactID,
		Name:              uuid.New().String()[:12],
		InlineData:        []byte("x"),
		InlineContentType: pgtype.Text{String: "text/plain", Valid: true},
		InlineSizeBytes:   pgtype.Int8{Int64: 1, Valid: true},
	})
	require.NoError(t, err)
	return row
}
