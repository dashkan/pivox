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
	db "github.com/dashkan/pivox/internal/db/generated"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	typespb "github.com/dashkan/pivox/internal/pkg/gen/pivox/types"
)

// DeleteSpaceArgs is the River job input for the DeleteSpace LRO.
// Force=true drives the PURGING path (FK CASCADE hard-delete);
// Force=false drives the MARKING_DELETED path (soft-delete +
// 30-day grace window). DeletedBy is the identity uuid; OrgSlug is
// needed to render the result proto's name field.
type DeleteSpaceArgs struct {
	OperationID  uuid.UUID `json:"operation_id"`
	SpaceID      uuid.UUID `json:"space_id"`
	OrgSlug      string    `json:"org_slug"`
	Resource     string    `json:"resource"`
	DeletedBy    uuid.UUID `json:"deleted_by"`
	Force        bool      `json:"force"`
	ExpectedEtag string    `json:"expected_etag"`
}

// Kind implements river.JobArgs.
func (DeleteSpaceArgs) Kind() string { return "lro_delete_space" }

// DeleteSpaceWorker handles DeleteSpace LROs. Single SQL action per
// branch (force=PurgeSpace, soft=SoftDeleteSpace), so the action +
// CompleteOperation + JobCompleteTx all run in one pgx tx.
type DeleteSpaceWorker struct {
	river.WorkerDefaults[DeleteSpaceArgs]

	Pool   *pgxpool.Pool
	Audit  *audit.Resolver
	Logger *slog.Logger
}

// Work implements river.Worker[DeleteSpaceArgs].
func (w *DeleteSpaceWorker) Work(ctx context.Context, job *river.Job[DeleteSpaceArgs]) error {
	args := job.Args

	tx, err := w.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := db.New(tx)

	if args.Force {
		// PURGING: FK ON DELETE CASCADE removes space_members,
		// assets, requests transitively. Etag-guarded: a concurrent
		// state mutation between handler validation and worker
		// execution surfaces ErrNoRows here.
		if _, err := qtx.PurgeSpace(ctx, db.PurgeSpaceParams{
			ID:   args.SpaceID,
			Etag: args.ExpectedEtag,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				if _, err := qtx.FailOperation(ctx, db.FailOperationParams{
					ID:           args.OperationID,
					ErrorCode:    pgtype.Int4{Int32: int32(codes.FailedPrecondition), Valid: true},
					ErrorMessage: pgtype.Text{String: "space revision changed since delete was requested; refresh and retry", Valid: true},
				}); err != nil {
					return err
				}
				if _, err := river.JobCompleteTx[*riverpgxv5.Driver](ctx, tx, job); err != nil {
					return err
				}
				return tx.Commit(ctx)
			}
			w.Logger.ErrorContext(ctx, "lro_delete_space: purge failed",
				"space", args.Resource, "error", err)
			return err
		}
		// Force path: row is gone. Surface the resource name only.
		resultBytes, err := marshalSpaceProto(&apiv1.Space{Name: args.Resource})
		if err != nil {
			return err
		}
		if _, err := qtx.CompleteOperation(ctx, db.CompleteOperationParams{
			ID:     args.OperationID,
			Result: resultBytes,
		}); err != nil {
			return err
		}
		if _, err := river.JobCompleteTx[*riverpgxv5.Driver](ctx, tx, job); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}

	// MARKING_DELETED: soft-delete. The query refuses to fire on a
	// non-ACTIVE row (delete_time IS NULL guard) so a race with a
	// concurrent delete surfaces ErrNoRows.
	updated, err := qtx.SoftDeleteSpace(ctx, db.SoftDeleteSpaceParams{
		ID:        args.SpaceID,
		DeletedBy: pgtype.UUID{Bytes: args.DeletedBy, Valid: args.DeletedBy != uuid.Nil},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			if _, err := qtx.FailOperation(ctx, db.FailOperationParams{
				ID:           args.OperationID,
				ErrorCode:    pgtype.Int4{Int32: int32(codes.FailedPrecondition), Valid: true},
				ErrorMessage: pgtype.Text{String: "space state changed; cannot soft-delete (was it already deleted?)", Valid: true},
			}); err != nil {
				return err
			}
			if _, err := river.JobCompleteTx[*riverpgxv5.Driver](ctx, tx, job); err != nil {
				return err
			}
			return tx.Commit(ctx)
		}
		w.Logger.ErrorContext(ctx, "lro_delete_space: soft-delete failed",
			"space", args.Resource, "error", err)
		return err
	}

	actors, resolveErr := w.resolveActors(ctx, updated)
	if resolveErr != nil {
		w.Logger.WarnContext(ctx, "lro_delete_space: actor resolution failed; result emitted without actors",
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
	if _, err := river.JobCompleteTx[*riverpgxv5.Driver](ctx, tx, job); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (w *DeleteSpaceWorker) resolveActors(ctx context.Context, space db.Space) (map[uuid.UUID]*typespb.Actor, error) {
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

// marshalSpaceProto packs an arbitrary *apiv1.Space (e.g. force-path
// stub with only Name set) into operations.result. Mirrors
// marshalSpace which marshals from a db.Space via SpaceToProto.
func marshalSpaceProto(p *apiv1.Space) (json.RawMessage, error) {
	a, err := anypb.New(p)
	if err != nil {
		return nil, err
	}
	b, err := protojson.Marshal(a)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}

var _ river.Worker[DeleteSpaceArgs] = (*DeleteSpaceWorker)(nil)
