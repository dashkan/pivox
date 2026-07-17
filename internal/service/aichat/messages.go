package aichat

import (
	"context"
	"time"

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

	pageSize := filter.ClampPageSize(s.messageFilter, req.GetPageSize())

	// Resolve order_by against the sortable whitelist. With no client order_by
	// the resource's DefaultOrder ("id desc") applies — newest-first. The plan
	// also tells the cursor codec whether the sort value is a timestamp.
	plan, err := filter.PlanOrderBy(s.messageFilter, req.GetOrderBy())
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("order_by", err.Error()))
	}
	cursor, err := filter.DecodeCursor(s.codec, plan, req.GetPageToken())
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("page_token", "invalid or malformed"))
	}

	// Base scope: messages under the caller-owned conversation (ownership already
	// verified via resolveConversation). Every value is bound as a $N parameter
	// by BuildListQuery — nothing is string-interpolated.
	sql, args, err := filter.BuildListQuery(filter.ListQuery{
		Resource: s.messageFilter,
		Base:     []filter.Predicate{{SQL: "conversation_id = %s", Arg: conv.ID}},
		Filter:   req.GetFilter(),
		Order:    plan,
		PageSize: pageSize,
		Cursor:   cursor,
	})
	if err != nil {
		// The only error source is the filter transpiler (bad user filter).
		return nil, apierr.InvalidArgument(apierr.FieldViolation("filter", err.Error()))
	}

	pgxRows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, apierr.Internal(err, "list messages")
	}
	results, err := filter.ScanMessages(pgxRows)
	if err != nil {
		return nil, apierr.Internal(err, "database error")
	}

	// filter.Paginate trims the over-fetched result to pageSize and derives the
	// next-page token from the LAST RETURNED row via the compound cursor —
	// encoding (sortValue, id) so the resume predicate matches the ORDER BY.
	results, nextPageToken, err := filter.Paginate(results, int(pageSize), func(last db.AiMessage) (string, error) {
		return filter.EncodeCursor(s.codec, plan, messageSortValue(plan, last), last.ID)
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

// messageSortValue renders the active order_by column's value for the given row
// as the string the compound page token carries. Timestamps use RFC3339Nano so
// filter.DecodeCursor can parse them back to an exact time.Time. For the id-only
// ordering (plan.Field == "", incl. the "id desc" default) the value is unused,
// so "" is returned.
func messageSortValue(plan filter.OrderByPlan, r db.AiMessage) string {
	switch plan.Field {
	case "createTime":
		return r.CreateTime.UTC().Format(time.RFC3339Nano)
	default:
		return ""
	}
}
