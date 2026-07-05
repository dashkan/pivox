package organizations

import (
	"context"
	"log/slog"

	"cloud.google.com/go/longrunning/autogen/longrunningpb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/appkey"
	"github.com/dashkan/pivox/internal/audit"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/filter"
	"github.com/dashkan/pivox/internal/lro"
	"github.com/dashkan/pivox/internal/permission"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	typespb "github.com/dashkan/pivox/internal/pkg/gen/pivox/types"
	"github.com/dashkan/pivox/internal/resource"
	"github.com/dashkan/pivox/internal/server"
)

type OrganizationsServer struct {
	apiv1.UnimplementedOrganizationsServer
	pool     db.RWPool
	queries  db.Querier
	filter   *filter.ResourceFilter
	codec    *appkey.Codec
	resolver *permission.Resolver
	// audit inflates created_by/updated_by/deleted_by UUIDs into
	// Actor protos. Optional in tests that don't assert on audit
	// output (the converters tolerate a nil map).
	audit *audit.Resolver
	// lroManager drives the asynchronous orchestrators for
	// DeleteOrganization and UndeleteOrganization. Optional in
	// tests that don't exercise lifecycle paths.
	lroManager *lro.Manager
}

// Config is the constructor input for OrganizationsServer.
type Config struct {
	// Pool is the database pool, used both as a db.DBTX for filter
	// reads and as a tx beginner for write paths. Required.
	Pool *pgxpool.Pool
	// Queries is the sqlc query interface. Required.
	Queries db.Querier
	// Codec opaque-encodes resource names. Required.
	Codec *appkey.Codec
	// Resolver gates per-resource permission checks. Optional;
	// nil is acceptable in unit tests that don't exercise the
	// permission paths.
	Resolver *permission.Resolver
	// AuditResolver inflates audit-field UUIDs into Actor protos.
	// Optional; nil leaves Actor fields unset.
	AuditResolver *audit.Resolver
	// LROManager drives the asynchronous orchestrators for
	// DeleteOrganization / UndeleteOrganization. Optional in
	// tests that don't exercise lifecycle paths.
	LROManager *lro.Manager
}

// NewOrganizationsServer constructs the server from cfg. Panics on a
// missing required field — a startup-time programmer error rather
// than a runtime failure.
func NewOrganizationsServer(cfg Config) *OrganizationsServer {
	if cfg.Pool == nil {
		panic("organizations: Config.Pool is required")
	}
	if cfg.Queries == nil {
		panic("organizations: Config.Queries is required")
	}
	if cfg.Codec == nil {
		panic("organizations: Config.Codec is required")
	}
	return &OrganizationsServer{
		pool:       cfg.Pool,
		queries:    cfg.Queries,
		filter:     filter.OrganizationFilter(),
		codec:      cfg.Codec,
		resolver:   cfg.Resolver,
		audit:      cfg.AuditResolver,
		lroManager: cfg.LROManager,
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
	actors, err := s.resolveOrgActors(ctx, []db.Organization{resolved.Row})
	if err != nil {
		return nil, err
	}
	return convert.OrganizationToProto(resolved.Row, actors), nil
}

// resolveOrgActors resolves the union of created_by/updated_by/
// deleted_by UUIDs across the page into a single Actor map. Returns
// nil when no audit resolver is wired (tests, partial responses).
func (s *OrganizationsServer) resolveOrgActors(ctx context.Context, orgs []db.Organization) (map[uuid.UUID]*typespb.Actor, error) {
	if s.audit == nil {
		return nil, nil
	}
	ids := make([]uuid.UUID, 0, len(orgs)*3)
	for _, o := range orgs {
		if o.CreatedBy.Valid {
			ids = append(ids, o.CreatedBy.Bytes)
		}
		if o.UpdatedBy.Valid {
			ids = append(ids, o.UpdatedBy.Bytes)
		}
		if o.DeletedBy.Valid {
			ids = append(ids, o.DeletedBy.Bytes)
		}
	}
	actors, err := s.audit.Resolve(ctx, ids)
	if err != nil {
		slog.ErrorContext(ctx, "resolve org actors failed", "error", err)
		return nil, apierr.Internal("resolve actors")
	}
	return actors, nil
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
	identityID := server.MustUserID(ctx)

	rows, err := s.queries.ListOrganizationsForIdentity(ctx, convert.PgUUID(identityID))
	if err != nil {
		slog.ErrorContext(ctx, "list organizations failed", "identity_id", identityID, "error", err)
		return nil, apierr.Internal("list organizations")
	}

	actors, err := s.resolveOrgActors(ctx, rows)
	if err != nil {
		return nil, err
	}
	orgs := make([]*apiv1.Organization, 0, len(rows))
	for _, o := range rows {
		orgs = append(orgs, convert.OrganizationToProto(o, actors))
	}
	return &apiv1.ListOrganizationsResponse{Organizations: orgs}, nil
}

func (s *OrganizationsServer) CreateOrganization(ctx context.Context, req *apiv1.CreateOrganizationRequest) (*longrunningpb.Operation, error) {
	// Caller identity UUID — the universal user UUID post-Phase-7.
	// AuthInterceptor extracted it from the verified token's
	// `sub` (== identities.id). Used as both the immutable founder
	// pointer (`created_by_identity_id`) and the principal of the
	// owner membership seeded below.
	callerID := server.MustUserID(ctx)

	// organization_id is required at the wire boundary —
	// protovalidate enforces ^[a-z][a-z0-9-]{3,19}$ which rejects
	// the empty string. Handler-side auto-generation is therefore
	// unreachable; clients always supply a slug.
	orgSlug := req.GetOrganizationId()

	var err error
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		slog.ErrorContext(ctx, "begin transaction failed", "error", err)
		return nil, apierr.Internal("begin transaction")
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := db.New(tx)

	org, err := qtx.CreateOrganization(ctx, db.CreateOrganizationParams{
		ID:          uuid.New(),
		Name:        orgSlug,
		DisplayName: req.GetOrganization().GetDisplayName(),
		CreatedBy:   convert.PgUUID(callerID),
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Organization", orgSlug)
	}

	// Seed the 4 system roles for this org and bind the founder
	// (caller's identity_id, the universal user uuid post-
	// Phase-7) to the owner role. Atomic with the org create above —
	// a failure here rolls the whole bootstrap back, so no half-formed
	// org ever exists. "≥1 owner per org" is established by definition
	// for new orgs from this point forward.
	if err := bootstrapOrgRoles(ctx, qtx, org.ID, callerID); err != nil {
		slog.ErrorContext(ctx, "bootstrap org roles failed", "org_id", org.ID, "error", err)
		return nil, apierr.Internal("bootstrap org roles")
	}

	if err := tx.Commit(ctx); err != nil {
		slog.ErrorContext(ctx, "commit transaction failed", "org_id", org.ID, "error", err)
		return nil, apierr.Internal("commit transaction")
	}

	// Best-effort enrichment: org has committed, don't fail the
	// create on a transient identity lookup error.
	actors, resolveErr := s.resolveOrgActors(ctx, []db.Organization{org})
	if resolveErr != nil {
		slog.WarnContext(ctx, "create org: actor resolution failed; returning proto without audit actors",
			"org_id", org.ID, "error", resolveErr)
		actors = nil
	}
	return lro.DoneOperation(convert.OrganizationToProto(org, actors))
}
