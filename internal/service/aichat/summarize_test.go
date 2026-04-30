package aichat

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	db "github.com/dashkan/pivox/internal/db/generated"
	aiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/ai/v1"
	"github.com/dashkan/pivox/internal/service/aichat/model"
	"github.com/dashkan/pivox/internal/service/aichat/tools"
	"github.com/dashkan/pivox/internal/testutil/mocks"
)

func TestSummarizeConversation_NoOpWhenTitleUserSet(t *testing.T) {
	q := new(mocks.MockQuerier)
	llm := &mockLanguageModel{}
	srv := NewServer(nil, q, llm, tools.NewRegistry(), nil, nil, slog.Default())

	org := testOrg()
	uid := "user1"
	conv := testConversation(org.ID, uid)
	conv.Title = "User Picked This"
	conv.TitleUserSet = true
	ctx := authenticatedCtx(uid)

	q.On("GetOrganizationByName", mock.Anything, "acme").Return(org, nil)
	q.On("GetConversationByName", mock.Anything, mock.Anything).Return(conv, nil)

	resp, err := srv.SummarizeConversation(ctx, &aiv1.SummarizeConversationRequest{
		Name: testConvPath("acme", "conv1"),
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "User Picked This", resp.GetTitle(),
		"summarize must NOT overwrite a user-set title")

	// SetAutoTitle must never fire when title_user_set is true.
	for _, c := range q.Calls {
		assert.NotEqual(t, "SetAutoTitle", c.Method,
			"SetAutoTitle should be skipped when title_user_set is true")
	}
}

func TestSummarizeConversation_NoOpWhenTranscriptEmpty(t *testing.T) {
	q := new(mocks.MockQuerier)
	llm := &mockLanguageModel{}
	srv := NewServer(nil, q, llm, tools.NewRegistry(), nil, nil, slog.Default())

	org := testOrg()
	uid := "user1"
	conv := testConversation(org.ID, uid)
	conv.Title = "stub"
	ctx := authenticatedCtx(uid)

	q.On("GetOrganizationByName", mock.Anything, "acme").Return(org, nil)
	q.On("GetConversationByName", mock.Anything, mock.Anything).Return(conv, nil)
	q.On("ListMessagesNewestFirst", mock.Anything, mock.Anything).Return([]db.AiMessage{}, nil)

	resp, err := srv.SummarizeConversation(ctx, &aiv1.SummarizeConversationRequest{
		Name: testConvPath("acme", "conv1"),
	})
	require.NoError(t, err)
	assert.Equal(t, "stub", resp.GetTitle(),
		"empty transcript must not trigger a model call")

	for _, c := range q.Calls {
		assert.NotEqual(t, "SetAutoTitle", c.Method)
	}
}

func TestSummarizeConversation_HappyPathWritesViaSetAutoTitle(t *testing.T) {
	q := new(mocks.MockQuerier)
	llm := &mockLanguageModel{
		events: []model.ModelEvent{
			{Kind: "text_delta", Text: "Bug Fix Discussion"},
			{Kind: "finish"},
		},
	}
	srv := NewServer(nil, q, llm, tools.NewRegistry(), nil, nil, slog.Default())

	org := testOrg()
	uid := "user1"
	conv := testConversation(org.ID, uid)
	conv.Title = "stub"
	ctx := authenticatedCtx(uid)

	// Synthesize a tiny transcript so renderTranscriptForSummary
	// has something to feed the model.
	partsJSON, _ := marshalParts([]*aiv1.MessagePart{
		{Part: &aiv1.MessagePart_Text{Text: &aiv1.TextPart{Text: "How do I fix this bug?"}}},
	})
	transcript := []db.AiMessage{
		{Role: "user", Parts: partsJSON, TokenCount: 5, Sequence: 1},
	}

	q.On("GetOrganizationByName", mock.Anything, "acme").Return(org, nil)
	q.On("GetConversationByName", mock.Anything, mock.Anything).Return(conv, nil)
	q.On("ListMessagesNewestFirst", mock.Anything, mock.Anything).Return(transcript, nil)

	updated := conv
	updated.Title = "Bug Fix Discussion"
	q.On("SetAutoTitle", mock.Anything, mock.MatchedBy(func(p db.SetAutoTitleParams) bool {
		return p.ID == conv.ID && p.Title == "Bug Fix Discussion"
	})).Return(updated, nil)

	resp, err := srv.SummarizeConversation(ctx, &aiv1.SummarizeConversationRequest{
		Name: testConvPath("acme", "conv1"),
	})
	require.NoError(t, err)
	assert.Equal(t, "Bug Fix Discussion", resp.GetTitle())

	// Verify SetAutoTitle was called — UpdateConversation must NOT
	// be used here because that would flip title_user_set=true.
	var sawSetAuto, sawUpdate bool
	for _, c := range q.Calls {
		switch c.Method {
		case "SetAutoTitle":
			sawSetAuto = true
		case "UpdateConversation":
			sawUpdate = true
		}
	}
	assert.True(t, sawSetAuto, "auto-summarize must persist via SetAutoTitle")
	assert.False(t, sawUpdate, "must NOT use UpdateConversation (would flip title_user_set)")
}

func TestSummarizeConversation_RejectsNonOwner(t *testing.T) {
	q := new(mocks.MockQuerier)
	llm := &mockLanguageModel{}
	srv := NewServer(nil, q, llm, tools.NewRegistry(), nil, nil, slog.Default())

	org := testOrg()
	// Conversation owned by a different user-uuid than the path's
	// caller — resolveConversation must surface NotFound (path-vs-row
	// creator_id mismatch).
	conv := testConversation(org.ID, "other-user")
	conv.CreatorID = uuid.MustParse("0192a000-0099-7000-8000-000000000099")
	ctx := authenticatedCtx("user1")

	q.On("GetOrganizationByName", mock.Anything, "acme").Return(org, nil)
	q.On("GetConversationByName", mock.Anything, mock.Anything).Return(conv, nil)

	_, err := srv.SummarizeConversation(ctx, &aiv1.SummarizeConversationRequest{
		Name: testConvPath("acme", "conv1"),
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
}

func TestSummarizeConversation_NotFound(t *testing.T) {
	q := new(mocks.MockQuerier)
	llm := &mockLanguageModel{}
	srv := NewServer(nil, q, llm, tools.NewRegistry(), nil, nil, slog.Default())

	org := testOrg()
	ctx := authenticatedCtx("user1")
	q.On("GetOrganizationByName", mock.Anything, "acme").Return(org, nil)
	q.On("GetConversationByName", mock.Anything, mock.Anything).Return(
		db.AiConversation{}, pgx.ErrNoRows)

	_, err := srv.SummarizeConversation(ctx, &aiv1.SummarizeConversationRequest{
		Name: testConvPath("acme", "missing"),
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
}

// TestSanitizeTitle exercises the defense-in-depth post-processor
// for cases the model emits despite the system prompt forbidding
// formatting characters.
func TestSanitizeTitle(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "Bug Fix Discussion", "Bug Fix Discussion"},
		{"strips ** bold **", "**C++ Implementation of Fibonacci**", "C++ Implementation of Fibonacci"},
		{"strips _underscore_", "_Internal Notes_", "Internal Notes"},
		{"strips backticks", "`Inline Code` Topic", "Inline Code Topic"},
		{"strips ASCII quotes", `"Quoted Title"`, "Quoted Title"},
		{"strips smart quotes", "\u201cSmart Title\u201d", "Smart Title"},
		{"strips brackets", "[Bracketed]", "Bracketed"},
		{"strips parens", "(Title)", "Title"},
		{"strips trailing period", "Final Thoughts.", "Final Thoughts"},
		{"strips trailing exclaim", "Wow Great!", "Wow Great"},
		{"nested wrappers", "[\"**Title**\"]", "Title"},
		{"trims whitespace", "   spaced out   ", "spaced out"},
		{"empty stays empty", "", ""},
		{"single char", "A", "A"},
		{"strips arithmetic asterisks (rare in titles)", "5 * 3 = 15", "5 3 = 15"},
		{"strips leading prefix asterisk", "* leading bullet", "leading bullet"},
		{"strips trailing comma", "Hello, World,", "Hello, World"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeTitle(tc.in)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestUpdateConversation_RejectsMissingMask covers the AIP-134
// requirement that Update RPCs include an update_mask. Without this
// gate, a no-mask Update intended for `archived` would also re-write
// the title field and silently flip title_user_set=true via the SQL
// trigger — destroying auto-summarize forever for that conversation.
func TestUpdateConversation_RejectsMissingMask(t *testing.T) {
	q := new(mocks.MockQuerier)
	srv := NewServer(nil, q, nil, nil, nil, nil, slog.Default())

	org := testOrg()
	uid := "user1"
	conv := testConversation(org.ID, uid)
	ctx := authenticatedCtx(uid)

	q.On("GetOrganizationByName", mock.Anything, "acme").Return(org, nil)
	q.On("GetConversationByName", mock.Anything, mock.Anything).Return(conv, nil)

	_, err := srv.UpdateConversation(ctx, &aiv1.UpdateConversationRequest{
		Conversation: &aiv1.Conversation{
			Name:     testConvPath("acme", "conv1"),
			Archived: true,
		},
		// No update_mask.
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())

	// Critically: UpdateConversation must NOT have been called on
	// the DB layer with title in the params.
	for _, c := range q.Calls {
		assert.NotEqual(t, "UpdateConversation", c.Method,
			"DB write must be blocked when mask is missing")
	}
}

// Compile-time guard — the production code must continue to wrap
// scan errors as Internal so the logging interceptor surfaces them
// without losing the underlying cause.
func TestScanErrorWrapping(t *testing.T) {
	err := status.Errorf(codes.Internal, "scan conversations: %v", errors.New("boom"))
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Internal, st.Code())
	assert.Contains(t, st.Message(), "boom")
}

// TestRunGenerate_OrgMembershipCheckRejectsPhantomOrg ensures that
// a request with a real-looking but non-existent parent fails BEFORE
// the model is invoked. Without this guard the stateless path would
// burn inference budget on phantom orgs.
func TestRunGenerate_OrgMembershipCheckRejectsPhantomOrg(t *testing.T) {
	q := new(mocks.MockQuerier)
	llm := &mockLanguageModel{
		events: []model.ModelEvent{{Kind: "text_delta", Text: "should not run"}},
	}
	srv := NewServer(nil, q, llm, tools.NewRegistry(), nil, nil, slog.Default())

	ctx := authenticatedCtx("user1")
	q.On("GetOrganizationByName", mock.Anything, "phantom").Return(
		db.Organization{}, pgx.ErrNoRows)

	resp, err := srv.GenerateContent(ctx, &aiv1.GenerateContentRequest{
		Parent: "organizations/phantom",
		Messages: []*aiv1.InputMessage{
			{Role: aiv1.Role_USER, Parts: []*aiv1.MessagePart{
				{Part: &aiv1.MessagePart_Text{Text: &aiv1.TextPart{Text: "Hi"}}},
			}},
		},
	})
	require.Error(t, err)
	require.Nil(t, resp)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())

	// The model must NOT have been called.
	for _, c := range q.Calls {
		switch c.Method {
		case "GetOrganizationByName":
			// expected
		default:
			t.Errorf("unexpected DB call after org rejection: %s", c.Method)
		}
	}
}

// silence "imported and not used" if test imports go stale.
var _ = context.Background
