package aichat

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/filter"
	"github.com/dashkan/pivox/internal/permission"
	aiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/ai/v1"
	"github.com/dashkan/pivox/internal/server"
	"github.com/dashkan/pivox/internal/service/aichat/model"
)

func (s *Server) GetConversation(ctx context.Context, req *aiv1.GetConversationRequest) (*aiv1.Conversation, error) {
	orgName, pathUser, convName, err := parseConversationName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Conversation", req.GetName())
	}
	row, err := s.resolveConversation(ctx, orgName, pathUser, convName, permission.AiConversationsReadAll)
	if err != nil {
		return nil, err
	}
	actors, err := s.resolveConversationActors(ctx, []db.AiConversation{row})
	if err != nil {
		return nil, err
	}
	return convert.ConversationToProto(row, orgName, actors), nil
}

func (s *Server) ListConversations(ctx context.Context, req *aiv1.ListConversationsRequest) (*aiv1.ListConversationsResponse, error) {
	orgName, pathUser, err := parseConversationParent(req.GetParent())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "User", req.GetParent())
	}

	orgID, err := s.resolveOrg(ctx, orgName)
	if err != nil {
		return nil, err
	}

	// Verify the path-bound user matches the caller, OR the caller
	// carries `ai.conversations.readAll`. Without this check a member
	// could enumerate any peer's conversations by guessing user-uuids.
	if err := s.verifyOwnerOrAllPerm(ctx, orgID, pathUser, permission.AiConversationsReadAll); err != nil {
		return nil, err
	}

	rows, err := filter.Query(ctx, s.pool, s.conversationFilter, filter.QueryParams{
		Filter:   req.GetFilter(),
		ParentID: orgID.String(),
		UserID:   pathUser.String(),
		OrderBy:  req.GetOrderBy(),
		PageSize: req.GetPageSize(),
		Cursor:   req.GetPageToken(),
		Codec:    s.codec,
	})
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("filter", err.Error()))
	}

	results, err := filter.ScanConversations(rows)
	if err != nil {
		slog.ErrorContext(ctx, "scan conversations failed", "error", err)
		return nil, apierr.Internal("scan conversations")
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

	actors, err := s.resolveConversationActors(ctx, results)
	if err != nil {
		return nil, err
	}
	convs := make([]*aiv1.Conversation, 0, len(results))
	for _, r := range results {
		convs = append(convs, convert.ConversationToProto(r, orgName, actors))
	}

	return &aiv1.ListConversationsResponse{
		Conversations: convs,
		NextPageToken: nextPageToken,
	}, nil
}

func (s *Server) CreateConversation(ctx context.Context, req *aiv1.CreateConversationRequest) (*aiv1.Conversation, error) {
	orgName, pathUser, err := parseConversationParent(req.GetParent())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "User", req.GetParent())
	}

	orgID, err := s.resolveOrg(ctx, orgName)
	if err != nil {
		return nil, err
	}

	// Create is creator-only — no `*All` bypass. An admin auditing
	// another user's conversations doesn't need to mint conversations
	// on their behalf, and allowing it would let the admin frame a
	// user with manufactured chat history.
	callerUserID := server.MustPivoxUserID(ctx)
	if pathUser != callerUserID {
		return nil, apierr.PermissionDenied("conversations may only be created under the caller's own user-uuid")
	}

	conv := req.GetConversation()
	row, err := s.queries.CreateConversation(ctx, db.CreateConversationParams{
		OrgID:       orgID,
		Name:        uuid.New().String()[:12],
		Title:       conv.GetTitle(),
		Description: conv.GetDescription(),
		CreatedBy:   callerUserID,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Conversation", "")
	}
	// Best-effort enrichment after commit: don't fail the create on
	// a transient identity lookup error.
	actors, resolveErr := s.resolveConversationActors(ctx, []db.AiConversation{row})
	if resolveErr != nil {
		slog.WarnContext(ctx, "create conversation: actor resolution failed; returning proto without audit actors",
			"conversation_id", row.ID, "error", resolveErr)
		actors = nil
	}
	return convert.ConversationToProto(row, orgName, actors), nil
}

func (s *Server) UpdateConversation(ctx context.Context, req *aiv1.UpdateConversationRequest) (*aiv1.Conversation, error) {
	conv := req.GetConversation()
	orgName, pathUser, convName, err := parseConversationName(conv.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Conversation", conv.GetName())
	}

	// Update is creator-only. The audit `*All` perms cover read/
	// delete (legal-hold cleanup) but admins don't edit titles/
	// archived flags on a user's behalf — the user owns the
	// conversation's mutable state.
	existing, err := s.resolveConversation(ctx, orgName, pathUser, convName, "")
	if err != nil {
		return nil, err
	}

	mask := req.GetUpdateMask()
	if mask == nil || len(mask.GetPaths()) == 0 {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("update_mask",
			"update_mask is required and must list at least one field"))
	}

	params := db.UpdateConversationParams{
		ID:        existing.ID,
		UpdatedBy: convert.PgUUID(server.MustPivoxUserID(ctx)),
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
			return nil, apierr.InvalidArgument(apierr.FieldViolation("update_mask",
				fmt.Sprintf("unknown field %q", path)))
		}
	}

	row, err := s.queries.UpdateConversation(ctx, params)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Conversation", conv.GetName())
	}
	actors, resolveErr := s.resolveConversationActors(ctx, []db.AiConversation{row})
	if resolveErr != nil {
		slog.WarnContext(ctx, "update conversation: actor resolution failed; returning proto without audit actors",
			"conversation_id", row.ID, "error", resolveErr)
		actors = nil
	}
	return convert.ConversationToProto(row, orgName, actors), nil
}

// SummarizeConversation generates a short title for the conversation
// by summarizing its current contents and persists the result.
func (s *Server) SummarizeConversation(ctx context.Context, req *aiv1.SummarizeConversationRequest) (*aiv1.Conversation, error) {
	orgName, pathUser, convName, err := parseConversationName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Conversation", req.GetName())
	}
	// Creator-only — same rationale as UpdateConversation.
	row, err := s.resolveConversation(ctx, orgName, pathUser, convName, "")
	if err != nil {
		return nil, err
	}

	// Hard short-circuit: never overwrite a user-set title.
	if row.TitleUserSet {
		actors, err := s.resolveConversationActors(ctx, []db.AiConversation{row})
		if err != nil {
			return nil, err
		}
		return convert.ConversationToProto(row, orgName, actors), nil
	}

	history, err := s.loadModelHistory(ctx, row.ID)
	if err != nil {
		slog.ErrorContext(ctx, "load history failed", "conversation_id", row.ID, "error", err)
		return nil, apierr.Internal("failed to load history")
	}
	transcript := renderTranscriptForSummary(history)
	if transcript == "" {
		actors, err := s.resolveConversationActors(ctx, []db.AiConversation{row})
		if err != nil {
			return nil, err
		}
		return convert.ConversationToProto(row, orgName, actors), nil
	}

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
		slog.ErrorContext(ctx, "summarize failed", "conversation_id", row.ID, "error", err)
		return nil, apierr.Internal("summarize failed")
	}
	title := sanitizeTitle(extractTextFromMessage(msg))
	if title == "" {
		actors, err := s.resolveConversationActors(ctx, []db.AiConversation{row})
		if err != nil {
			return nil, err
		}
		return convert.ConversationToProto(row, orgName, actors), nil
	}

	updated, err := s.queries.SetAutoTitle(ctx, db.SetAutoTitleParams{
		ID:    row.ID,
		Title: title,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Conversation", req.GetName())
	}
	// Best-effort enrichment after commit.
	actors, resolveErr := s.resolveConversationActors(ctx, []db.AiConversation{updated})
	if resolveErr != nil {
		slog.WarnContext(ctx, "summarize conversation: actor resolution failed; returning proto without audit actors",
			"conversation_id", updated.ID, "error", resolveErr)
		actors = nil
	}
	return convert.ConversationToProto(updated, orgName, actors), nil
}

const summarySystemPrompt = "You generate concise conversation titles. " +
	"Given a transcript, respond with a 3-6 word title that captures the topic. " +
	"Return only the title as plain text. " +
	"Do not use Markdown, asterisks, backticks, underscores, brackets, or any " +
	"other formatting characters. Do not wrap the title in quotes. Do not add " +
	"a leading label, preface, or trailing punctuation. Plain words only."

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

func sanitizeTitle(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	for _, ch := range []string{"**", "__", "`", "*", "_"} {
		s = strings.ReplaceAll(s, ch, "")
	}
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
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
	s = strings.TrimRightFunc(s, func(r rune) bool {
		switch r {
		case '.', ',', ':', ';', '!', '?':
			return true
		}
		return false
	})
	return strings.TrimSpace(s)
}

func trimSurroundingQuotes(s string) string {
	if len(s) < 2 {
		return s
	}
	pairs := [][2]rune{{'"', '"'}, {'\'', '\''}, {'“', '”'}, {'‘', '’'}}
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
	orgName, pathUser, convName, err := parseConversationName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Conversation", req.GetName())
	}
	existing, err := s.resolveConversation(ctx, orgName, pathUser, convName, permission.AiConversationsDeleteAll)
	if err != nil {
		return nil, err
	}
	if err := s.queries.DeleteConversation(ctx, existing.ID); err != nil {
		return nil, apierr.HandleResourceError(err, "Conversation", req.GetName())
	}
	return &emptypb.Empty{}, nil
}

// resolveConversation loads a conversation, enforcing path-bound
// ownership semantics:
//
//  1. Path's user-uuid must match the row's `creator_id` — a
//     fabricated path with a wrong user segment surfaces NotFound,
//     not a misleading "wrong owner" leak.
//  2. The caller's `pivox_user_id` claim must equal the path's
//     user-uuid OR the caller must hold `allPerm` (e.g.
//     `ai.conversations.readAll` or `deleteAll`). Otherwise
//     NotFound.
//
// Pass `allPerm = ""` to disable the audit-bypass entirely
// (creator-only operations like UpdateConversation /
// SummarizeConversation).
func (s *Server) resolveConversation(ctx context.Context, orgName string, pathUser uuid.UUID, convName, allPerm string) (db.AiConversation, error) {
	orgID, err := s.resolveOrg(ctx, orgName)
	if err != nil {
		return db.AiConversation{}, err
	}
	conv, err := s.queries.GetConversationByName(ctx, db.GetConversationByNameParams{
		OrgID: orgID,
		Name:  convName,
	})
	if err != nil {
		return db.AiConversation{}, apierr.HandleResourceError(err, "Conversation", buildConversationName(orgName, pathUser, convName))
	}
	if conv.CreatedBy != pathUser {
		// Path's user segment doesn't match the row's owner — surface
		// as NotFound so a malformed path can't probe ownership.
		return db.AiConversation{}, apierr.HandleResourceError(pgx.ErrNoRows, "Conversation", buildConversationName(orgName, pathUser, convName))
	}
	if err := s.verifyOwnerOrAllPerm(ctx, orgID, pathUser, allPerm); err != nil {
		return db.AiConversation{}, err
	}
	return conv, nil
}

// verifyOwnerOrAllPerm: the caller's `pivox_user_id` claim must
// equal the path-bound user-uuid, or the caller must hold `allPerm`
// (one of the `*All` audit perms). Errors are NotFound-shaped so a
// peer's existence isn't disclosed via the response code.
//
// `allPerm == ""` disables the audit bypass — the call must be the
// owner's own.
func (s *Server) verifyOwnerOrAllPerm(ctx context.Context, orgID, pathUser uuid.UUID, allPerm string) error {
	callerUserID := server.MustPivoxUserID(ctx)
	if callerUserID == pathUser {
		return nil
	}
	if allPerm == "" || s.resolver == nil {
		return apierr.NotFound("Conversation", "")
	}
	allowed, err := s.resolver.HasPermission(ctx, callerUserID, permission.OrgTarget(orgID), allPerm)
	if err != nil {
		slog.ErrorContext(ctx, "resolve all-perm failed", "permission", allPerm, "error", err)
		return apierr.Internal("resolve permission")
	}
	if !allowed {
		return apierr.NotFound("Conversation", "")
	}
	return nil
}

// resolveOrg resolves an org name to its UUID.
func (s *Server) resolveOrg(ctx context.Context, orgName string) (uuid.UUID, error) {
	org, err := s.queries.GetOrganizationByName(ctx, orgName)
	if err != nil {
		return uuid.Nil, apierr.HandleResourceError(err, "Organization", fmt.Sprintf("organizations/%s", orgName))
	}
	return org.ID, nil
}
