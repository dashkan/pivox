package aichat

import (
	"context"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/filter"
	"github.com/dashkan/pivox/internal/permission"
	aiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/ai/v1"
)

func (s *Server) GetMessage(ctx context.Context, req *aiv1.GetMessageRequest) (*aiv1.Message, error) {
	orgName, pathUser, convName, msgName, err := parseMessageName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Message", req.GetName())
	}

	conv, err := s.resolveConversation(ctx, orgName, pathUser, convName, permission.AiConversationsReadAll)
	if err != nil {
		return nil, err
	}

	row, err := s.queries.GetMessageByName(ctx, db.GetMessageByNameParams{
		ConversationID: conv.ID,
		Name:           msgName,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Message", req.GetName())
	}

	convFullName := buildConversationName(orgName, pathUser, convName)
	pb, err := convert.MessageToProto(row, convFullName)
	if err != nil {
		return nil, apierr.Internal(err, "failed to convert message")
	}
	return pb, nil
}

func (s *Server) ListMessages(ctx context.Context, req *aiv1.ListMessagesRequest) (*aiv1.ListMessagesResponse, error) {
	orgName, pathUser, convName, err := parseMessageParent(req.GetParent())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Conversation", req.GetParent())
	}

	conv, err := s.resolveConversation(ctx, orgName, pathUser, convName, permission.AiConversationsReadAll)
	if err != nil {
		return nil, err
	}

	rows, err := filter.Query(ctx, s.pool, s.messageFilter, filter.QueryParams{
		Filter:   req.GetFilter(),
		ParentID: conv.ID.String(),
		OrderBy:  req.GetOrderBy(),
		PageSize: req.GetPageSize(),
		Cursor:   req.GetPageToken(),
		Codec:    s.codec,
	})
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("filter", err.Error()))
	}

	results, err := filter.ScanMessages(rows)
	if err != nil {
		return nil, apierr.Internal(err, "database error")
	}

	pageSize := req.GetPageSize()
	if pageSize <= 0 {
		pageSize = 100
	}
	if pageSize > 1000 {
		pageSize = 1000
	}

	results, nextPageToken, err := filter.Paginate(results, int(pageSize), func(last db.AiMessage) (string, error) {
		return filter.EncodeNextPageToken(s.codec, last.ID)
	})
	if err != nil {
		return nil, apierr.Internal(err, "encode page token")
	}

	convFullName := buildConversationName(orgName, pathUser, convName)
	msgs := make([]*aiv1.Message, 0, len(results))
	for _, r := range results {
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
