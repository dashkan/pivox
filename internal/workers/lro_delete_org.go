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

// DeleteOrgArgs is the River job input for the DeleteOrganization
// LRO. Force=true → CancelRunningOpsForOrg + PurgeOrganization
// (FK CASCADE hard-delete). Force=false →
// CancelRunningOpsForOrg + SoftDeleteOrganization (DELETE_REQUESTED
// + 30-day grace, then SpacePurgeWorker / OrgPurgeWorker cascades).
type DeleteOrgArgs struct {
	OperationID  uuid.UUID `json:"operation_id"`
	OrgID        uuid.UUID `json:"org_id"`
	Resource     string    `json:"resource"`
	DeletedBy    uuid.UUID `json:"deleted_by"`
	Force        bool      `json:"force"`
	ExpectedEtag string    `json:"expected_etag"`
}

// Kind implements river.JobArgs.
func (DeleteOrgArgs) Kind() string { return "lro_delete_org" }

// DeleteOrgWorker handles DeleteOrganization LROs. The whole
// orchestration runs in one pgx tx: CancelRunningOpsForOrg +
// (PurgeOrganization | SoftDeleteOrganization) + CompleteOperation
// + JobCompleteTx all commit together.
//
// Cancellation for long-running River jobs (e.g. VerifyDomainWorker)
// goes through the SQL update on operations.done, which the worker
// self-checks each tick.
type DeleteOrgWorker struct {
	river.WorkerDefaults[DeleteOrgArgs]

	Pool   *pgxpool.Pool
	Audit  *audit.Resolver
	Logger *slog.Logger
}

// Work implements river.Worker[DeleteOrgArgs].
func (w *DeleteOrgWorker) Work(ctx context.Context, job *river.Job[DeleteOrgArgs]) error {
	args := job.Args

	tx, err := w.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := db.New(tx)

	// CANCELLING_OPERATIONS: mark in-flight org-scoped LROs
	// cancelled. The DB update is the cross-replica signal —
	// long-running River workers (VerifyDomainWorker, future
	// org-scoped LROs) self-check operations.done each tick and
	// exit. Short-lived workers naturally complete inside the
	// race window we'd never have caught anyway.
	if _, err := qtx.CancelRunningOpsForOrg(ctx, pgtype.UUID{Bytes: args.OrgID, Valid: true}); err != nil {
		w.Logger.ErrorContext(ctx, "lro_delete_org: cancel in-flight ops failed",
			"org", args.Resource, "error", err)
		return err
	}

	if args.Force {
		// PURGING: FK ON DELETE CASCADE removes spaces, members,
		// domains, SSO config, assets, requests, tags, API keys,
		// AI conversations transitively. Etag-guarded.
		if _, err := qtx.PurgeOrganization(ctx, db.PurgeOrganizationParams{
			ID:   args.OrgID,
			Etag: args.ExpectedEtag,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				if _, err := qtx.FailOperation(ctx, db.FailOperationParams{
					ID:           args.OperationID,
					ErrorCode:    pgtype.Int4{Int32: int32(codes.FailedPrecondition), Valid: true},
					ErrorMessage: pgtype.Text{String: "organization revision changed since delete was requested; refresh and retry", Valid: true},
				}); err != nil {
					return err
				}
				if _, err := river.JobCompleteTx[*riverpgxv5.Driver](ctx, tx, job); err != nil {
					return err
				}
				return tx.Commit(ctx)
			}
			w.Logger.ErrorContext(ctx, "lro_delete_org: purge failed",
				"org", args.Resource, "error", err)
			return err
		}
		// Force path: row is gone. Surface the resource name only.
		resultBytes, err := marshalOrgProto(&apiv1.Organization{Name: args.Resource})
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

	// MARKING_DELETED: soft-delete. Refuses on non-ACTIVE row.
	updated, err := qtx.SoftDeleteOrganization(ctx, db.SoftDeleteOrganizationParams{
		ID:        args.OrgID,
		DeletedBy: pgtype.UUID{Bytes: args.DeletedBy, Valid: args.DeletedBy != uuid.Nil},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			if _, err := qtx.FailOperation(ctx, db.FailOperationParams{
				ID:           args.OperationID,
				ErrorCode:    pgtype.Int4{Int32: int32(codes.FailedPrecondition), Valid: true},
				ErrorMessage: pgtype.Text{String: "organization state changed; cannot soft-delete (was it already deleted?)", Valid: true},
			}); err != nil {
				return err
			}
			if _, err := river.JobCompleteTx[*riverpgxv5.Driver](ctx, tx, job); err != nil {
				return err
			}
			return tx.Commit(ctx)
		}
		w.Logger.ErrorContext(ctx, "lro_delete_org: soft-delete failed",
			"org", args.Resource, "error", err)
		return err
	}

	actors, resolveErr := w.resolveActors(ctx, updated)
	if resolveErr != nil {
		w.Logger.WarnContext(ctx, "lro_delete_org: actor resolution failed; result emitted without actors",
			"op", args.OperationID, "error", resolveErr)
		actors = nil
	}
	resultBytes, err := marshalOrg(updated, actors)
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

func (w *DeleteOrgWorker) resolveActors(ctx context.Context, org db.Organization) (map[uuid.UUID]*typespb.Actor, error) {
	if w.Audit == nil {
		return nil, nil
	}
	var ids []uuid.UUID
	if org.CreatedBy.Valid {
		ids = append(ids, org.CreatedBy.Bytes)
	}
	if org.UpdatedBy.Valid {
		ids = append(ids, org.UpdatedBy.Bytes)
	}
	if org.DeletedBy.Valid {
		ids = append(ids, org.DeletedBy.Bytes)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	return w.Audit.Resolve(ctx, ids)
}

// marshalOrgProto packs a freshly-built *apiv1.Organization into
// operations.result. Used by the force path which surfaces just
// the resource name.
func marshalOrgProto(p *apiv1.Organization) (json.RawMessage, error) {
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

var _ river.Worker[DeleteOrgArgs] = (*DeleteOrgWorker)(nil)
