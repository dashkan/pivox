package aichat

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/filter"
	aiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/ai/v1"
	"github.com/dashkan/pivox/internal/server"
	"github.com/dashkan/pivox/internal/service/aichat/model"
)

func (s *Server) GetConversation(ctx context.Context, req *aiv1.GetConversationRequest) (*aiv1.Conversation, error) {
	orgName, convName, err := parseConversationName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Conversation", req.GetName())
	}

	uid := server.MustAuthenticatedUID(ctx)
	row, err := s.resolveConversation(ctx, orgName, convName, uid)
	if err != nil {
		return nil, err
	}
	return convert.ConversationToProto(row, orgName), nil
}

func (s *Server) ListConversations(ctx context.Context, req *aiv1.ListConversationsRequest) (*aiv1.ListConversationsResponse, error) {
	orgName, err := parseConversationParent(req.GetParent())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Organization", req.GetParent())
	}

	orgID, err := s.resolveOrg(ctx, orgName)
	if err != nil {
		return nil, err
	}

	uid := server.MustAuthenticatedUID(ctx)

	rows, err := filter.Query(ctx, s.db, s.conversationFilter, filter.QueryParams{
		Filter:   req.GetFilter(),
		ParentID: orgID.String(),
		UserID:   uid,
		OrderBy:  req.GetOrderBy(),
		PageSize: req.GetPageSize(),
		Cursor:   req.GetPageToken(),
		Codec:    s.codec,
	})
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid list params: %v", err)
	}

	results, err := filter.ScanConversations(rows)
	if err != nil {
		// Wrap so the interceptor's error log surfaces the
		// underlying DB cause — `apierr.Internal("database error")`
		// would mask it.
		return nil, status.Errorf(codes.Internal, "scan conversations: %v", err)
	}

	pageSize := req.GetPageSize()
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 1000 {
		pageSize = 1000
	}

	var nextPageToken string
	if int32(len(results)) > pageSize {
		nextPageToken, err = filter.EncodeNextPageToken(s.codec, results[pageSize].ID)
		if err != nil {
			return nil, apierr.Internal("encode page token")
		}
		results = results[:pageSize]
	}

	convs := make([]*aiv1.Conversation, 0, len(results))
	for _, r := range results {
		convs = append(convs, convert.ConversationToProto(r, orgName))
	}

	return &aiv1.ListConversationsResponse{
		Conversations: convs,
		NextPageToken: nextPageToken,
	}, nil
}

func (s *Server) CreateConversation(ctx context.Context, req *aiv1.CreateConversationRequest) (*aiv1.Conversation, error) {
	orgName, err := parseConversationParent(req.GetParent())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Organization", req.GetParent())
	}

	orgID, err := s.resolveOrg(ctx, orgName)
	if err != nil {
		return nil, err
	}

	uid := server.MustAuthenticatedUID(ctx)
	conv := req.GetConversation()

	row, err := s.queries.CreateConversation(ctx, db.CreateConversationParams{
		OrgID:       orgID,
		Name:        uuid.New().String()[:12],
		Title:       conv.GetTitle(),
		Description: conv.GetDescription(),
		CreatedBy:   uid,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Conversation", "")
	}

	return convert.ConversationToProto(row, orgName), nil
}

func (s *Server) UpdateConversation(ctx context.Context, req *aiv1.UpdateConversationRequest) (*aiv1.Conversation, error) {
	conv := req.GetConversation()
	orgName, convName, err := parseConversationName(conv.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Conversation", conv.GetName())
	}

	orgID, err := s.resolveOrg(ctx, orgName)
	if err != nil {
		return nil, err
	}

	uid := server.MustAuthenticatedUID(ctx)

	existing, err := s.queries.GetConversationByName(ctx, db.GetConversationByNameParams{
		OrgID: orgID,
		Name:  convName,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Conversation", conv.GetName())
	}

	// AIP-134 requires an update mask on Update RPCs. Beyond
	// convention, our SQL flips `title_user_set` whenever the
	// `title` narg is non-NULL — so a mask-less Update intended
	// only for `archived` would silently mark the title as
	// user-curated and disable auto-summarize. Reject the
	// ambiguous case explicitly.
	mask := req.GetUpdateMask()
	if mask == nil || len(mask.GetPaths()) == 0 {
		return nil, status.Error(codes.InvalidArgument,
			"update_mask is required and must list at least one field")
	}

	params := db.UpdateConversationParams{
		ID:        existing.ID,
		UpdatedBy: uid,
	}
	for _, path := range mask.GetPaths() {
		switch path {
		case "title":
			params.Title = pgtype.Text{String: conv.GetTitle(), Valid: true}
		case "description":
			params.Description = pgtype.Text{String: conv.GetDescription(), Valid: true}
		case "archived":
			params.Archived = pgtype.Bool{Bool: conv.GetArchived(), Valid: true}
		case "pinned":
			params.Pinned = pgtype.Bool{Bool: conv.GetPinned(), Valid: true}
		default:
			return nil, status.Errorf(codes.InvalidArgument,
				"update_mask contains unknown field %q", path)
		}
	}

	row, err := s.queries.UpdateConversation(ctx, params)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Conversation", conv.GetName())
	}

	return convert.ConversationToProto(row, orgName), nil
}

// SummarizeConversation generates a short title for the conversation
// by summarizing its current contents and persists the result. No-ops
// when `title_user_set` is true so we never overwrite a user-curated
// title. Idempotent in spirit: repeated calls produce a (possibly
// updated) title from the latest conversation contents.
func (s *Server) SummarizeConversation(ctx context.Context, req *aiv1.SummarizeConversationRequest) (*aiv1.Conversation, error) {
	orgName, convName, err := parseConversationName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Conversation", req.GetName())
	}

	// Ownership-checked lookup — never run summarize against a
	// peer's conversation. Mirrors the gating in artifacts.go and
	// the streaming path; treats CreatedBy mismatch as NotFound so
	// we don't leak existence of other users' conversations.
	uid := server.MustAuthenticatedUID(ctx)
	row, err := s.resolveConversation(ctx, orgName, convName, uid)
	if err != nil {
		return nil, err
	}

	// Hard short-circuit: never overwrite a user-set title. Returning
	// the conversation unchanged keeps the call idempotent and lets
	// callers fire it without first checking the flag.
	if row.TitleUserSet {
		return convert.ConversationToProto(row, orgName), nil
	}

	// Build the summarization prompt from the conversation's history.
	// We use the same `runGenerate` path as a regular call but mark
	// it stateless (no `conversation` field) — the summary call
	// itself shouldn't be persisted as a turn in the conversation
	// being summarized.
	history, err := s.loadModelHistory(ctx, row.ID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to load history: %v", err)
	}

	transcript := renderTranscriptForSummary(history)
	if transcript == "" {
		// Nothing to summarize yet; leave the title alone.
		return convert.ConversationToProto(row, orgName), nil
	}

	// Stateless one-shot via runGenerate with system_instruction.
	genReq := &aiv1.GenerateContentRequest{
		Parent:            fmt.Sprintf("organizations/%s", orgName),
		SystemInstruction: summarySystemPrompt,
		MaxOutputTokens:   32,
		Temperature:       0.3,
		Messages: []*aiv1.InputMessage{
			{
				Role: aiv1.Role_USER,
				Parts: []*aiv1.MessagePart{
					{Part: &aiv1.MessagePart_Text{Text: &aiv1.TextPart{Text: transcript}}},
				},
			},
		},
	}

	msg, _, _, err := s.runGenerate(ctx, genReq, nil)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "summarize failed: %v", err)
	}

	title := sanitizeTitle(extractTextFromMessage(msg))
	if title == "" {
		// Model returned nothing usable; leave existing title alone.
		return convert.ConversationToProto(row, orgName), nil
	}

	updated, err := s.queries.SetAutoTitle(ctx, db.SetAutoTitleParams{
		ID:    row.ID,
		Title: title,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Conversation", req.GetName())
	}

	return convert.ConversationToProto(updated, orgName), nil
}

const summarySystemPrompt = "You generate concise conversation titles. " +
	"Given a transcript, respond with a 3-6 word title that captures the topic. " +
	"Return only the title as plain text. " +
	"Do not use Markdown, asterisks, backticks, underscores, brackets, or any " +
	"other formatting characters. Do not wrap the title in quotes. Do not add " +
	"a leading label, preface, or trailing punctuation. Plain words only."

// renderTranscriptForSummary collapses the model history into a flat
// transcript suitable for one-shot summarization.
func renderTranscriptForSummary(history []model.Message) string {
	var sb strings.Builder
	for _, m := range history {
		var text strings.Builder
		for _, p := range m.Parts {
			if p.Type == "text" {
				text.WriteString(p.Text)
			}
		}
		if text.Len() == 0 {
			continue
		}
		switch m.Role {
		case "user":
			sb.WriteString("User: ")
		case "assistant":
			sb.WriteString("Assistant: ")
		case "tool":
			continue
		default:
			sb.WriteString(m.Role + ": ")
		}
		sb.WriteString(text.String())
		sb.WriteString("\n")
	}
	return sb.String()
}

// extractTextFromMessage flattens an assistant `Message`'s text parts.
func extractTextFromMessage(m *aiv1.Message) string {
	if m == nil {
		return ""
	}
	var sb strings.Builder
	for _, p := range m.GetParts() {
		if tp := p.GetText(); tp != nil {
			sb.WriteString(tp.GetText())
		}
	}
	return sb.String()
}

// sanitizeTitle strips formatting characters the model sometimes
// emits despite the system prompt forbidding them — surrounding
// quotes, markdown emphasis (`**bold**`, `*italic*`, `_under_`),
// inline code backticks, leading/trailing punctuation, and stray
// brackets. Defensive belt-and-suspenders so a single ignored
// instruction doesn't surface as a `**Title**` in the UI.
func sanitizeTitle(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}

	// Strip markdown formatting characters anywhere in the string.
	// We do this BEFORE trimming surrounding quotes so e.g. a
	// `"**Title**"` collapses cleanly to `Title`. We strip ALL
	// asterisks / underscores rather than only emphasis-like
	// patterns — titles practically never need them as content
	// (arithmetic in a title is rare; the worst case is "5 3 = 15"
	// which still reads).
	for _, ch := range []string{"**", "__", "`", "*", "_"} {
		s = strings.ReplaceAll(s, ch, "")
	}
	// Collapse any double-spaces produced by the strip pass.
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}

	// Leading/trailing brackets and bare quotes the prompt asked us
	// not to use. Iterative because nested wrappers like `["Title"]`
	// need multiple passes to fully collapse.
	for {
		original := s
		s = strings.TrimSpace(s)
		s = trimSurroundingQuotes(s)
		s = strings.TrimFunc(s, func(r rune) bool {
			switch r {
			case '[', ']', '(', ')', '<', '>', '"', '\'', ' ':
				return true
			}
			return false
		})
		if s == original {
			break
		}
	}

	// Trailing punctuation the model adds out of habit (period,
	// comma, colon, semicolon, exclaim).
	s = strings.TrimRightFunc(s, func(r rune) bool {
		switch r {
		case '.', ',', ':', ';', '!', '?':
			return true
		}
		return false
	})

	return strings.TrimSpace(s)
}

// trimSurroundingQuotes strips a single layer of paired ASCII or
// smart quotes from the ends.
func trimSurroundingQuotes(s string) string {
	if len(s) < 2 {
		return s
	}
	pairs := [][2]rune{{'"', '"'}, {'\'', '\''}, {'\u201c', '\u201d'}, {'\u2018', '\u2019'}}
	runes := []rune(s)
	first, last := runes[0], runes[len(runes)-1]
	for _, pair := range pairs {
		if first == pair[0] && last == pair[1] {
			return string(runes[1 : len(runes)-1])
		}
	}
	return s
}

func (s *Server) DeleteConversation(ctx context.Context, req *aiv1.DeleteConversationRequest) (*emptypb.Empty, error) {
	orgName, convName, err := parseConversationName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Conversation", req.GetName())
	}

	orgID, err := s.resolveOrg(ctx, orgName)
	if err != nil {
		return nil, err
	}

	existing, err := s.queries.GetConversationByName(ctx, db.GetConversationByNameParams{
		OrgID: orgID,
		Name:  convName,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Conversation", req.GetName())
	}

	if err := s.queries.DeleteConversation(ctx, existing.ID); err != nil {
		return nil, apierr.HandleResourceError(err, "Conversation", req.GetName())
	}

	return &emptypb.Empty{}, nil
}

// resolveOrg resolves an org name to its UUID.
func (s *Server) resolveOrg(ctx context.Context, orgName string) (uuid.UUID, error) {
	org, err := s.queries.GetOrganizationByName(ctx, orgName)
	if err != nil {
		return uuid.Nil, apierr.HandleResourceError(err, "Organization", fmt.Sprintf("organizations/%s", orgName))
	}
	return org.ID, nil
}
