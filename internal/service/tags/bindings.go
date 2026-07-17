package tags

import (
	"context"
	"log/slog"

	"cloud.google.com/go/longrunning/autogen/longrunningpb"
	"github.com/google/uuid"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/appkey"
	"github.com/dashkan/pivox/internal/audit"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/filter"
	"github.com/dashkan/pivox/internal/lro"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	typespb "github.com/dashkan/pivox/internal/pkg/gen/pivox/types"
	"github.com/dashkan/pivox/internal/resource"
	"github.com/dashkan/pivox/internal/server"
)

type TagBindingsServer struct {
	apiv1.UnimplementedTagBindingsServer
	pool    db.RWPool
	queries db.Querier
	filter  *filter.ResourceFilter
	codec   *appkey.Codec
	audit   *audit.Resolver
}

// TagBindingsConfig is the constructor input for TagBindingsServer.
type TagBindingsConfig struct {
	// Pool is the database pool — DBTX for filter reads + TxBeginner
	// for any future tx-wrapped paths. Required.
	Pool db.RWPool
	// Queries is the sqlc query interface. Required.
	Queries db.Querier
	// Codec opaque-encodes resource names. Required.
	Codec *appkey.Codec
	// AuditResolver inflates audit-field UUIDs into Actor protos.
	// Optional; nil leaves Actor fields unset.
	AuditResolver *audit.Resolver
}

// NewTagBindingsServer constructs the server from cfg. Panics on a
// missing required field.
func NewTagBindingsServer(cfg TagBindingsConfig) *TagBindingsServer {
	if cfg.Pool == nil {
		panic("tags: TagBindingsConfig.Pool is required")
	}
	if cfg.Queries == nil {
		panic("tags: TagBindingsConfig.Queries is required")
	}
	if cfg.Codec == nil {
		panic("tags: TagBindingsConfig.Codec is required")
	}
	return &TagBindingsServer{
		pool:    cfg.Pool,
		queries: cfg.Queries,
		filter:  filter.TagBindingFilter(),
		codec:   cfg.Codec,
		audit:   cfg.AuditResolver,
	}
}

// resolveTagBindingActors gathers created_by UUIDs (bindings have no
// updated_by) across the page and resolves them in one batched call.
func (s *TagBindingsServer) resolveTagBindingActors(ctx context.Context, rows []db.TagBinding) (map[uuid.UUID]*typespb.Actor, error) {
	if s.audit == nil {
		return nil, nil
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		if r.CreatedBy.Valid {
			ids = append(ids, r.CreatedBy.Bytes)
		}
	}
	actors, err := s.audit.Resolve(ctx, ids)
	if err != nil {
		slog.ErrorContext(ctx, "resolve tag binding actors failed", "error", err)
		return nil, apierr.Internal(err, "resolve actors")
	}
	return actors, nil
}

// ListTagBindings is a dynamic AIP-160 filtered + AIP-132 sorted +
// compound-cursor keyset list. The parent_resource is the NON-NEGOTIABLE base
// scope (parent_resource = $); the request's filter/order_by layer ON TOP of it
// and can only narrow. Every value is bound as a $N parameter by
// filter.BuildListQuery, and column/direction come only from TagBindingFilter's
// whitelist. This replaced the legacy id-only filter.Query path. Because
// parent_resource is constant within a single list scope, an order_by on it
// falls through to the id tiebreaker the compound cursor adds — the legacy ORDER
// BY had none and resumed on an id-only token, dropping/duplicating rows across
// boundaries. See docs/aip-list-transpiler-procedure.md.
func (s *TagBindingsServer) ListTagBindings(ctx context.Context, req *apiv1.ListTagBindingsRequest) (*apiv1.ListTagBindingsResponse, error) {
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
		Base:     []filter.Predicate{{SQL: "parent_resource = %s", Arg: req.GetParent()}},
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
		return nil, apierr.Internal(err, "list tag bindings")
	}
	results, err := filter.ScanTagBindings(pgxRows)
	if err != nil {
		return nil, apierr.Internal(err, "list tag bindings")
	}

	results, nextPageToken, err := filter.Paginate(results, int(pageSize), func(last db.TagBinding) (string, error) {
		return filter.EncodeCursor(s.codec, plan, tagBindingSortValue(plan, last), last.ID)
	})
	if err != nil {
		return nil, apierr.Internal(err, "encode page token")
	}

	actors, err := s.resolveTagBindingActors(ctx, results)
	if err != nil {
		return nil, err
	}
	tagBindings := make([]*apiv1.TagBinding, 0, len(results))
	for _, tb := range results {
		tv, err := s.queries.GetTagValue(ctx, tb.TagValueID)
		if err != nil {
			continue
		}
		tagBindings = append(tagBindings, convert.TagBindingToProto(tb, tv, actors))
	}

	return &apiv1.ListTagBindingsResponse{
		TagBindings:   tagBindings,
		NextPageToken: nextPageToken,
	}, nil
}

// tagBindingSortValue renders the active order_by column's value for the given
// row as the string the compound page token carries. parentResource is the only
// sortable and is a plain string; for the default id ordering (plan.Field == "")
// the value is unused, so "" is returned.
func tagBindingSortValue(plan filter.OrderByPlan, r db.TagBinding) string {
	switch plan.Field {
	case "parentResource":
		return r.ParentResource
	default:
		return ""
	}
}

func (s *TagBindingsServer) GetTagBinding(ctx context.Context, req *apiv1.GetTagBindingRequest) (*apiv1.TagBinding, error) {
	segment, err := resource.ParseSegment(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "TagBinding", req.GetName())
	}
	id, err := uuid.Parse(segment)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "TagBinding", req.GetName())
	}
	tb, err := s.queries.GetTagBinding(ctx, id)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "TagBinding", req.GetName())
	}
	tv, err := s.queries.GetTagValue(ctx, tb.TagValueID)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "TagValue", "")
	}
	actors, err := s.resolveTagBindingActors(ctx, []db.TagBinding{tb})
	if err != nil {
		return nil, err
	}
	return convert.TagBindingToProto(tb, tv, actors), nil
}

func (s *TagBindingsServer) CreateTagBinding(ctx context.Context, req *apiv1.CreateTagBindingRequest) (*longrunningpb.Operation, error) {
	tb := req.GetTagBinding()

	// Parse tag value name: "tagKeys/{uuid}/tagValues/{uuid}"
	tvID, err := parseTagValueName(tb.GetTagValue())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "TagValue", tb.GetTagValue())
	}
	tagValue, err := s.queries.GetTagValue(ctx, tvID)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "TagValue", tb.GetTagValue())
	}

	// validate_only runs the INSERT against real constraints and rolls it
	// back, so a would-fail request (e.g. duplicate binding) returns the
	// same error a live one would while persisting nothing.
	created, err := db.RunInTxValidate(ctx, s.pool, req.GetValidateOnly(), func(qtx db.Querier) (db.TagBinding, error) {
		return qtx.CreateTagBinding(ctx, db.CreateTagBindingParams{
			ID:             uuid.New(),
			ParentResource: req.GetParent(),
			TagValueID:     tagValue.ID,
			CreatedBy:      convert.PgUUID(server.MustUserID(ctx)),
		})
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "TagBinding", "")
	}
	actors, resolveErr := s.resolveTagBindingActors(ctx, []db.TagBinding{created})
	if resolveErr != nil {
		slog.WarnContext(ctx, "create tag binding: actor resolution failed; returning proto without audit actors",
			"tag_binding_id", created.ID, "error", resolveErr)
		actors = nil
	}
	return lro.DoneOperation(convert.TagBindingToProto(created, tagValue, actors))
}

func (s *TagBindingsServer) DeleteTagBinding(ctx context.Context, req *apiv1.DeleteTagBindingRequest) (*longrunningpb.Operation, error) {
	segment, err := resource.ParseSegment(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "TagBinding", req.GetName())
	}
	id, err := uuid.Parse(segment)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "TagBinding", req.GetName())
	}

	// validate_only runs the existence check + DELETE against real state
	// and rolls it back, so a would-fail request (e.g. missing binding)
	// returns the same error a live one would while persisting nothing.
	if err := db.RunInTxVoidValidate(ctx, s.pool, req.GetValidateOnly(), func(qtx db.Querier) error {
		existing, err := qtx.GetTagBinding(ctx, id)
		if err != nil {
			return apierr.HandleResourceError(err, "TagBinding", req.GetName())
		}
		if err := qtx.DeleteTagBinding(ctx, existing.ID); err != nil {
			return apierr.HandleResourceError(err, "TagBinding", req.GetName())
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return lro.DoneOperation(&apiv1.TagBinding{Name: req.GetName()})
}

func (s *TagBindingsServer) ListEffectiveTags(ctx context.Context, req *apiv1.ListEffectiveTagsRequest) (*apiv1.ListEffectiveTagsResponse, error) {
	rows, err := s.queries.ListEffectiveTags(ctx, req.GetParent())
	if err != nil {
		return nil, apierr.Internal(err, "database error")
	}

	effectiveTags := make([]*apiv1.EffectiveTag, 0, len(rows))
	for _, row := range rows {
		effectiveTags = append(effectiveTags, convert.EffectiveTagToProto(row))
	}

	return &apiv1.ListEffectiveTagsResponse{
		EffectiveTags: effectiveTags,
	}, nil
}
