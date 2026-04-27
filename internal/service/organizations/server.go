package organizations

import (
	"context"
	"errors"
	"log/slog"

	"cloud.google.com/go/longrunning/autogen/longrunningpb"
	iampb "github.com/dashkan/pivox/internal/pkg/gen/pivox/iam/v1"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

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

// ListOrganizations is the post-signin "which orgs am I in?" query.
// Always caller-scoped: returns only orgs the authenticated user has
// a membership row for. Memberless callers (and freshly-Firebase-
// registered users whose account row hasn't been synced yet) get an
// empty list, which the native client uses to detect the
// zero-membership state and route to the org-creation screen.
//
// `page_size`, `page_token`, `filter`, `order_by`, `show_deleted`
// from the request are intentionally ignored — typical users are in
// 1-3 orgs and we always return them all (capped at 1000 in the
// underlying query as a defensive backstop). If a real user ever hits
// that ceiling, something else is wrong.
func (s *OrganizationsServer) ListOrganizations(ctx context.Context, req *apiv1.ListOrganizationsRequest) (*apiv1.ListOrganizationsResponse, error) {
	_ = req // request fields intentionally unused; see method comment
	uid, ok := s.readUID(ctx)
	if !ok {
		return nil, apierr.Unauthenticated("missing authenticated caller")
	}

	caller, err := s.queries.GetAccountByFirebaseUID(ctx, uid)
	if err != nil {
		// No account row yet (race with the /internal/sync-account
		// webhook on a freshly-Firebase-registered user). Memberless
		// state — return an empty list so the client routes through
		// the org-creation bootstrap path.
		if errors.Is(err, pgx.ErrNoRows) {
			return &apiv1.ListOrganizationsResponse{}, nil
		}
		return nil, apierr.HandleResourceError(err, "Account", uid)
	}

	rows, err := s.queries.ListOrganizationsForAccount(ctx, caller.ID)
	if err != nil {
		slog.ErrorContext(ctx, "list organizations failed", "account_id", caller.ID, "error", err)
		return nil, apierr.Internal("list organizations")
	}

	orgs := make([]*apiv1.Organization, 0, len(rows))
	for _, o := range rows {
		orgs = append(orgs, convert.OrganizationToProto(o))
	}
	return &apiv1.ListOrganizationsResponse{Organizations: orgs}, nil
}

func (s *OrganizationsServer) CreateOrganization(ctx context.Context, req *apiv1.CreateOrganizationRequest) (*longrunningpb.Operation, error) {
	// Resolve caller → account row. The caller's Firebase UID comes
	// from the auth interceptor; we map it to a Pivox `accounts` row
	// so the new org can record both the immutable founder pointer
	// (`created_by_account_id`) and the per-org owner membership.
	uid, ok := s.readUID(ctx)
	if !ok {
		return nil, apierr.Unauthenticated("missing authenticated caller")
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
		slog.ErrorContext(ctx, "begin transaction failed", "error", err)
		return nil, apierr.Internal("begin transaction")
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
		slog.ErrorContext(ctx, "create owner membership failed", "org_id", org.ID, "account_id", caller.ID, "error", err)
		return nil, apierr.Internal("create owner membership")
	}

	if err := tx.Commit(ctx); err != nil {
		slog.ErrorContext(ctx, "commit transaction failed", "org_id", org.ID, "error", err)
		return nil, apierr.Internal("commit transaction")
	}

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
