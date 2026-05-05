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
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/dashkan/pivox/internal/audit"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	typespb "github.com/dashkan/pivox/internal/pkg/gen/pivox/types"
)

// UndeleteOrgArgs is the River job input for the UndeleteOrganization
// LRO. The OperationID lets the worker mark the corresponding
// `operations` row done atomically with the SQL action; OrgID is the
// target org. Both are pre-generated and threaded through by the
// originating handler.
type UndeleteOrgArgs struct {
	OperationID uuid.UUID `json:"operation_id"`
	OrgID       uuid.UUID `json:"org_id"`
}

// Kind implements river.JobArgs. Stable string — changing it would
// orphan in-flight rows.
func (UndeleteOrgArgs) Kind() string { return "lro_undelete_org" }

// UndeleteOrgWorker handles UndeleteOrganization LROs. Single SQL
// step: flip the org back to ACTIVE, return the updated proto. The
// underlying query is race-safe (only fires on a row still in
// DELETE_REQUESTED with an unelapsed purge_time); concurrent attempts
// fall through pgx.ErrNoRows which we map to FailedPrecondition.
//
// Atomicity: the SQL action + operations-row completion + River
// job completion all run in one pgx tx via river.JobCompleteTx[*riverpgxv5.Driver].
// Either everything commits or nothing does — no "row updated but
// operation says pending" inconsistencies.
//
// Forward note: when we adopt River Pro Workflows, multi-step LROs
// (DeleteOrganization, etc.) will be expressed as workflow
// definitions whose activities ARE workers like this one. Keep the
// Work() body narrowly scoped to one logical step so each port
// translates cleanly into an activity.
type UndeleteOrgWorker struct {
	river.WorkerDefaults[UndeleteOrgArgs]

	Pool   *pgxpool.Pool
	Audit  *audit.Resolver
	Logger *slog.Logger
}

// Work implements river.Worker[UndeleteOrgArgs].
func (w *UndeleteOrgWorker) Work(ctx context.Context, job *river.Job[UndeleteOrgArgs]) error {
	args := job.Args

	tx, err := w.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after Commit

	qtx := db.New(tx)

	// Run the SQL action.
	updated, workErr := qtx.UndeleteOrganization(ctx, args.OrgID)

	// Decide outcome shape: success → CompleteOperation, terminal
	// failure → FailOperation, transient failure → return err so
	// River retries on its schedule.
	switch {
	case workErr == nil:
		// Success — marshal the updated org with audit actors.
		actors, resolveErr := w.resolveActors(ctx, updated)
		if resolveErr != nil {
			// Best-effort: if actor resolution fails, return without
			// actors rather than failing the operation.
			w.Logger.WarnContext(ctx, "lro_undelete_org: actor resolution failed; result emitted without actors",
				"op", args.OperationID, "error", resolveErr)
			actors = nil
		}
		resultBytes, err := marshalOrg(updated, actors)
		if err != nil {
			return err // transient/unexpected — let River retry
		}
		if _, err := qtx.CompleteOperation(ctx, db.CompleteOperationParams{
			ID:     args.OperationID,
			Result: resultBytes,
		}); err != nil {
			return err
		}

	case errors.Is(workErr, pgx.ErrNoRows):
		// Terminal: org is no longer eligible (left DELETE_REQUESTED,
		// or purge_time elapsed). Mirrors the legacy handler's
		// FailedPrecondition mapping.
		if _, err := qtx.FailOperation(ctx, db.FailOperationParams{
			ID:           args.OperationID,
			ErrorCode:    pgtype.Int4{Int32: int32(codes.FailedPrecondition), Valid: true},
			ErrorMessage: pgtype.Text{String: "organization is no longer eligible for undelete (purge window may have elapsed)", Valid: true},
		}); err != nil {
			return err
		}

	default:
		// Other DB errors — return so River retries. If the error is
		// genuinely permanent the retry budget will exhaust and the
		// job moves to discarded; we don't pre-judge here.
		w.Logger.ErrorContext(ctx, "lro_undelete_org: query failed", "op", args.OperationID, "org_id", args.OrgID, "error", workErr)
		return workErr
	}

	// Atomically complete the River job in the same tx.
	if _, err := river.JobCompleteTx[*riverpgxv5.Driver](ctx, tx, job); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// resolveActors gathers the audit-field UUIDs from the updated org
// row and asks the shared resolver to inflate them. nil-safe: if the
// resolver isn't wired (e.g., tests that don't care about actors),
// returns (nil, nil).
func (w *UndeleteOrgWorker) resolveActors(ctx context.Context, org db.Organization) (map[uuid.UUID]*typespb.Actor, error) {
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

// marshalOrg packs the updated org proto into a JSON-marshaled
// google.protobuf.Any for storage in operations.result. Mirrors
// internal/lro/convert.go marshalAny but is duplicated here to avoid
// a circular import — workers depend on lro for the args contract,
// not the other way around.
func marshalOrg(org db.Organization, actors map[uuid.UUID]*typespb.Actor) (json.RawMessage, error) {
	pb := convert.OrganizationToProto(org, actors)
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

// Compile-time check: UndeleteOrgWorker satisfies river.Worker.
var _ river.Worker[UndeleteOrgArgs] = (*UndeleteOrgWorker)(nil)

// Suppress "imported and not used" while a future change might drop
// proto imports — explicit reference keeps the import live for
// clarity in any future refactor.
var _ proto.Message = (*typespb.Actor)(nil)
