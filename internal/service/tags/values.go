package tags

import (
	"context"
	"fmt"
	"strings"

	"cloud.google.com/go/longrunning/autogen/longrunningpb"
	iampb "github.com/dashkan/pivox-server/internal/pkg/gen/pivox/iam/v1"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/dashkan/pivox-server/internal/apierr"
	"github.com/dashkan/pivox-server/internal/convert"
	db "github.com/dashkan/pivox-server/internal/db/generated"
	"github.com/dashkan/pivox-server/internal/filter"
	"github.com/dashkan/pivox-server/internal/iam"
	"github.com/dashkan/pivox-server/internal/lro"
	apiv1 "github.com/dashkan/pivox-server/internal/pkg/gen/pivox/api/v1"
)

type TagValuesServer struct {
	apiv1.UnimplementedTagValuesServer
	db      db.DBTX
	queries *db.Queries
	iam     *iam.Helper
	filter  *filter.ResourceFilter
}

func NewTagValuesServer(pool db.DBTX, queries *db.Queries, iam *iam.Helper) *TagValuesServer {
	return &TagValuesServer{
		db:      pool,
		queries: queries,
		iam:     iam,
		filter:  filter.TagValueFilter(),
	}
}

// parseTagKeyParent parses "tagKeys/{uuid}" and returns the tag key UUID.
func parseTagKeyParent(parent string) (uuid.UUID, error) {
	parts := strings.SplitN(parent, "/", 2)
	if len(parts) != 2 || parts[0] != "tagKeys" {
		return uuid.Nil, fmt.Errorf("invalid tag key parent %q: expected tagKeys/*", parent)
	}
	return uuid.Parse(parts[1])
}

// parseTagValueName parses "tagKeys/{uuid}/tagValues/{uuid}" and returns the tag value UUID.
func parseTagValueName(name string) (uuid.UUID, error) {
	parts := strings.Split(name, "/")
	if len(parts) != 4 || parts[0] != "tagKeys" || parts[2] != "tagValues" {
		return uuid.Nil, fmt.Errorf("invalid tag value name %q: expected tagKeys/*/tagValues/*", name)
	}
	return uuid.Parse(parts[3])
}

func (s *TagValuesServer) ListTagValues(ctx context.Context, req *apiv1.ListTagValuesRequest) (*apiv1.ListTagValuesResponse, error) {
	tagKeyID, err := parseTagKeyParent(req.GetParent())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "TagKey", req.GetParent())
	}
	// Verify the tag key exists.
	if _, err := s.queries.GetTagKey(ctx, tagKeyID); err != nil {
		return nil, apierr.HandleResourceError(err, "TagKey", req.GetParent())
	}

	rows, err := filter.Query(ctx, s.db, s.filter, filter.QueryParams{
		Filter:   req.GetFilter(),
		ParentID: tagKeyID.String(),
		OrderBy:  req.GetOrderBy(),
		PageSize: req.GetPageSize(),
		Cursor:   req.GetPageToken(),
	})
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid filter: %v", err)
	}

	results, err := filter.ScanTagValues(rows)
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

	tagValues := make([]*apiv1.TagValue, 0, len(results))
	for _, r := range results {
		tagValues = append(tagValues, convert.TagValueToProto(r))
	}

	return &apiv1.ListTagValuesResponse{
		TagValues:     tagValues,
		NextPageToken: nextPageToken,
	}, nil
}

func (s *TagValuesServer) GetTagValue(ctx context.Context, req *apiv1.GetTagValueRequest) (*apiv1.TagValue, error) {
	id, err := parseTagValueName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "TagValue", req.GetName())
	}
	tagValue, err := s.queries.GetTagValue(ctx, id)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "TagValue", req.GetName())
	}
	return convert.TagValueToProto(tagValue), nil
}

func (s *TagValuesServer) CreateTagValue(ctx context.Context, req *apiv1.CreateTagValueRequest) (*longrunningpb.Operation, error) {
	tagValue := req.GetTagValue()
	parent := req.GetParent()

	tagKeyID, err := parseTagKeyParent(parent)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "TagKey", parent)
	}

	parentKey, err := s.queries.GetTagKey(ctx, tagKeyID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, apierr.NotFound("TagKey", parent)
		}
		return nil, apierr.Internal("failed to get parent tag key")
	}

	tagValueID := req.GetTagValueId()
	if tagValueID == "" {
		tagValueID = uuid.New().String()
	}
	namespacedName := parentKey.NamespacedName + "/" + tagValueID

	result, err := s.queries.CreateTagValue(ctx, db.CreateTagValueParams{
		ID:             uuid.New(),
		TagKeyID:       parentKey.ID,
		ShortName:      tagValueID,
		NamespacedName: namespacedName,
		Description:    tagValue.GetDescription(),
		CreatedBy:      "",
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "TagValue", "")
	}

	return lro.DoneOperation(convert.TagValueToProto(result))
}

func (s *TagValuesServer) UpdateTagValue(ctx context.Context, req *apiv1.UpdateTagValueRequest) (*longrunningpb.Operation, error) {
	tagValue := req.GetTagValue()
	id, err := parseTagValueName(tagValue.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "TagValue", tagValue.GetName())
	}

	existing, err := s.queries.GetTagValue(ctx, id)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "TagValue", tagValue.GetName())
	}

	updateParams := db.UpdateTagValueParams{
		ID:        existing.ID,
		UpdatedBy: "",
	}

	mask := req.GetUpdateMask()
	if mask != nil {
		for _, path := range mask.GetPaths() {
			switch path {
			case "description":
				updateParams.Description = pgtype.Text{String: tagValue.GetDescription(), Valid: true}
			}
		}
	} else {
		updateParams.Description = pgtype.Text{String: tagValue.GetDescription(), Valid: true}
	}

	result, err := s.queries.UpdateTagValue(ctx, updateParams)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "TagValue", tagValue.GetName())
	}

	return lro.DoneOperation(convert.TagValueToProto(result))
}

func (s *TagValuesServer) DeleteTagValue(ctx context.Context, req *apiv1.DeleteTagValueRequest) (*longrunningpb.Operation, error) {
	id, err := parseTagValueName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "TagValue", req.GetName())
	}

	existing, err := s.queries.GetTagValue(ctx, id)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "TagValue", req.GetName())
	}

	bindingCount, err := s.queries.CountTagBindingsByTagValue(ctx, existing.ID)
	if err != nil {
		return nil, apierr.Internal("failed to check tag bindings")
	}
	if bindingCount > 0 {
		return nil, apierr.FailedPrecondition("cannot delete tag value with existing tag bindings")
	}

	err = s.queries.DeleteTagValue(ctx, existing.ID)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "TagValue", req.GetName())
	}

	return lro.DoneOperation(&apiv1.TagValue{})
}

func (s *TagValuesServer) GetIamPolicy(ctx context.Context, req *iampb.GetIamPolicyRequest) (*iampb.Policy, error) {
	return s.iam.GetIamPolicy(ctx, req)
}

func (s *TagValuesServer) SetIamPolicy(ctx context.Context, req *iampb.SetIamPolicyRequest) (*iampb.Policy, error) {
	return s.iam.SetIamPolicy(ctx, req)
}

func (s *TagValuesServer) TestIamPermissions(ctx context.Context, req *iampb.TestIamPermissionsRequest) (*iampb.TestIamPermissionsResponse, error) {
	return s.iam.TestIamPermissions(ctx, req)
}
