package server

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	iampb "github.com/dashkan/pivox-server/internal/pkg/gen/pivox/iam/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/dashkan/pivox-server/internal/convert"
	db "github.com/dashkan/pivox-server/internal/db/generated"
	"github.com/dashkan/pivox-server/internal/filter"
	"github.com/dashkan/pivox-server/internal/firebase"
	"github.com/dashkan/pivox-server/internal/iam"
	apiv1 "github.com/dashkan/pivox-server/internal/pkg/gen/pivox/api/v1"
	"github.com/dashkan/pivox-server/internal/resource"
)

type OrganizationsServer struct {
	apiv1.UnimplementedOrganizationsServer
	db      db.DBTX
	pool    *pgxpool.Pool
	queries *db.Queries
	iam     *iam.Helper
	tenants *firebase.AuthService
	filter  *filter.ResourceFilter
}

func NewOrganizationsServer(pool *pgxpool.Pool, queries *db.Queries, iam *iam.Helper, tenants *firebase.AuthService) *OrganizationsServer {
	return &OrganizationsServer{
		db:      pool,
		pool:    pool,
		queries: queries,
		iam:     iam,
		tenants: tenants,
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

func (s *OrganizationsServer) CreateOrganization(ctx context.Context, req *apiv1.CreateOrganizationRequest) (*apiv1.Organization, error) {
	orgSlug := req.GetOrganizationId()
	if orgSlug == "" {
		orgSlug = uuid.New().String()[:8]
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "begin transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := s.queries.WithTx(tx)

	org, err := qtx.CreateOrganization(ctx, db.CreateOrganizationParams{
		ID:          uuid.New(),
		Name:        orgSlug,
		DisplayName: req.GetOrganization().GetDisplayName(),
		CreatedBy:   "",
	})
	if err != nil {
		return nil, handleResourceError(err, "Organization", orgSlug)
	}

	tenantID, err := s.tenants.CreateTenant(ctx, orgSlug)
	if err != nil {
		slog.ErrorContext(ctx, "failed to create Firebase tenant", "org", orgSlug, "error", err)
		return nil, status.Errorf(codes.Internal, "create auth tenant: %v", err)
	}

	if err := qtx.SetOrganizationTenantID(ctx, db.SetOrganizationTenantIDParams{
		ID:       org.ID,
		TenantID: tenantID,
	}); err != nil {
		// Clean up the Firebase tenant we just created.
		if delErr := s.tenants.DeleteTenant(ctx, tenantID); delErr != nil {
			slog.ErrorContext(ctx, "failed to clean up Firebase tenant", "tenantID", tenantID, "error", delErr)
		}
		return nil, status.Errorf(codes.Internal, "set tenant id: %v", err)
	}

	if err := tx.Commit(ctx); err != nil {
		// Clean up the Firebase tenant since the commit failed.
		if delErr := s.tenants.DeleteTenant(ctx, tenantID); delErr != nil {
			slog.ErrorContext(ctx, "failed to clean up Firebase tenant after commit failure", "tenantID", tenantID, "error", delErr)
		}
		return nil, status.Errorf(codes.Internal, "commit transaction: %v", err)
	}

	org.TenantID = tenantID
	return convert.OrganizationToProto(org), nil
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
