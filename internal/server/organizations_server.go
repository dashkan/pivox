package server

import (
	"context"

	iampb "github.com/pivoxai/pivox/internal/pkg/gen/pivox/iam/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/pivoxai/pivox/internal/convert"
	db "github.com/pivoxai/pivox/internal/db/generated"
	"github.com/pivoxai/pivox/internal/filter"
	"github.com/pivoxai/pivox/internal/iam"
	apiv1 "github.com/pivoxai/pivox/internal/pkg/gen/pivox/api/v1"
	"github.com/pivoxai/pivox/internal/resource"
)

type OrganizationsServer struct {
	apiv1.UnimplementedOrganizationsServer
	db      db.DBTX
	queries *db.Queries
	iam     *iam.Helper
	filter  *filter.ResourceFilter
}

func NewOrganizationsServer(pool db.DBTX, queries *db.Queries, iam *iam.Helper) *OrganizationsServer {
	return &OrganizationsServer{
		db:      pool,
		queries: queries,
		iam:     iam,
		filter:  filter.OrganizationFilter(),
	}
}

func (s *OrganizationsServer) GetOrganization(ctx context.Context, req *apiv1.GetOrganizationRequest) (*apiv1.Organization, error) {
	segment, err := resource.ParseSegment(req.GetName())
	if err != nil {
		return nil, handleResourceError(err, "Organization", req.GetName())
	}

	org, err := s.queries.GetOrganizationByName(ctx, segment)
	if err != nil {
		return nil, handleResourceError(err, "Organization", req.GetName())
	}

	return convert.OrganizationToProto(org), nil
}

func (s *OrganizationsServer) ListOrganizations(ctx context.Context, req *apiv1.ListOrganizationsRequest) (*apiv1.ListOrganizationsResponse, error) {
	rows, err := filter.Query(ctx, s.db, s.filter, filter.QueryParams{
		Filter:      req.GetFilter(),
		OrderBy:     req.GetOrderBy(),
		PageSize:    req.GetPageSize(),
		Cursor:      req.GetPageToken(),
		ShowDeleted: req.GetShowDeleted(),
	})
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid filter: %v", err)
	}

	results, err := filter.ScanOrganizations(rows)
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

	orgs := make([]*apiv1.Organization, 0, len(results))
	for _, o := range results {
		orgs = append(orgs, convert.OrganizationToProto(o))
	}

	return &apiv1.ListOrganizationsResponse{
		Organizations: orgs,
		NextPageToken: nextPageToken,
	}, nil
}

func (s *OrganizationsServer) GetIamPolicy(ctx context.Context, req *iampb.GetIamPolicyRequest) (*iampb.Policy, error) {
	return s.iam.GetIamPolicy(ctx, req)
}

func (s *OrganizationsServer) SetIamPolicy(ctx context.Context, req *iampb.SetIamPolicyRequest) (*iampb.Policy, error) {
	return s.iam.SetIamPolicy(ctx, req)
}

func (s *OrganizationsServer) TestIamPermissions(ctx context.Context, req *iampb.TestIamPermissionsRequest) (*iampb.TestIamPermissionsResponse, error) {
	return s.iam.TestIamPermissions(ctx, req)
}
