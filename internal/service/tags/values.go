package tags

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"cloud.google.com/go/longrunning/autogen/longrunningpb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/appkey"
	"github.com/dashkan/pivox/internal/audit"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/filter"
	"github.com/dashkan/pivox/internal/lro"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	typespb "github.com/dashkan/pivox/internal/pkg/gen/pivox/types"
	"github.com/dashkan/pivox/internal/server"
)

type TagValuesServer struct {
	apiv1.UnimplementedTagValuesServer
	pool    db.RWPool
	queries db.Querier
	filter  *filter.ResourceFilter
	codec   *appkey.Codec
	audit   *audit.Resolver
}

// TagValuesConfig is the constructor input for TagValuesServer.
type TagValuesConfig struct {
	// Pool is the database pool — DBTX for filter reads + TxBeginner
	// for tx-wrapped delete paths. Required.
	Pool db.RWPool
	// Queries is the sqlc query interface. Required.
	Queries db.Querier
	// Codec opaque-encodes resource names. Required.
	Codec *appkey.Codec
	// AuditResolver inflates audit-field UUIDs into Actor protos.
	// Optional; nil leaves Actor fields unset.
	AuditResolver *audit.Resolver
}

// NewTagValuesServer constructs the server from cfg. Panics on a
// missing required field.
func NewTagValuesServer(cfg TagValuesConfig) *TagValuesServer {
	if cfg.Pool == nil {
		panic("tags: TagValuesConfig.Pool is required")
	}
	if cfg.Queries == nil {
		panic("tags: TagValuesConfig.Queries is required")
	}
	if cfg.Codec == nil {
		panic("tags: TagValuesConfig.Codec is required")
	}
	return &TagValuesServer{
		pool:    cfg.Pool,
		queries: cfg.Queries,
		filter:  filter.TagValueFilter(),
		codec:   cfg.Codec,
		audit:   cfg.AuditResolver,
	}
}

// resolveTagValueActors gathers created_by/updated_by UUIDs across
// the page and resolves them in a single batched call.
func (s *TagValuesServer) resolveTagValueActors(ctx context.Context, rows []db.TagValue) (map[uuid.UUID]*typespb.Actor, error) {
	if s.audit == nil {
		return nil, nil
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
		slog.ErrorContext(ctx, "resolve tag value actors failed", "error", err)
		return nil, apierr.Internal(err, "resolve actors")
	}
	return actors, nil
}

// parseTagKeyParent parses "organizations/{org}/tagKeys/{uuid}" and returns the
// tag key UUID. The name is org-scoped so it round-trips through the permission
// interceptor's scope extractor; the org segment is resolved by the interceptor,
// so here we only need the leaf tag key id.
func parseTagKeyParent(parent string) (uuid.UUID, error) {
	parts := strings.Split(parent, "/")
	if len(parts) != 4 || parts[0] != "organizations" || parts[1] == "" || parts[2] != "tagKeys" {
		return uuid.Nil, fmt.Errorf("invalid tag key parent %q: expected organizations/*/tagKeys/*", parent)
	}
	return uuid.Parse(parts[3])
}

// parseTagValueName parses "organizations/{org}/tagKeys/{uuid}/tagValues/{uuid}"
// and returns the tag value UUID.
func parseTagValueName(name string) (uuid.UUID, error) {
	parts := strings.Split(name, "/")
	if len(parts) != 6 || parts[0] != "organizations" || parts[1] == "" || parts[2] != "tagKeys" || parts[4] != "tagValues" {
		return uuid.Nil, fmt.Errorf("invalid tag value name %q: expected organizations/*/tagKeys/*/tagValues/*", name)
	}
	return uuid.Parse(parts[5])
}

// ListTagValues is a dynamic AIP-160 filtered + AIP-132 sorted + compound-cursor
// keyset list. The parent tag key is the NON-NEGOTIABLE base scope
// (tag_key_id = $); the request's filter/order_by layer ON TOP of it and can
// only narrow. Every value is bound as a $N parameter by filter.BuildListQuery,
// and column/direction come only from TagValueFilter's whitelist. This replaced
// the legacy id-only filter.Query path (id-only cursor + non-id sortable columns
// → drop/dup across page boundaries under a custom order_by). See
// docs/aip-list-transpiler-procedure.md.
func (s *TagValuesServer) ListTagValues(ctx context.Context, req *apiv1.ListTagValuesRequest) (*apiv1.ListTagValuesResponse, error) {
	tagKeyID, err := parseTagKeyParent(req.GetParent())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "TagKey", req.GetParent())
	}
	// Verify the tag key exists.
	if _, err := s.queries.GetTagKey(ctx, tagKeyID); err != nil {
		return nil, apierr.HandleResourceError(err, "TagKey", req.GetParent())
	}

	rf := s.filter
	pageSize := filter.ClampPageSize(rf, req.GetPageSize())

	plan, err := filter.PlanOrderBy(rf, req.GetOrderBy())
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("order_by", err.Error()))
	}
	cursor, err := filter.DecodeCursor(s.codec, plan, req.GetPageToken())
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("page_token", "invalid or malformed"))
	}

	sql, args, err := filter.BuildListQuery(filter.ListQuery{
		Resource: rf,
		Base:     []filter.Predicate{{SQL: "tag_key_id = %s", Arg: tagKeyID}},
		Filter:   req.GetFilter(),
		Order:    plan,
		PageSize: pageSize,
		Cursor:   cursor,
	})
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("filter", err.Error()))
	}

	pgxRows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, apierr.Internal(err, "list tag values")
	}
	results, err := filter.ScanTagValues(pgxRows)
	if err != nil {
		return nil, apierr.Internal(err, "list tag values")
	}

	results, nextPageToken, err := filter.Paginate(results, int(pageSize), func(last db.TagValue) (string, error) {
		return filter.EncodeCursor(s.codec, plan, tagValueSortValue(plan, last), last.ID)
	})
	if err != nil {
		return nil, apierr.Internal(err, "encode page token")
	}

	actors, err := s.resolveTagValueActors(ctx, results)
	if err != nil {
		return nil, err
	}
	orgSlug := server.MustResolvedOrgFromContext(ctx).Slug
	tagValues := make([]*apiv1.TagValue, 0, len(results))
	for _, r := range results {
		tagValues = append(tagValues, convert.TagValueToProto(r, orgSlug, actors))
	}

	return &apiv1.ListTagValuesResponse{
		TagValues:     tagValues,
		NextPageToken: nextPageToken,
	}, nil
}

// tagValueSortValue renders the active order_by column's value for the given row
// as the string the compound page token carries (timestamps as RFC3339Nano). For
// the default id ordering (plan.Field == "") the value is unused, so "" is
// returned.
func tagValueSortValue(plan filter.OrderByPlan, r db.TagValue) string {
	switch plan.Field {
	case "shortName":
		return r.ShortName
	case "namespacedName":
		return r.NamespacedName
	case "createTime":
		return r.CreateTime.UTC().Format(time.RFC3339Nano)
	default:
		return ""
	}
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
	actors, err := s.resolveTagValueActors(ctx, []db.TagValue{tagValue})
	if err != nil {
		return nil, err
	}
	orgSlug := server.MustResolvedOrgFromContext(ctx).Slug
	return convert.TagValueToProto(tagValue, orgSlug, actors), nil
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
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierr.NotFound("TagKey", parent)
		}
		return nil, apierr.Internal(err, "failed to get parent tag key")
	}

	tagValueID := req.GetTagValueId()
	if tagValueID == "" {
		tagValueID = uuid.New().String()
	}
	namespacedName := parentKey.NamespacedName + "/" + tagValueID

	// validate_only runs the INSERT against real constraints and rolls it
	// back, so a would-fail request (e.g. duplicate short_name) returns the
	// same error a live one would while persisting nothing.
	result, err := db.RunInTxValidate(ctx, s.pool, req.GetValidateOnly(), func(qtx db.Querier) (db.TagValue, error) {
		return qtx.CreateTagValue(ctx, db.CreateTagValueParams{
			ID:             uuid.New(),
			TagKeyID:       parentKey.ID,
			ShortName:      tagValueID,
			NamespacedName: namespacedName,
			Description:    tagValue.GetDescription(),
			CreatedBy:      convert.PgUUID(server.MustUserID(ctx)),
		})
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "TagValue", "")
	}

	actors, resolveErr := s.resolveTagValueActors(ctx, []db.TagValue{result})
	if resolveErr != nil {
		slog.WarnContext(ctx, "create tag value: actor resolution failed; returning proto without audit actors",
			"tag_value_id", result.ID, "error", resolveErr)
		actors = nil
	}
	orgSlug := server.MustResolvedOrgFromContext(ctx).Slug
	return lro.DoneOperation(convert.TagValueToProto(result, orgSlug, actors))
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
		UpdatedBy: convert.PgUUID(server.MustUserID(ctx)),
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

	// validate_only runs the UPDATE against real constraints and rolls it
	// back, so a would-fail request returns the same error a live one would
	// while persisting nothing.
	result, err := db.RunInTxValidate(ctx, s.pool, req.GetValidateOnly(), func(qtx db.Querier) (db.TagValue, error) {
		return qtx.UpdateTagValue(ctx, updateParams)
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "TagValue", tagValue.GetName())
	}

	actors, resolveErr := s.resolveTagValueActors(ctx, []db.TagValue{result})
	if resolveErr != nil {
		slog.WarnContext(ctx, "update tag value: actor resolution failed; returning proto without audit actors",
			"tag_value_id", result.ID, "error", resolveErr)
		actors = nil
	}
	orgSlug := server.MustResolvedOrgFromContext(ctx).Slug
	return lro.DoneOperation(convert.TagValueToProto(result, orgSlug, actors))
}

// DeleteTagValue removes an unbound tag value. Refuses with
// FailedPrecondition if any TagBindings still reference it.
//
// Tx scope: lock the parent row FOR UPDATE, count bindings inside
// the same tx, DELETE on empty. The FK SHARE lock that a binding
// INSERT takes on this row conflicts with our FOR UPDATE, so a
// concurrent CreateTagBinding queues until our tx resolves —
// closing the TOCTOU between "no bindings" and "delete value".
func (s *TagValuesServer) DeleteTagValue(ctx context.Context, req *apiv1.DeleteTagValueRequest) (*longrunningpb.Operation, error) {
	id, err := parseTagValueName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "TagValue", req.GetName())
	}

	// validate_only runs the whole delete tx (row lock, binding count,
	// DELETE) against real state and rolls it back, so a would-fail request
	// (e.g. the value still has bindings) returns the same error a live one
	// would while persisting nothing.
	if err := db.RunInTxVoidValidate(ctx, s.pool, req.GetValidateOnly(), func(qtx db.Querier) error {
		existing, err := qtx.GetTagValueForUpdate(ctx, id)
		if err != nil {
			return apierr.HandleResourceError(err, "TagValue", req.GetName())
		}
		bindingCount, err := qtx.CountTagBindingsByTagValue(ctx, existing.ID)
		if err != nil {
			return apierr.Internal(err, "failed to check tag bindings")
		}
		if bindingCount > 0 {
			return apierr.FailedPrecondition("cannot delete tag value with existing tag bindings")
		}
		if err := qtx.DeleteTagValue(ctx, existing.ID); err != nil {
			return apierr.HandleResourceError(err, "TagValue", req.GetName())
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return lro.DoneOperation(&apiv1.TagValue{})
}
