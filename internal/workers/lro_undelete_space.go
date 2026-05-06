package workers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/dashkan/pivox/internal/audit"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	typespb "github.com/dashkan/pivox/internal/pkg/gen/pivox/types"
)

// UndeleteSpaceArgs is the River job input for the UndeleteSpace
// LRO. OperationID lets the worker mark the operations row done in
// the same tx as the SQL action; SpaceID + UpdatedBy parameterize
// the UPDATE; OrgSlug is needed to render the result proto's
// `name = organizations/{org}/spaces/{space}` field — the worker
// can't derive it from the space row alone.
type UndeleteSpaceArgs struct {
	OperationID uuid.UUID `json:"operation_id"`
	SpaceID     uuid.UUID `json:"space_id"`
	OrgSlug     string    `json:"org_slug"`
	UpdatedBy   uuid.UUID `json:"updated_by"`
}

// Kind implements river.JobArgs. Stable string — changing it would
// orphan in-flight rows.
func (UndeleteSpaceArgs) Kind() string { return "lro_undelete_space" }

// UndeleteSpaceWorker handles UndeleteSpace LROs. Single SQL step:
// flip the space back to ACTIVE, return the updated proto.
//
// Atomicity: SQL action + operations completion + River job
// completion all run in one pgx tx. Either everything commits or
// nothing does.
type UndeleteSpaceWorker struct {
	river.WorkerDefaults[UndeleteSpaceArgs]

	Pool   *pgxpool.Pool
	Audit  *audit.Resolver
	Logger *slog.Logger
}

// Work implements river.Worker[UndeleteSpaceArgs].
func (w *UndeleteSpaceWorker) Work(ctx context.Context, job *river.Job[UndeleteSpaceArgs]) error {
	args := job.Args

	tx, err := w.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after Commit

	qtx := db.New(tx)

	updated, workErr := qtx.UndeleteSpace(ctx, db.UndeleteSpaceParams{
		ID:        args.SpaceID,
		UpdatedBy: pgtype.UUID{Bytes: args.UpdatedBy, Valid: args.UpdatedBy != uuid.Nil},
	})

	switch {
	case workErr == nil:
		actors, resolveErr := w.resolveActors(ctx, updated)
		if resolveErr != nil {
			w.Logger.WarnContext(ctx, "lro_undelete_space: actor resolution failed; result emitted without actors",
				"op", args.OperationID, "error", resolveErr)
			actors = nil
		}
		resultBytes, err := marshalSpace(updated, args.OrgSlug, actors)
		if err != nil {
			return err
		}
		if _, err := qtx.CompleteOperation(ctx, db.CompleteOperationParams{
			ID:     args.OperationID,
			Result: resultBytes,
		}); err != nil {
			return err
		}

	case errors.Is(workErr, pgx.ErrNoRows):
		// Terminal: space left DELETE_REQUESTED (purged or restored
		// concurrently) or purge_time already elapsed. Mirrors the
		// legacy handler's FailedPrecondition mapping.
		if _, err := qtx.FailOperation(ctx, db.FailOperationParams{
			ID:           args.OperationID,
			ErrorCode:    pgtype.Int4{Int32: int32(codes.FailedPrecondition), Valid: true},
			ErrorMessage: pgtype.Text{String: "space is no longer in DELETE_REQUESTED state (was it purged or restored concurrently?)", Valid: true},
		}); err != nil {
			return err
		}

	default:
		w.Logger.ErrorContext(ctx, "lro_undelete_space: query failed", "op", args.OperationID, "space_id", args.SpaceID, "error", workErr)
		return workErr
	}

	if _, err := river.JobCompleteTx[*riverpgxv5.Driver](ctx, tx, job); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// resolveActors gathers audit-field UUIDs from the updated space row
// and asks the shared resolver to inflate them. nil-safe.
func (w *UndeleteSpaceWorker) resolveActors(ctx context.Context, space db.Space) (map[uuid.UUID]*typespb.Actor, error) {
	if w.Audit == nil {
		return nil, nil
	}
	var ids []uuid.UUID
	if space.CreatedBy.Valid {
		ids = append(ids, space.CreatedBy.Bytes)
	}
	if space.UpdatedBy.Valid {
		ids = append(ids, space.UpdatedBy.Bytes)
	}
	if space.DeletedBy.Valid {
		ids = append(ids, space.DeletedBy.Bytes)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	return w.Audit.Resolve(ctx, ids)
}

// marshalSpace packs the updated space proto into a JSON-marshaled
// google.protobuf.Any for storage in operations.result. Mirrors
// marshalOrg in lro_undelete_org.go — duplicated to keep workers
// independent of internal/lro internals.
func marshalSpace(space db.Space, orgSlug string, actors map[uuid.UUID]*typespb.Actor) (json.RawMessage, error) {
	pb := convert.SpaceToProto(space, orgSlug, actors)
	a, err := anypb.New(pb)
	if err != nil {
		return nil, err
	}
	b, err := protojson.Marshal(a)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}

var _ river.Worker[UndeleteSpaceArgs] = (*UndeleteSpaceWorker)(nil)
