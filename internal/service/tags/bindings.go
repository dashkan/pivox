package tags

import (
	"context"

	"cloud.google.com/go/longrunning/autogen/longrunningpb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/appkey"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/filter"
	"github.com/dashkan/pivox/internal/lro"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	"github.com/dashkan/pivox/internal/resource"
)

type TagBindingsServer struct {
	apiv1.UnimplementedTagBindingsServer
	db      db.DBTX
	queries db.Querier
	filter  *filter.ResourceFilter
	codec   *appkey.Codec
}

func NewTagBindingsServer(pool db.DBTX, queries db.Querier, codec *appkey.Codec) *TagBindingsServer {
	return &TagBindingsServer{
		db:      pool,
		queries: queries,
		filter:  filter.TagBindingFilter(),
		codec:   codec,
	}
}

func (s *TagBindingsServer) ListTagBindings(ctx context.Context, req *apiv1.ListTagBindingsRequest) (*apiv1.ListTagBindingsResponse, error) {
	rows, err := filter.Query(ctx, s.db, s.filter, filter.QueryParams{
		Filter:   req.GetFilter(),
		ParentID: req.GetParent(),
		OrderBy:  req.GetOrderBy(),
		PageSize: req.GetPageSize(),
		Cursor:   req.GetPageToken(),
		Codec:    s.codec,
	})
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("filter", err.Error()))
	}

	results, err := filter.ScanTagBindings(rows)
	if err != nil {
		return nil, apierr.Internal("database error")
	}

	pageSize := req.GetPageSize()
	if pageSize <= 0 {
		pageSize = 100
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

	tagBindings := make([]*apiv1.TagBinding, 0, len(results))
	for _, tb := range results {
		tv, err := s.queries.GetTagValue(ctx, tb.TagValueID)
		if err != nil {
			continue
		}
		tagBindings = append(tagBindings, convert.TagBindingToProto(tb, tv))
	}

	return &apiv1.ListTagBindingsResponse{
		TagBindings:   tagBindings,
		NextPageToken: nextPageToken,
	}, nil
}

func (s *TagBindingsServer) GetTagBinding(ctx context.Context, req *apiv1.GetTagBindingRequest) (*apiv1.TagBinding, error) {
	segment, err := resource.ParseSegment(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "TagBinding", req.GetName())
	}
	id, err := uuid.Parse(segment)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "TagBinding", req.GetName())
	}
	tb, err := s.queries.GetTagBinding(ctx, id)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "TagBinding", req.GetName())
	}
	tv, err := s.queries.GetTagValue(ctx, tb.TagValueID)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "TagValue", "")
	}
	return convert.TagBindingToProto(tb, tv), nil
}

func (s *TagBindingsServer) CreateTagBinding(ctx context.Context, req *apiv1.CreateTagBindingRequest) (*longrunningpb.Operation, error) {
	tb := req.GetTagBinding()

	// Parse tag value name: "tagKeys/{uuid}/tagValues/{uuid}"
	tvID, err := parseTagValueName(tb.GetTagValue())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "TagValue", tb.GetTagValue())
	}
	tagValue, err := s.queries.GetTagValue(ctx, tvID)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "TagValue", tb.GetTagValue())
	}

	created, err := s.queries.CreateTagBinding(ctx, db.CreateTagBindingParams{
		ID:             uuid.New(),
		ParentResource: req.GetParent(),
		TagValueID:     tagValue.ID,
		CreatedBy:      pgtype.UUID{},
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "TagBinding", "")
	}
	return lro.DoneOperation(convert.TagBindingToProto(created, tagValue))
}

func (s *TagBindingsServer) DeleteTagBinding(ctx context.Context, req *apiv1.DeleteTagBindingRequest) (*longrunningpb.Operation, error) {
	segment, err := resource.ParseSegment(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "TagBinding", req.GetName())
	}
	id, err := uuid.Parse(segment)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "TagBinding", req.GetName())
	}

	existing, err := s.queries.GetTagBinding(ctx, id)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "TagBinding", req.GetName())
	}
	if err := s.queries.DeleteTagBinding(ctx, existing.ID); err != nil {
		return nil, apierr.HandleResourceError(err, "TagBinding", req.GetName())
	}
	return lro.DoneOperation(&apiv1.TagBinding{Name: req.GetName()})
}

func (s *TagBindingsServer) ListEffectiveTags(ctx context.Context, req *apiv1.ListEffectiveTagsRequest) (*apiv1.ListEffectiveTagsResponse, error) {
	rows, err := s.queries.ListEffectiveTags(ctx, req.GetParent())
	if err != nil {
		return nil, apierr.Internal("database error")
	}

	effectiveTags := make([]*apiv1.EffectiveTag, 0, len(rows))
	for _, row := range rows {
		effectiveTags = append(effectiveTags, convert.EffectiveTagToProto(row))
	}

	return &apiv1.ListEffectiveTagsResponse{
		EffectiveTags: effectiveTags,
	}, nil
}
