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

// parseConnectorName extracts the leaf UUID from a full Connector resource
// name ("organizations/{org}[/spaces/{space}]/connectors/{uuid}"). A malformed
// name or non-UUID leaf is NotFound (the named connector doesn't exist).
func parseConnectorName(name string) (uuid.UUID, error) {
	idx := strings.LastIndex(name, "/")
	if idx < 0 {
		return uuid.Nil, apierr.NotFound("Connector", name)
	}
	id, err := uuid.Parse(name[idx+1:])
	if err != nil {
		return uuid.Nil, apierr.NotFound("Connector", name)
	}
	return id, nil
}

// checkScope enforces that a connector fetched by its (global) uuid actually
// belongs to the caller's interceptor-resolved scope. Without this, a caller
// with access to org A could read/mutate a connector in org B by crafting
// "organizations/A/connectors/{B-uuid}" — the interceptor gates on A but never
// verifies the uuid is A's. A mismatch is NotFound (don't leak existence).
func checkScope(row db.Connector, orgID uuid.UUID, spaceID pgtype.UUID, name string) error {
	if row.OrgID != orgID || row.SpaceID != spaceID {
		return apierr.NotFound("Connector", name)
	}
	return nil
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

func (s *ConnectorsServer) ListConnectors(ctx context.Context, req *workflowsv1.ListConnectorsRequest) (*workflowsv1.ListConnectorsResponse, error) {
	orgID, spaceID, prefix := s.scope(ctx)

	pageSize := req.GetPageSize()
	if pageSize <= 0 {
		pageSize = 100
	}
	if pageSize > 1000 {
		pageSize = 1000
	}

	var cursor pgtype.UUID
	if tok := req.GetPageToken(); tok != "" {
		raw, err := s.codec.Decrypt(tok)
		if err != nil || len(raw) != 16 {
			return nil, apierr.InvalidArgument(apierr.FieldViolation("page_token", "invalid or malformed"))
		}
		var id uuid.UUID
		copy(id[:], raw)
		cursor = convert.PgUUID(id)
	}

	rows, err := s.queries.ListConnectorsByParent(ctx, db.ListConnectorsByParentParams{
		OrgID:     orgID,
		SpaceID:   spaceID,
		Cursor:    cursor,
		PageLimit: pageSize + 1,
	})
	if err != nil {
		return nil, apierr.Internal("list connectors")
	}

	var nextPageToken string
	if int32(len(rows)) > pageSize {
		nextPageToken, err = filter.EncodeNextPageToken(s.codec, rows[pageSize].ID)
		if err != nil {
			return nil, apierr.Internal("encode page token")
		}
		rows = rows[:pageSize]
	}

	actors := s.resolveActors(ctx, rows)
	out := make([]*workflowsv1.Connector, 0, len(rows))
	for _, r := range rows {
		out = append(out, convert.ConnectorToProto(r, prefix, actors))
	}
	return &workflowsv1.ListConnectorsResponse{Connectors: out, NextPageToken: nextPageToken}, nil
}

func (s *ConnectorsServer) GetConnector(ctx context.Context, req *workflowsv1.GetConnectorRequest) (*workflowsv1.Connector, error) {
	id, err := parseConnectorName(req.GetName())
	if err != nil {
		return nil, err
	}
	orgID, spaceID, prefix := s.scope(ctx)
	row, err := s.queries.GetConnector(ctx, id)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Connector", req.GetName())
	}
	if err := checkScope(row, orgID, spaceID, req.GetName()); err != nil {
		return nil, err
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
		return nil, apierr.Internal("generate connector id")
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
		ConnectorID: connectorID,
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
		if err := trackSecretRefs(ctx, qtx, row.ID, orgID, spaceID, in); err != nil {
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
	id, err := parseConnectorName(in.GetName())
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

	params := db.UpdateConnectorParams{
		ID:        id,
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
		existing, err := qtx.GetConnectorForUpdate(ctx, id)
		if err != nil {
			return apierr.HandleResourceError(err, "Connector", in.GetName())
		}
		if err := checkScope(existing, orgID, spaceID, in.GetName()); err != nil {
			return err
		}
		if etag := in.GetEtag(); etag != "" && etag != existing.Etag {
			return apierr.Aborted("Connector", in.GetName(), "etag mismatch")
		}
		row, err = qtx.UpdateConnector(ctx, params)
		if err != nil {
			return apierr.HandleResourceError(err, "Connector", in.GetName())
		}
		// Re-track secret refs only when config changed. A config-less update
		// (metadata only) leaves the tracked set — and the config — untouched.
		if inScope("config") {
			if err := trackSecretRefs(ctx, qtx, id, orgID, spaceID, in); err != nil {
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
	id, err := parseConnectorName(req.GetName())
	if err != nil {
		return nil, err
	}
	orgID, spaceID, _ := s.scope(ctx)
	err = db.RunInTxVoidValidate(ctx, s.pool, req.GetValidateOnly(), func(qtx db.Querier) error {
		existing, err := qtx.GetConnectorForUpdate(ctx, id)
		if err != nil {
			return apierr.HandleResourceError(err, "Connector", req.GetName())
		}
		if err := checkScope(existing, orgID, spaceID, req.GetName()); err != nil {
			return err
		}
		if etag := req.GetEtag(); etag != "" && etag != existing.Etag {
			return apierr.Aborted("Connector", req.GetName(), "etag mismatch")
		}
		return qtx.DeleteConnector(ctx, id)
	})
	if err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}
