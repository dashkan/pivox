package aichat

import (
	"context"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	aiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/ai/v1"
)

func (s *Server) GetMessage(ctx context.Context, req *aiv1.GetMessageRequest) (*aiv1.Message, error) {
	orgName, convName, msgName, err := parseMessageName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Message", req.GetName())
	}

	orgID, err := s.resolveOrg(ctx, orgName)
	if err != nil {
		return nil, err
	}

	conv, err := s.queries.GetConversationByName(ctx, db.GetConversationByNameParams{
		OrgID: orgID,
		Name:  convName,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Conversation", buildConversationName(orgName, convName))
	}

	row, err := s.queries.GetMessageByName(ctx, db.GetMessageByNameParams{
		ConversationID: conv.ID,
		Name:           msgName,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Message", req.GetName())
	}

	convFullName := buildConversationName(orgName, convName)
	pb, err := convert.MessageToProto(row, convFullName)
	if err != nil {
		return nil, apierr.Internal("failed to convert message")
	}
	return pb, nil
}

func (s *Server) ListMessages(ctx context.Context, req *aiv1.ListMessagesRequest) (*aiv1.ListMessagesResponse, error) {
	orgName, convName, err := parseMessageParent(req.GetParent())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Conversation", req.GetParent())
	}

	orgID, err := s.resolveOrg(ctx, orgName)
	if err != nil {
		return nil, err
	}

	conv, err := s.queries.GetConversationByName(ctx, db.GetConversationByNameParams{
		OrgID: orgID,
		Name:  convName,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Conversation", req.GetParent())
	}

	pageSize := req.GetPageSize()
	if pageSize <= 0 {
		pageSize = 100
	}
	if pageSize > 1000 {
		pageSize = 1000
	}

	rows, err := s.queries.ListMessagesByConversation(ctx, db.ListMessagesByConversationParams{
		ConversationID: conv.ID,
		Limit:          pageSize + 1,
		Offset:         0,
	})
	if err != nil {
		return nil, apierr.Internal("database error")
	}

	var nextPageToken string
	if int32(len(rows)) > pageSize {
		nextPageToken = rows[pageSize].ID.String()
		rows = rows[:pageSize]
	}

	convFullName := buildConversationName(orgName, convName)
	msgs := make([]*aiv1.Message, 0, len(rows))
	for _, r := range rows {
		pb, err := convert.MessageToProto(r, convFullName)
		if err != nil {
			s.logger.Warn("failed to convert message", "message", r.Name, "error", err)
			continue
		}
		msgs = append(msgs, pb)
	}

	return &aiv1.ListMessagesResponse{
		Messages:      msgs,
		NextPageToken: nextPageToken,
	}, nil
}
