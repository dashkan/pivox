package organizations

import (
	"context"
	"log/slog"

	"cloud.google.com/go/longrunning/autogen/longrunningpb"
	iampb "github.com/dashkan/pivox/internal/pkg/gen/pivox/iam/v1"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/appkey"
	"github.com/dashkan/pivox/internal/authn"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/filter"
	"github.com/dashkan/pivox/internal/iam"
	"github.com/dashkan/pivox/internal/lro"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	"github.com/dashkan/pivox/internal/resource"
)

// AuthContextReader extracts the caller's Firebase UID from the
// request context. Injected so unit tests can stub the auth context
// without wiring the gRPC interceptor. Production wires this to
// `server.AuthenticatedUID` (set by AuthInterceptor).
type AuthContextReader func(ctx context.Context) (uid string, ok bool)

// TxBeginner abstracts transaction creation for testability.
// *pgxpool.Pool satisfies this interface.
type TxBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

type OrganizationsServer struct {
	apiv1.UnimplementedOrganizationsServer
	db      db.DBTX
	pool    TxBeginner
	queries db.Querier
	iam     *iam.Helper
	auth    authn.Service
	filter  *filter.ResourceFilter
	codec   *appkey.Codec
	readUID AuthContextReader
}

func NewOrganizationsServer(pool *pgxpool.Pool, queries db.Querier, iam *iam.Helper, auth authn.Service, codec *appkey.Codec, readUID AuthContextReader) *OrganizationsServer {
	return &OrganizationsServer{
		db:      pool,
		pool:    pool,
		queries: queries,
		iam:     iam,
		auth:    auth,
		filter:  filter.OrganizationFilter(),
		codec:   codec,
		readUID: readUID,
	}
}

func (s *OrganizationsServer) GetOrganization(ctx context.Context, req *apiv1.GetOrganizationRequest) (*apiv1.Organization, error) {
	segment, err := resource.ParseSegment(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Organization", req.GetName())
	}

	org, err := s.queries.GetOrganizationByName(ctx, segment)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Organization", req.GetName())
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
		Codec:       s.codec,
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
		nextPageToken, err = filter.EncodeNextPageToken(s.codec, results[pageSize].ID)
		if err != nil {
			return nil, apierr.Internal("encode page token")
		}
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

func (s *OrganizationsServer) CreateOrganization(ctx context.Context, req *apiv1.CreateOrganizationRequest) (*longrunningpb.Operation, error) {
	// Resolve caller → account row. The caller's Firebase UID comes
	// from the auth interceptor; we map it to a Pivox `accounts` row
	// so the new org can record both the immutable founder pointer
	// (`created_by_account_id`) and the per-org owner membership.
	uid, ok := s.readUID(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing authenticated caller")
	}
	caller, err := s.queries.GetAccountByFirebaseUID(ctx, uid)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Account", uid)
	}

	orgSlug := req.GetOrganizationId()
	if orgSlug == "" {
		orgSlug = uuid.New().String()[:8]
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "begin transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := db.New(tx)

	org, err := qtx.CreateOrganization(ctx, db.CreateOrganizationParams{
		ID:                 uuid.New(),
		Name:               orgSlug,
		DisplayName:        req.GetOrganization().GetDisplayName(),
		CreatedByAccountID: pgtype.UUID{Bytes: caller.ID, Valid: true},
		CreatedBy:          caller.ID.String(),
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Organization", orgSlug)
	}

	// Founder gets an `owner` membership row in the same transaction.
	// "≥1 owner per org" is preserved by definition for new orgs;
	// future role-mutation code enforces it at the boundary.
	if _, err := qtx.CreateUserMembership(ctx, db.CreateUserMembershipParams{
		ID:        uuid.New(),
		OrgID:     org.ID,
		AccountID: caller.ID,
		Role:      db.OrgRoleOwner,
	}); err != nil {
		return nil, status.Errorf(codes.Internal, "create owner membership: %v", err)
	}

	tenantID, err := s.auth.CreateTenant(ctx, orgSlug)
	if err != nil {
		slog.ErrorContext(ctx, "failed to create Firebase tenant", "org", orgSlug, "error", err)
		return nil, status.Errorf(codes.Internal, "create auth tenant: %v", err)
	}

	if err := qtx.SetOrganizationTenantID(ctx, db.SetOrganizationTenantIDParams{
		ID:       org.ID,
		TenantID: tenantID,
	}); err != nil {
		// Clean up the Firebase tenant we just created.
		if delErr := s.auth.DeleteTenant(ctx, tenantID); delErr != nil {
			slog.ErrorContext(ctx, "failed to clean up Firebase tenant", "tenantID", tenantID, "error", delErr)
		}
		return nil, status.Errorf(codes.Internal, "set tenant id: %v", err)
	}

	if err := tx.Commit(ctx); err != nil {
		// Clean up the Firebase tenant since the commit failed.
		if delErr := s.auth.DeleteTenant(ctx, tenantID); delErr != nil {
			slog.ErrorContext(ctx, "failed to clean up Firebase tenant after commit failure", "tenantID", tenantID, "error", delErr)
		}
		return nil, status.Errorf(codes.Internal, "commit transaction: %v", err)
	}

	org.TenantID = tenantID
	return lro.DoneOperation(convert.OrganizationToProto(org))
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
