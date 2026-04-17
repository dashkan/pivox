package aichat

import (
	"context"
	"fmt"

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
)

func (s *Server) GetConversation(ctx context.Context, req *aiv1.GetConversationRequest) (*aiv1.Conversation, error) {
	orgName, convName, err := parseConversationName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Conversation", req.GetName())
	}

	orgID, err := s.resolveOrg(ctx, orgName)
	if err != nil {
		return nil, err
	}

	row, err := s.queries.GetConversationByName(ctx, db.GetConversationByNameParams{
		OrgID: orgID,
		Name:  convName,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Conversation", req.GetName())
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

	rows, err := filter.Query(ctx, s.db, s.filter, filter.QueryParams{
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
		return nil, apierr.Internal("database error")
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

	params := db.UpdateConversationParams{
		ID:        existing.ID,
		UpdatedBy: uid,
	}

	mask := req.GetUpdateMask()
	if mask != nil {
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
			}
		}
	} else {
		params.Title = pgtype.Text{String: conv.GetTitle(), Valid: true}
		params.Description = pgtype.Text{String: conv.GetDescription(), Valid: true}
		params.Archived = pgtype.Bool{Bool: conv.GetArchived(), Valid: true}
		params.Pinned = pgtype.Bool{Bool: conv.GetPinned(), Valid: true}
	}

	row, err := s.queries.UpdateConversation(ctx, params)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Conversation", conv.GetName())
	}

	return convert.ConversationToProto(row, orgName), nil
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
