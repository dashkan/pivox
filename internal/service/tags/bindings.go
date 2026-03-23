package tags

import (
	"context"

	"cloud.google.com/go/longrunning/autogen/longrunningpb"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/dashkan/pivox-server/internal/apierr"
	"github.com/dashkan/pivox-server/internal/convert"
	db "github.com/dashkan/pivox-server/internal/db/generated"
	"github.com/dashkan/pivox-server/internal/filter"
	"github.com/dashkan/pivox-server/internal/lro"
	apiv1 "github.com/dashkan/pivox-server/internal/pkg/gen/pivox/api/v1"
	"github.com/dashkan/pivox-server/internal/resource"
)

type TagBindingsServer struct {
	apiv1.UnimplementedTagBindingsServer
	db      db.DBTX
	queries *db.Queries
	filter  *filter.ResourceFilter
}

func NewTagBindingsServer(pool db.DBTX, queries *db.Queries) *TagBindingsServer {
	return &TagBindingsServer{
		db:      pool,
		queries: queries,
		filter:  filter.TagBindingFilter(),
	}
}

func (s *TagBindingsServer) ListTagBindings(ctx context.Context, req *apiv1.ListTagBindingsRequest) (*apiv1.ListTagBindingsResponse, error) {
	rows, err := filter.Query(ctx, s.db, s.filter, filter.QueryParams{
		Filter:   req.GetFilter(),
		ParentID: req.GetParent(),
		OrderBy:  req.GetOrderBy(),
		PageSize: req.GetPageSize(),
		Cursor:   req.GetPageToken(),
	})
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid filter: %v", err)
	}

	results, err := filter.ScanTagBindings(rows)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "database error")
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
		nextPageToken = results[pageSize].ID.String()
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
		CreatedBy:      "",
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
		return nil, status.Errorf(codes.Internal, "database error")
	}

	effectiveTags := make([]*apiv1.EffectiveTag, 0, len(rows))
	for _, row := range rows {
		effectiveTags = append(effectiveTags, convert.EffectiveTagToProto(row))
	}

	return &apiv1.ListEffectiveTagsResponse{
		EffectiveTags: effectiveTags,
	}, nil
}
