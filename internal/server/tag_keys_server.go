package server

import (
	"context"

	iampb "github.com/pivoxai/pivox/internal/pkg/gen/pivox/iam/v1"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/pivoxai/pivox/internal/apierr"
	"github.com/pivoxai/pivox/internal/convert"
	db "github.com/pivoxai/pivox/internal/db/generated"
	"github.com/pivoxai/pivox/internal/filter"
	"github.com/pivoxai/pivox/internal/iam"
	apiv1 "github.com/pivoxai/pivox/internal/pkg/gen/pivox/api/v1"
	"github.com/pivoxai/pivox/internal/resource"
)

type TagKeysServer struct {
	apiv1.UnimplementedTagKeysServer
	db      db.DBTX
	queries *db.Queries
	iam     *iam.Helper
	filter  *filter.ResourceFilter
}

func NewTagKeysServer(pool db.DBTX, queries *db.Queries, iam *iam.Helper) *TagKeysServer {
	return &TagKeysServer{
		db:      pool,
		queries: queries,
		iam:     iam,
		filter:  filter.TagKeyFilter(),
	}
}

func (s *TagKeysServer) ListTagKeys(ctx context.Context, req *apiv1.ListTagKeysRequest) (*apiv1.ListTagKeysResponse, error) {
	orgID, err := resource.ResolveOrgParent(ctx, s.queries, req.GetParent())
	if err != nil {
		return nil, err
	}

	rows, err := filter.Query(ctx, s.db, s.filter, filter.QueryParams{
		Filter:   req.GetFilter(),
		ParentID: orgID.String(),
		OrderBy:  req.GetOrderBy(),
		PageSize: req.GetPageSize(),
		Cursor:   req.GetPageToken(),
	})
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid filter: %v", err)
	}

	results, err := filter.ScanTagKeys(rows)
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

	tagKeys := make([]*apiv1.TagKey, 0, len(results))
	for _, r := range results {
		tagKeys = append(tagKeys, convert.TagKeyToProto(r))
	}

	return &apiv1.ListTagKeysResponse{
		TagKeys:       tagKeys,
		NextPageToken: nextPageToken,
	}, nil
}

func (s *TagKeysServer) GetTagKey(ctx context.Context, req *apiv1.GetTagKeyRequest) (*apiv1.TagKey, error) {
	segment, err := resource.ParseSegment(req.GetName())
	if err != nil {
		return nil, handleResourceError(err, "TagKey", req.GetName())
	}
	id, err := uuid.Parse(segment)
	if err != nil {
		return nil, handleResourceError(err, "TagKey", req.GetName())
	}
	tagKey, err := s.queries.GetTagKey(ctx, id)
	if err != nil {
		return nil, handleResourceError(err, "TagKey", req.GetName())
	}
	return convert.TagKeyToProto(tagKey), nil
}

func (s *TagKeysServer) CreateTagKey(ctx context.Context, req *apiv1.CreateTagKeyRequest) (*apiv1.TagKey, error) {
	tagKey := req.GetTagKey()

	orgID, err := resource.ResolveOrgParent(ctx, s.queries, req.GetParent())
	if err != nil {
		return nil, err
	}

	tagKeyID := req.GetTagKeyId()
	if tagKeyID == "" {
		tagKeyID = uuid.New().String()
	}

	result, err := s.queries.CreateTagKey(ctx, db.CreateTagKeyParams{
		ID:             uuid.New(),
		OrgID:          orgID,
		ShortName:      tagKeyID,
		NamespacedName: orgID.String() + "/" + tagKeyID,
		Description:    tagKey.GetDescription(),
		CreatedBy:      "",
	})
	if err != nil {
		return nil, handleResourceError(err, "TagKey", "")
	}

	return convert.TagKeyToProto(result), nil
}

func (s *TagKeysServer) UpdateTagKey(ctx context.Context, req *apiv1.UpdateTagKeyRequest) (*apiv1.TagKey, error) {
	tagKey := req.GetTagKey()
	segment, err := resource.ParseSegment(tagKey.GetName())
	if err != nil {
		return nil, handleResourceError(err, "TagKey", tagKey.GetName())
	}
	id, err := uuid.Parse(segment)
	if err != nil {
		return nil, handleResourceError(err, "TagKey", tagKey.GetName())
	}

	existing, err := s.queries.GetTagKey(ctx, id)
	if err != nil {
		return nil, handleResourceError(err, "TagKey", tagKey.GetName())
	}

	updateParams := db.UpdateTagKeyParams{
		ID:        existing.ID,
		UpdatedBy: "",
	}

	mask := req.GetUpdateMask()
	if mask != nil {
		for _, path := range mask.GetPaths() {
			switch path {
			case "description":
				updateParams.Description = pgtype.Text{String: tagKey.GetDescription(), Valid: true}
			}
		}
	} else {
		updateParams.Description = pgtype.Text{String: tagKey.GetDescription(), Valid: true}
	}

	result, err := s.queries.UpdateTagKey(ctx, updateParams)
	if err != nil {
		return nil, handleResourceError(err, "TagKey", tagKey.GetName())
	}

	return convert.TagKeyToProto(result), nil
}

func (s *TagKeysServer) DeleteTagKey(ctx context.Context, req *apiv1.DeleteTagKeyRequest) (*apiv1.TagKey, error) {
	segment, err := resource.ParseSegment(req.GetName())
	if err != nil {
		return nil, handleResourceError(err, "TagKey", req.GetName())
	}
	id, err := uuid.Parse(segment)
	if err != nil {
		return nil, handleResourceError(err, "TagKey", req.GetName())
	}

	existing, err := s.queries.GetTagKey(ctx, id)
	if err != nil {
		return nil, handleResourceError(err, "TagKey", req.GetName())
	}

	count, err := s.queries.CountTagValuesByTagKey(ctx, existing.ID)
	if err != nil {
		return nil, apierr.Internal("failed to check tag values")
	}
	if count > 0 {
		return nil, apierr.FailedPrecondition("cannot delete tag key with existing tag values")
	}

	err = s.queries.DeleteTagKey(ctx, existing.ID)
	if err != nil {
		return nil, handleResourceError(err, "TagKey", req.GetName())
	}

	return &apiv1.TagKey{}, nil
}

func (s *TagKeysServer) GetIamPolicy(ctx context.Context, req *iampb.GetIamPolicyRequest) (*iampb.Policy, error) {
	return s.iam.GetIamPolicy(ctx, req)
}

func (s *TagKeysServer) SetIamPolicy(ctx context.Context, req *iampb.SetIamPolicyRequest) (*iampb.Policy, error) {
	return s.iam.SetIamPolicy(ctx, req)
}

func (s *TagKeysServer) TestIamPermissions(ctx context.Context, req *iampb.TestIamPermissionsRequest) (*iampb.TestIamPermissionsResponse, error) {
	return s.iam.TestIamPermissions(ctx, req)
}
