package organizations

import (
	"context"
	"errors"
	"log/slog"

	"cloud.google.com/go/longrunning/autogen/longrunningpb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/appkey"
	"github.com/dashkan/pivox/internal/authn"
	"github.com/dashkan/pivox/internal/convert"
	"github.com/dashkan/pivox/internal/crypto"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/filter"
	"github.com/dashkan/pivox/internal/lro"
	"github.com/dashkan/pivox/internal/permission"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	"github.com/dashkan/pivox/internal/resource"
	"github.com/dashkan/pivox/internal/server"
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
	db       db.DBTX
	pool     TxBeginner
	queries  db.Querier
	auth     authn.Service
	filter   *filter.ResourceFilter
	codec    *appkey.Codec
	readUID  AuthContextReader
	resolver *permission.Resolver
	caller   server.CallerIdentityResolver
	// lroManager drives the asynchronous orchestrators for
	// DeleteOrganization and UndeleteOrganization. Optional in
	// tests that don't exercise lifecycle paths.
	lroManager *lro.Manager
	// encryptor wraps Cloud KMS for column-level encryption of
	// SsoConfig.client_secret. Optional in tests that don't
	// exercise the SSO path.
	encryptor crypto.Encryptor
}

func NewOrganizationsServer(pool *pgxpool.Pool, queries db.Querier, auth authn.Service, codec *appkey.Codec, readUID AuthContextReader, resolver *permission.Resolver, caller server.CallerIdentityResolver, lroManager *lro.Manager, encryptor crypto.Encryptor) *OrganizationsServer {
	return &OrganizationsServer{
		db:         pool,
		pool:       pool,
		queries:    queries,
		auth:       auth,
		filter:     filter.OrganizationFilter(),
		codec:      codec,
		readUID:    readUID,
		resolver:   resolver,
		caller:     caller,
		lroManager: lroManager,
		encryptor:  encryptor,
	}
}

func (s *OrganizationsServer) GetOrganization(ctx context.Context, req *apiv1.GetOrganizationRequest) (*apiv1.Organization, error) {
	segment, err := resource.ParseSegment(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Organization", req.GetName())
	}

	// Use the org row resolved by the permission interceptor — its
	// gate is soft-delete-aware (uses GetOrganizationByNameForGate)
	// so the caller reaches us with a row that may be DELETE_REQUESTED.
	// Re-fetching via GetOrganizationByName would filter that row
	// out and surface NotFound, breaking the grace-window read path
	// the soft-delete-gate explicitly allows. Defensive slug check
	// mirrors the audit MED #4 fix for member handlers.
	resolved := server.MustResolvedOrgFromContext(ctx)
	if segment != resolved.Slug {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("name",
			"org slug in path does not match resolved scope"))
	}
	return convert.OrganizationToProto(resolved.Row), nil
}

// ListOrganizations is the post-signin "which orgs am I in?" query.
// Always caller-scoped: returns only orgs the authenticated user has
// a membership row for. Memberless callers (and freshly-Firebase-
// registered users whose firebase_identity row hasn't been synced yet)
// get an empty list, which the native client uses to detect the
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

	caller, err := s.queries.GetFirebaseIdentityByUID(ctx, uid)
	if err != nil {
		// No firebase_identity row yet (race with the sync-identity
		// webhook on a freshly-Firebase-registered user). Memberless
		// state — return an empty list so the client routes through
		// the org-creation bootstrap path.
		if errors.Is(err, pgx.ErrNoRows) {
			return &apiv1.ListOrganizationsResponse{}, nil
		}
		return nil, apierr.HandleResourceError(err, "FirebaseIdentity", uid)
	}

	rows, err := s.queries.ListOrganizationsForFirebaseIdentity(ctx, caller.ID)
	if err != nil {
		slog.ErrorContext(ctx, "list organizations failed", "firebase_identity_id", caller.ID, "error", err)
		return nil, apierr.Internal("list organizations")
	}

	orgs := make([]*apiv1.Organization, 0, len(rows))
	for _, o := range rows {
		orgs = append(orgs, convert.OrganizationToProto(o))
	}
	return &apiv1.ListOrganizationsResponse{Organizations: orgs}, nil
}

func (s *OrganizationsServer) CreateOrganization(ctx context.Context, req *apiv1.CreateOrganizationRequest) (*longrunningpb.Operation, error) {
	// Resolve caller → firebase_identity row. The caller's Firebase
	// UID comes from the auth interceptor; we map it to a Pivox
	// `firebase_identities` row so the new org can record both the
	// immutable founder pointer (`created_by_firebase_identity_id`)
	// and the per-org owner membership.
	uid, ok := s.readUID(ctx)
	if !ok {
		return nil, apierr.Unauthenticated("missing authenticated caller")
	}
	caller, err := s.queries.GetFirebaseIdentityByUID(ctx, uid)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "FirebaseIdentity", uid)
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
		ID:                          uuid.New(),
		Name:                        orgSlug,
		DisplayName:                 req.GetOrganization().GetDisplayName(),
		CreatedByFirebaseIdentityID: pgtype.UUID{Bytes: caller.ID, Valid: true},
		CreatedBy:                   caller.ID.String(),
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Organization", orgSlug)
	}

	// Founder gets a per-org user row in the same transaction.
	founder, err := qtx.CreateUserMembership(ctx, db.CreateUserMembershipParams{
		ID:                 uuid.New(),
		OrgID:              org.ID,
		FirebaseIdentityID: caller.ID,
	})
	if err != nil {
		slog.ErrorContext(ctx, "create founder user row failed", "org_id", org.ID, "firebase_identity_id", caller.ID, "error", err)
		return nil, apierr.Internal("create founder user row")
	}

	// Seed the 4 system roles for this org and bind the founder to
	// the owner role. Atomic with the org/user creates above — a
	// failure here rolls the whole bootstrap back, so no half-formed
	// org ever exists. "≥1 owner per org" is established by
	// definition for new orgs from this point forward.
	if err := bootstrapOrgRoles(ctx, qtx, org.ID, founder.ID, caller.ID.String()); err != nil {
		slog.ErrorContext(ctx, "bootstrap org roles failed", "org_id", org.ID, "error", err)
		return nil, apierr.Internal("bootstrap org roles")
	}

	if err := tx.Commit(ctx); err != nil {
		slog.ErrorContext(ctx, "commit transaction failed", "org_id", org.ID, "error", err)
		return nil, apierr.Internal("commit transaction")
	}

	return lro.DoneOperation(convert.OrganizationToProto(org))
}
