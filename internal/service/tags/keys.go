package tags

import (
	"context"
	"log/slog"

	"cloud.google.com/go/longrunning/autogen/longrunningpb"
	"github.com/google/uuid"
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
	"github.com/dashkan/pivox/internal/resource"
	"github.com/dashkan/pivox/internal/server"
)

type TagKeysServer struct {
	apiv1.UnimplementedTagKeysServer
	pool    db.RWPool
	queries db.Querier
	filter  *filter.ResourceFilter
	codec   *appkey.Codec
	audit   *audit.Resolver
}

// TagKeysConfig is the constructor input for TagKeysServer.
type TagKeysConfig struct {
	// Pool is the database pool — used as a DBTX for filter reads
	// and as a TxBeginner for tx-wrapped delete paths. *pgxpool.Pool
	// satisfies db.RWPool directly. Required.
	Pool db.RWPool
	// Queries is the sqlc query interface. Required.
	Queries db.Querier
	// Codec opaque-encodes resource names. Required.
	Codec *appkey.Codec
	// AuditResolver inflates audit-field UUIDs into Actor protos.
	// Optional; nil leaves Actor fields unset.
	AuditResolver *audit.Resolver
}

// NewTagKeysServer constructs the server from cfg. Panics on a
// missing required field.
func NewTagKeysServer(cfg TagKeysConfig) *TagKeysServer {
	if cfg.Pool == nil {
		panic("tags: TagKeysConfig.Pool is required")
	}
	if cfg.Queries == nil {
		panic("tags: TagKeysConfig.Queries is required")
	}
	if cfg.Codec == nil {
		panic("tags: TagKeysConfig.Codec is required")
	}
	return &TagKeysServer{
		pool:    cfg.Pool,
		queries: cfg.Queries,
		filter:  filter.TagKeyFilter(),
		codec:   cfg.Codec,
		audit:   cfg.AuditResolver,
	}
}

// resolveTagKeyActors gathers created_by/updated_by UUIDs across the
// page and resolves them in a single batched call.
func (s *TagKeysServer) resolveTagKeyActors(ctx context.Context, rows []db.TagKey) (map[uuid.UUID]*typespb.Actor, error) {
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
		slog.ErrorContext(ctx, "resolve tag key actors failed", "error", err)
		return nil, apierr.Internal(err, "resolve actors")
	}
	return actors, nil
}

func (s *TagKeysServer) ListTagKeys(ctx context.Context, req *apiv1.ListTagKeysRequest) (*apiv1.ListTagKeysResponse, error) {
	orgID, err := resource.ResolveOrgParent(ctx, s.queries, req.GetParent())
	if err != nil {
		return nil, err
	}

	rows, err := filter.Query(ctx, s.pool, s.filter, filter.QueryParams{
		Filter:   req.GetFilter(),
		ParentID: orgID.String(),
		OrderBy:  req.GetOrderBy(),
		PageSize: req.GetPageSize(),
		Cursor:   req.GetPageToken(),
		Codec:    s.codec,
	})
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("filter", err.Error()))
	}

	results, err := filter.ScanTagKeys(rows)
	if err != nil {
		return nil, apierr.Internal(err, "database error")
	}

	pageSize := req.GetPageSize()
	if pageSize <= 0 {
		pageSize = 100
	}
	if pageSize > 1000 {
		pageSize = 1000
	}

	results, nextPageToken, err := filter.Paginate(results, int(pageSize), func(last db.TagKey) (string, error) {
		return filter.EncodeNextPageToken(s.codec, last.ID)
	})
	if err != nil {
		return nil, apierr.Internal(err, "encode page token")
	}

	actors, err := s.resolveTagKeyActors(ctx, results)
	if err != nil {
		return nil, err
	}
	tagKeys := make([]*apiv1.TagKey, 0, len(results))
	for _, r := range results {
		tagKeys = append(tagKeys, convert.TagKeyToProto(r, actors))
	}

	return &apiv1.ListTagKeysResponse{
		TagKeys:       tagKeys,
		NextPageToken: nextPageToken,
	}, nil
}

func (s *TagKeysServer) GetTagKey(ctx context.Context, req *apiv1.GetTagKeyRequest) (*apiv1.TagKey, error) {
	segment, err := resource.ParseSegment(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "TagKey", req.GetName())
	}
	id, err := uuid.Parse(segment)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "TagKey", req.GetName())
	}
	tagKey, err := s.queries.GetTagKey(ctx, id)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "TagKey", req.GetName())
	}
	actors, err := s.resolveTagKeyActors(ctx, []db.TagKey{tagKey})
	if err != nil {
		return nil, err
	}
	return convert.TagKeyToProto(tagKey, actors), nil
}

func (s *TagKeysServer) CreateTagKey(ctx context.Context, req *apiv1.CreateTagKeyRequest) (*longrunningpb.Operation, error) {
	tagKey := req.GetTagKey()

	orgID, err := resource.ResolveOrgParent(ctx, s.queries, req.GetParent())
	if err != nil {
		return nil, err
	}

	tagKeyID := req.GetTagKeyId()
	if tagKeyID == "" {
		tagKeyID = uuid.New().String()
	}

	// validate_only runs the INSERT against real constraints and rolls it
	// back, so a would-fail request (e.g. duplicate short_name) returns the
	// same error a live one would while persisting nothing.
	result, err := db.RunInTxValidate(ctx, s.pool, req.GetValidateOnly(), func(qtx db.Querier) (db.TagKey, error) {
		return qtx.CreateTagKey(ctx, db.CreateTagKeyParams{
			ID:             uuid.New(),
			OrgID:          orgID,
			ShortName:      tagKeyID,
			NamespacedName: orgID.String() + "/" + tagKeyID,
			Description:    tagKey.GetDescription(),
			CreatedBy:      convert.PgUUID(server.MustUserID(ctx)),
		})
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "TagKey", "")
	}

	actors, resolveErr := s.resolveTagKeyActors(ctx, []db.TagKey{result})
	if resolveErr != nil {
		slog.WarnContext(ctx, "create tag key: actor resolution failed; returning proto without audit actors",
			"tag_key_id", result.ID, "error", resolveErr)
		actors = nil
	}
	return lro.DoneOperation(convert.TagKeyToProto(result, actors))
}

func (s *TagKeysServer) UpdateTagKey(ctx context.Context, req *apiv1.UpdateTagKeyRequest) (*longrunningpb.Operation, error) {
	tagKey := req.GetTagKey()
	segment, err := resource.ParseSegment(tagKey.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "TagKey", tagKey.GetName())
	}
	id, err := uuid.Parse(segment)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "TagKey", tagKey.GetName())
	}

	existing, err := s.queries.GetTagKey(ctx, id)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "TagKey", tagKey.GetName())
	}

	updateParams := db.UpdateTagKeyParams{
		ID:        existing.ID,
		UpdatedBy: convert.PgUUID(server.MustUserID(ctx)),
	}

	mask := req.GetUpdateMask()
	if mask != nil {
		for _, path := range mask.GetPaths() {
			switch path {
			case "description":
				updateParams.Description = pgtype.Text{String: tagKey.GetDescription(), Valid: true}
			}
		}
	} else {
		updateParams.Description = pgtype.Text{String: tagKey.GetDescription(), Valid: true}
	}

	// validate_only runs the UPDATE against real constraints and rolls it
	// back, so a would-fail request returns the same error a live one would
	// while persisting nothing.
	result, err := db.RunInTxValidate(ctx, s.pool, req.GetValidateOnly(), func(qtx db.Querier) (db.TagKey, error) {
		return qtx.UpdateTagKey(ctx, updateParams)
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "TagKey", tagKey.GetName())
	}

	actors, resolveErr := s.resolveTagKeyActors(ctx, []db.TagKey{result})
	if resolveErr != nil {
		slog.WarnContext(ctx, "update tag key: actor resolution failed; returning proto without audit actors",
			"tag_key_id", result.ID, "error", resolveErr)
		actors = nil
	}
	return lro.DoneOperation(convert.TagKeyToProto(result, actors))
}

// DeleteTagKey removes an empty tag key. Refuses with
// FailedPrecondition if any TagValues still reference this key.
//
// Tx scope: lock the parent row FOR UPDATE, count children inside
// the same tx, DELETE on empty. Without the row lock, a concurrent
// CreateTagValue could land between our count and our DELETE,
// leaving us deleting a parent whose FK targets are still being
// referenced — depending on the FK action, either the DELETE fails
// late (RESTRICT) or the new child gets cascaded away (CASCADE).
// FOR UPDATE on the parent conflicts with the FK SHARE lock that a
// child INSERT takes, so concurrent inserts queue until our tx
// resolves.
func (s *TagKeysServer) DeleteTagKey(ctx context.Context, req *apiv1.DeleteTagKeyRequest) (*longrunningpb.Operation, error) {
	segment, err := resource.ParseSegment(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "TagKey", req.GetName())
	}
	id, err := uuid.Parse(segment)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "TagKey", req.GetName())
	}

	// validate_only runs the whole delete tx (row lock, child count,
	// DELETE) against real state and rolls it back, so a would-fail request
	// (e.g. the key still has values) returns the same error a live one
	// would while persisting nothing.
	if err := db.RunInTxVoidValidate(ctx, s.pool, req.GetValidateOnly(), func(qtx db.Querier) error {
		existing, err := qtx.GetTagKeyForUpdate(ctx, id)
		if err != nil {
			return apierr.HandleResourceError(err, "TagKey", req.GetName())
		}
		count, err := qtx.CountTagValuesByTagKey(ctx, existing.ID)
		if err != nil {
			return apierr.Internal(err, "failed to check tag values")
		}
		if count > 0 {
			return apierr.FailedPrecondition("cannot delete tag key with existing tag values")
		}
		if err := qtx.DeleteTagKey(ctx, existing.ID); err != nil {
			return apierr.HandleResourceError(err, "TagKey", req.GetName())
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return lro.DoneOperation(&apiv1.TagKey{})
}
