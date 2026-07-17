// Package connectors implements the Connectors gRPC service — reusable,
// credentialed connections to external systems that workflow activities bind
// to by name. A Connector holds one system's endpoint config as an opaque
// typed `oneof config` (persisted as protojson in a JSONB column). Credentials
// are never inlined: config CEL fields reference vault Secrets via
// `secret("…")`, resolved at the injection boundary. This service does NOT own
// plaintext, so it takes no Encryptor.
package connectors

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/appkey"
	"github.com/dashkan/pivox/internal/audit"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/filter"
	typespb "github.com/dashkan/pivox/internal/pkg/gen/pivox/types"
	workflowsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/workflows/v1"
	"github.com/dashkan/pivox/internal/server"
)

// ConnectorsServer serves the Connectors RPCs.
type ConnectorsServer struct {
	workflowsv1.UnimplementedConnectorsServer
	pool    db.RWPool
	queries db.Querier
	codec   *appkey.Codec
	audit   *audit.Resolver
}

// Config is the constructor input for ConnectorsServer.
type Config struct {
	// Pool is the database pool (DBTX for reads, TxBeginner for tx writes).
	// Required.
	Pool db.RWPool
	// Queries is the sqlc query interface. Required.
	Queries db.Querier
	// Codec opaque-encodes page tokens. Required.
	Codec *appkey.Codec
	// AuditResolver inflates audit-field UUIDs into Actor protos. Optional.
	AuditResolver *audit.Resolver
}

// NewConnectorsServer constructs the server from cfg. Panics on a missing
// required field.
func NewConnectorsServer(cfg Config) *ConnectorsServer {
	if cfg.Pool == nil {
		panic("connectors: Config.Pool is required")
	}
	if cfg.Queries == nil {
		panic("connectors: Config.Queries is required")
	}
	if cfg.Codec == nil {
		panic("connectors: Config.Codec is required")
	}
	return &ConnectorsServer{
		pool:    cfg.Pool,
		queries: cfg.Queries,
		codec:   cfg.Codec,
		audit:   cfg.AuditResolver,
	}
}

// scope reads the interceptor-resolved org (always set) and space (set when
// the resource is space-scoped) from ctx. Returns the org id, the nullable
// space id, and the parent resource-name prefix.
func (s *ConnectorsServer) scope(ctx context.Context) (orgID uuid.UUID, spaceID pgtype.UUID, namePrefix string) {
	org := server.MustResolvedOrgFromContext(ctx)
	orgID = org.ID
	namePrefix = "organizations/" + org.Slug
	if space, ok := server.ResolvedSpaceFromContext(ctx); ok {
		spaceID = convert.PgUUID(space.ID)
		namePrefix += "/spaces/" + space.Slug
	}
	return orgID, spaceID, namePrefix
}

// resolveActors batch-resolves created_by/updated_by across the page.
func (s *ConnectorsServer) resolveActors(ctx context.Context, rows []db.Connector) map[uuid.UUID]*typespb.Actor {
	if s.audit == nil {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(rows)*2)
	for _, r := range rows {
		if r.CreatedBy.Valid {
			ids = append(ids, r.CreatedBy.Bytes)
		}
		if r.UpdatedBy.Valid {
			ids = append(ids, r.UpdatedBy.Bytes)
		}
	}
	actors, err := s.audit.Resolve(ctx, ids)
	if err != nil {
		slog.WarnContext(ctx, "connectors: actor resolution failed; returning without actors", "error", err)
		return nil
	}
	return actors
}

// parseConnectorName extracts the slug leaf from a full Connector resource name
// ("organizations/{org}[/spaces/{space}]/connectors/{slug}"). The slug is the
// user-assigned id, unique within the connector's parent scope; the handler
// resolves it to the internal uuid via a scoped (org + space + slug) lookup, so
// a name that names another org's connector simply finds no row in this scope
// (NotFound, no cross-scope leak). A name with no leaf is NotFound.
func parseConnectorName(name string) (string, error) {
	idx := strings.LastIndex(name, "/")
	if idx < 0 || idx == len(name)-1 {
		return "", apierr.NotFound("Connector", name)
	}
	return name[idx+1:], nil
}

// marshalAnnotations renders the labels map as JSONB. Empty map → "{}" (the
// column is NOT NULL DEFAULT '{}').
func marshalAnnotations(m map[string]string) (json.RawMessage, error) {
	if len(m) == 0 {
		return json.RawMessage("{}"), nil
	}
	return json.Marshal(m)
}

// marshalConfig renders the typed `oneof config` as protojson for the config
// JSONB column ({"http": {...}}). An unset oneof → "{}" (matches the column
// default). Marshaling only the oneof (via a scratch Connector) keeps the
// stored shape symmetric with convert.ConnectorToProto's read path.
func marshalConfig(in *workflowsv1.Connector) (json.RawMessage, error) {
	if in.GetConfig() == nil {
		return json.RawMessage("{}"), nil
	}
	b, err := protojson.Marshal(&workflowsv1.Connector{Config: in.GetConfig()})
	if err != nil {
		return nil, err
	}
	return b, nil
}

// ListConnectors is a dynamic AIP-160 filtered + AIP-132 sorted +
// compound-cursor keyset list. The interceptor-resolved (org, space) is the
// NON-NEGOTIABLE base scope, ANDed as the base of the query; the request's
// filter/order_by layer ON TOP of it and can only narrow, never widen. Every
// value (scope ids, filter operands, cursor values, page size) is bound as a
// $N parameter by filter.BuildListQuery — nothing is string-interpolated — and
// column/direction come only from ConnectorFilter's whitelist. See
// docs/aip-list-transpiler-procedure.md for the general procedure.
func (s *ConnectorsServer) ListConnectors(ctx context.Context, req *workflowsv1.ListConnectorsRequest) (*workflowsv1.ListConnectorsResponse, error) {
	orgID, spaceID, prefix := s.scope(ctx)
	rf := filter.ConnectorFilter()
	pageSize := filter.ClampPageSize(rf, req.GetPageSize())

	// Resolve order_by against the sortable whitelist (default: id). The plan
	// also tells the cursor codec whether the sort value is a timestamp.
	plan, err := filter.PlanOrderBy(rf, req.GetOrderBy())
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("order_by", err.Error()))
	}

	cursor, err := filter.DecodeCursor(s.codec, plan, req.GetPageToken())
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("page_token", "invalid or malformed"))
	}

	// Base scope: the interceptor-resolved org (always) + a space predicate that
	// depends on the parent's scope. The AIP filter is layered on top — it can
	// only narrow within the scope.
	//
	//   - Space-scoped parent (spaceID.Valid): narrow to that one space.
	//   - Org-level parent (!spaceID.Valid): the ROLLUP — org-direct rows
	//     (space_id NULL) PLUS every space's rows. No space_id predicate, so the
	//     org scope alone bounds the list. This matches the WorkflowRuns org
	//     wildcard (BE-1): the permission interceptor already gated
	//     connectors.read at the org scope, which is defined to cover the whole
	//     org (org-direct + all spaces).
	base := []filter.Predicate{{SQL: "org_id = %s", Arg: orgID}}
	if spaceID.Valid {
		base = append(base, filter.Predicate{SQL: "space_id = %s", Arg: spaceID})
	}
	sql, args, err := filter.BuildListQuery(filter.ListQuery{
		Resource: rf,
		Base:     base,
		Filter:   req.GetFilter(),
		Order:    plan,
		PageSize: pageSize,
		Cursor:   cursor,
	})
	if err != nil {
		// The only error source is the filter transpiler (bad user filter, e.g.
		// an unknown field) — surface it as InvalidArgument on "filter".
		return nil, apierr.InvalidArgument(apierr.FieldViolation("filter", err.Error()))
	}

	pgxRows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, apierr.Internal(err, "list connectors")
	}
	rows, err := filter.ScanConnectors(pgxRows)
	if err != nil {
		return nil, apierr.Internal(err, "list connectors")
	}

	// filter.Paginate trims the over-fetched result to pageSize and derives the
	// next-page token from the LAST RETURNED row — never the first un-returned
	// row (the resume predicate is a strict `>`/`<`, so rows[pageSize] would
	// silently drop it next page). Owning both the trim and the token here makes
	// that off-by-one unrepresentable at the call site.
	rows, nextPageToken, err := filter.Paginate(rows, int(pageSize), func(last db.Connector) (string, error) {
		return filter.EncodeCursor(s.codec, plan, connectorSortValue(plan, last), last.ID)
	})
	if err != nil {
		return nil, apierr.Internal(err, "encode page token")
	}

	actors := s.resolveActors(ctx, rows)
	// Per-row name prefix. A space-scoped list shares the single `prefix` (which
	// already carries /spaces/{space}) across every row. The org-level rollup
	// names each row by its actual location: an org-direct row (space_id NULL)
	// keeps the bare org prefix, while a space-scoped row gets its space segment,
	// with the page's distinct space slugs resolved in one batched lookup (no
	// N+1).
	var spaceSlugs map[uuid.UUID]string
	if !spaceID.Valid {
		// FAIL CLOSED: on a slug-resolution failure the space rows are already
		// fetched, but naming them without their /spaces/{slug} segment would
		// emit well-formed org-direct names that mis-route a later Get/Update/
		// Delete into the wrong scope. A wrong-but-valid name is worse than an
		// error, so surface the failure instead of rendering a mis-addressable
		// page.
		spaceSlugs, err = s.resolveSpaceSlugs(ctx, orgID, rows)
		if err != nil {
			return nil, apierr.Internal(err, "resolve space slugs")
		}
	}
	out := make([]*workflowsv1.Connector, 0, len(rows))
	for _, r := range rows {
		p := prefix
		if !spaceID.Valid && r.SpaceID.Valid {
			slug, ok := spaceSlugs[uuid.UUID(r.SpaceID.Bytes)]
			if !ok {
				// The slug query succeeded but this row's space is absent from the
				// result — a cross-org anomaly (no same-org FK enforces that a
				// connector's space belongs to its org). Unreachable via the API
				// today. OMIT the row rather than emit a mis-addressable
				// org-direct name: omission is safe where a wrong name is not.
				slog.WarnContext(ctx, "connectors: space slug missing for rolled-up connector; omitting from response",
					"connector_id", r.ID, "space_id", uuid.UUID(r.SpaceID.Bytes))
				continue
			}
			p = prefix + "/spaces/" + slug
		}
		out = append(out, convert.ConnectorToProto(r, p, actors))
	}

	// The "agents in use" facet is computed over the BASE SCOPE only (the whole
	// org for a rollup, or the selected space) — deliberately NOT narrowed by the
	// request filter, so a client can offer an agent-filter dropdown listing
	// every agent assigned to a connector in scope regardless of the current
	// page's filter. One extra distinct read alongside the page; no tx.
	agents, err := s.agentsInUse(ctx, orgID, spaceID)
	if err != nil {
		return nil, apierr.Internal(err, "list agents in use")
	}
	return &workflowsv1.ListConnectorsResponse{
		Connectors:    out,
		NextPageToken: nextPageToken,
		AgentsInUse:   agents,
	}, nil
}

// agentsInUse returns the distinct, sorted, non-empty agent values in the
// list's base scope: one space when spaceID is valid, else the whole org
// (org-direct + all spaces, matching the org-level rollup). The distinct/sort/
// non-empty filtering is done in SQL; this just dispatches on scope.
func (s *ConnectorsServer) agentsInUse(ctx context.Context, orgID uuid.UUID, spaceID pgtype.UUID) ([]string, error) {
	if spaceID.Valid {
		return s.queries.ListDistinctConnectorAgentsInSpace(ctx, db.ListDistinctConnectorAgentsInSpaceParams{
			OrgID:   orgID,
			SpaceID: spaceID,
		})
	}
	return s.queries.ListDistinctConnectorAgentsInOrg(ctx, orgID)
}

// resolveSpaceSlugs maps the distinct valid space ids across an org-level
// rollup page to their slug (spaces.name), scoped to orgID so a foreign org's
// space slug can never be resolved. One batched read (no N+1); a single
// autocommit statement needs no tx (per internal/CLAUDE.md). Mirrors the
// resolveActors helper shape. The query error is PROPAGATED (fail closed): the
// caller must not render a page of space rows without their space segment, as
// the resulting org-direct names would mis-route later mutations.
func (s *ConnectorsServer) resolveSpaceSlugs(ctx context.Context, orgID uuid.UUID, rows []db.Connector) (map[uuid.UUID]string, error) {
	var ids []uuid.UUID
	seen := make(map[uuid.UUID]struct{})
	for _, r := range rows {
		if !r.SpaceID.Valid {
			continue
		}
		id := uuid.UUID(r.SpaceID.Bytes)
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	slugByID := make(map[uuid.UUID]string, len(ids))
	if len(ids) == 0 {
		return slugByID, nil
	}
	got, err := s.queries.GetSpaceSlugsByIDs(ctx, db.GetSpaceSlugsByIDsParams{
		Ids:   ids,
		OrgID: orgID,
	})
	if err != nil {
		return nil, err
	}
	for _, sr := range got {
		slugByID[sr.ID] = sr.Name
	}
	return slugByID, nil
}

// connectorSortValue renders the primary order_by column's value for the given
// row as the string the compound page token carries. Timestamps use
// RFC3339Nano so filter.DecodeCursor can parse them back to an exact
// time.Time. For the default id ordering (plan.Field == "") the value is
// unused (EncodeCursor emits the id-only token), so "" is returned.
func connectorSortValue(plan filter.OrderByPlan, r db.Connector) string {
	switch plan.Field {
	case "displayName":
		return r.DisplayName
	case "createTime":
		return r.CreateTime.UTC().Format(time.RFC3339Nano)
	case "updateTime":
		return r.UpdateTime.UTC().Format(time.RFC3339Nano)
	default:
		return ""
	}
}

func (s *ConnectorsServer) GetConnector(ctx context.Context, req *workflowsv1.GetConnectorRequest) (*workflowsv1.Connector, error) {
	slug, err := parseConnectorName(req.GetName())
	if err != nil {
		return nil, err
	}
	orgID, spaceID, prefix := s.scope(ctx)
	row, err := s.queries.GetConnectorByParent(ctx, db.GetConnectorByParentParams{
		OrgID:   orgID,
		SpaceID: spaceID,
		Slug:    slug,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Connector", req.GetName())
	}
	return convert.ConnectorToProto(row, prefix, s.resolveActors(ctx, []db.Connector{row})), nil
}

func (s *ConnectorsServer) CreateConnector(ctx context.Context, req *workflowsv1.CreateConnectorRequest) (*workflowsv1.Connector, error) {
	connectorID := req.GetConnectorId()
	if connectorID == "" {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("connector_id", "is required"))
	}
	in := req.GetConnector()
	orgID, spaceID, prefix := s.scope(ctx)

	// App-generate the id up front (v7 for time-ordered listing, matching the
	// column's uuidv7 intent) so the caller has it before the write.
	id, err := uuid.NewV7()
	if err != nil {
		return nil, apierr.Internal(err, "generate connector id")
	}
	config, err := marshalConfig(in)
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("connector.config", err.Error()))
	}
	annotations, err := marshalAnnotations(in.GetAnnotations())
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("connector.annotations", err.Error()))
	}

	params := db.CreateConnectorParams{
		ID:          id,
		OrgID:       orgID,
		SpaceID:     spaceID,
		Slug:        connectorID,
		DisplayName: in.GetDisplayName(),
		Description: in.GetDescription(),
		Config:      config,
		Agent:       in.GetAgent(),
		Annotations: annotations,
		CreatedBy:   convert.PgUUID(server.MustUserID(ctx)),
	}
	// validate_only rolls back the insert but still runs it, so a would-fail
	// request (e.g. duplicate connector_id, or a config referencing a missing
	// secret) returns the same error a live one would. Both the write and the
	// secret-ref tracking run in the one tx — there are no non-DB side effects
	// to guard here. Errors are shaped inside the closure so trackSecretRefs's
	// InvalidArgument isn't flattened to Internal by an outer HandleResourceError.
	row, err := db.RunInTxValidate(ctx, s.pool, req.GetValidateOnly(), func(qtx db.Querier) (db.Connector, error) {
		row, err := qtx.CreateConnector(ctx, params)
		if err != nil {
			return db.Connector{}, apierr.HandleResourceError(err, "Connector", connectorID)
		}
		if err := trackSecretRefs(ctx, qtx, row.ID, orgID, spaceID, prefix, in); err != nil {
			return db.Connector{}, err
		}
		return row, nil
	})
	if err != nil {
		return nil, err
	}
	return convert.ConnectorToProto(row, prefix, s.resolveActors(ctx, []db.Connector{row})), nil
}

func (s *ConnectorsServer) UpdateConnector(ctx context.Context, req *workflowsv1.UpdateConnectorRequest) (*workflowsv1.Connector, error) {
	in := req.GetConnector()
	slug, err := parseConnectorName(in.GetName())
	if err != nil {
		return nil, err
	}
	orgID, spaceID, prefix := s.scope(ctx)

	mask := req.GetUpdateMask()
	// inScope: an empty mask means all fields are significant (AIP
	// full-replace). Unlike a Secret's write-only value, a connector's config
	// is readable, so it takes part in the full-replace default.
	inScope := func(field string) bool {
		if mask == nil || len(mask.GetPaths()) == 0 {
			return true
		}
		return slices.Contains(mask.GetPaths(), field)
	}

	// ID is filled in-tx from the scope-resolved row (the name leaf is the slug,
	// not the internal uuid).
	params := db.UpdateConnectorParams{
		UpdatedBy: convert.PgUUID(server.MustUserID(ctx)),
	}
	if inScope("display_name") {
		params.DisplayName = pgtype.Text{String: in.GetDisplayName(), Valid: true}
	}
	if inScope("description") {
		params.Description = pgtype.Text{String: in.GetDescription(), Valid: true}
	}
	if inScope("agent") {
		params.Agent = pgtype.Text{String: in.GetAgent(), Valid: true}
	}
	if inScope("annotations") {
		annotations, err := marshalAnnotations(in.GetAnnotations())
		if err != nil {
			return nil, apierr.InvalidArgument(apierr.FieldViolation("connector.annotations", err.Error()))
		}
		params.Annotations = annotations
	}
	if inScope("config") {
		// The secret("…") refs in this config are re-derived and tracked
		// in-tx below (only when config is in scope — an update that leaves
		// config untouched keeps its existing refs).
		config, err := marshalConfig(in)
		if err != nil {
			return nil, apierr.InvalidArgument(apierr.FieldViolation("connector.config", err.Error()))
		}
		params.Config = config
	}

	var row db.Connector
	err = db.RunInTxVoidValidate(ctx, s.pool, req.GetValidateOnly(), func(qtx db.Querier) error {
		existing, err := qtx.GetConnectorByParentForUpdate(ctx, db.GetConnectorByParentForUpdateParams{
			OrgID:   orgID,
			SpaceID: spaceID,
			Slug:    slug,
		})
		if err != nil {
			return apierr.HandleResourceError(err, "Connector", in.GetName())
		}
		if etag := in.GetEtag(); etag != "" && etag != existing.Etag {
			return apierr.Aborted("Connector", in.GetName(), "etag mismatch")
		}
		params.ID = existing.ID
		row, err = qtx.UpdateConnector(ctx, params)
		if err != nil {
			return apierr.HandleResourceError(err, "Connector", in.GetName())
		}
		// Re-track secret refs only when config changed. A config-less update
		// (metadata only) leaves the tracked set — and the config — untouched.
		if inScope("config") {
			if err := trackSecretRefs(ctx, qtx, existing.ID, orgID, spaceID, prefix, in); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return convert.ConnectorToProto(row, prefix, s.resolveActors(ctx, []db.Connector{row})), nil
}

func (s *ConnectorsServer) DeleteConnector(ctx context.Context, req *workflowsv1.DeleteConnectorRequest) (*emptypb.Empty, error) {
	slug, err := parseConnectorName(req.GetName())
	if err != nil {
		return nil, err
	}
	orgID, spaceID, _ := s.scope(ctx)
	err = db.RunInTxVoidValidate(ctx, s.pool, req.GetValidateOnly(), func(qtx db.Querier) error {
		existing, err := qtx.GetConnectorByParentForUpdate(ctx, db.GetConnectorByParentForUpdateParams{
			OrgID:   orgID,
			SpaceID: spaceID,
			Slug:    slug,
		})
		if err != nil {
			return apierr.HandleResourceError(err, "Connector", req.GetName())
		}
		if etag := req.GetEtag(); etag != "" && etag != existing.Etag {
			return apierr.Aborted("Connector", req.GetName(), "etag mismatch")
		}
		return qtx.DeleteConnector(ctx, existing.ID)
	})
	if err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}
