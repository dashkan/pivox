package workers

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/dashkan/pivox/internal/audit"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
)

// DeleteAccountArgs is the River job input for the DeleteAccount
// LRO. Multi-phase orchestration runs in the worker's Work():
// VALIDATING (sole-owner check) → REVOKING_MEMBERSHIPS (drop org +
// space members) → DELETING_PIVOX_RECORDS (soft-delete identity).
//
// The Keycloak realm user is currently deleted out-of-band (via
// Keycloak's own admin flows / account console), so the LRO only owns
// the Pivox-side records.
//
// TODO(kc): add a phase to this LRO that deletes the Keycloak realm user
// via the KC Admin API, so account deletion is complete rather than
// requiring an out-of-band step. Make it replay-safe like the others —
// treat an already-absent KC user as success.
//
// Each phase does its own short tx; River's retry contract
// means the whole Work() may run more than once, so each phase is
// replay-safe (idempotent on already-applied state — see e.g. the
// soft-delete-already-tombstoned guard in Phase 3).
type DeleteAccountArgs struct {
	OperationID uuid.UUID `json:"operation_id"`
	IdentityID  uuid.UUID `json:"identity_id"`
}

// Kind implements river.JobArgs.
func (DeleteAccountArgs) Kind() string { return "lro_delete_account" }

// DeleteAccountWorker handles DeleteAccount LROs. Holds a worker
// slot for the duration of all phases (typically sub-second).
type DeleteAccountWorker struct {
	river.WorkerDefaults[DeleteAccountArgs]

	Pool   *pgxpool.Pool
	Audit  *audit.Resolver
	Logger *slog.Logger
}

// Work implements river.Worker[DeleteAccountArgs]. Returns nil on
// terminal completion (success or terminal failure recorded on the
// operations row). Returns non-nil to let River retry.
//
// Replay-safety notes (River retries the whole Work() on transient
// error):
//   - Phase 1 (sole-owner check): read-only, idempotent.
//   - Phase 2 (revoke memberships): DELETEs are naturally idempotent
//     (no-op if rows already gone).
//   - Phase 3 (soft-delete identity): tx detects identity.IsDeleted=true
//     and skips the UPDATE.
func (w *DeleteAccountWorker) Work(ctx context.Context, job *river.Job[DeleteAccountArgs]) error {
	args := job.Args
	queries := db.New(w.Pool)

	// Phase 1 — VALIDATING: sole-owner check across active orgs.
	soleOwnerOrgs, err := queries.ListSoleOwnerOrgsForIdentity(ctx, convert.PgUUID(args.IdentityID))
	if err != nil {
		w.Logger.ErrorContext(ctx, "lro_delete_account: sole-owner check failed",
			"identity_id", args.IdentityID, "error", err)
		return err
	}
	if len(soleOwnerOrgs) > 0 {
		names := make([]string, len(soleOwnerOrgs))
		for i, o := range soleOwnerOrgs {
			names[i] = "organizations/" + o.Name
		}
		return w.failOp(ctx, args.OperationID, codes.FailedPrecondition,
			"cannot delete account: caller is the sole owner of "+strings.Join(names, ", ")+
				"; transfer ownership or delete those orgs first")
	}

	// Phase 2 — REVOKING_MEMBERSHIPS: cross-org drop. Tx-wrapped so
	// the org + space revocations land atomically.
	if err := db.RunInTxVoid(ctx, w.Pool, func(qtx db.Querier) error {
		if err := qtx.DeleteOrgMembersForIdentity(ctx, convert.PgUUID(args.IdentityID)); err != nil {
			w.Logger.ErrorContext(ctx, "lro_delete_account: revoke org members failed",
				"identity_id", args.IdentityID, "error", err)
			return err
		}
		if err := qtx.DeleteSpaceMembersForIdentity(ctx, convert.PgUUID(args.IdentityID)); err != nil {
			w.Logger.ErrorContext(ctx, "lro_delete_account: revoke space members failed",
				"identity_id", args.IdentityID, "error", err)
			return err
		}
		return nil
	}); err != nil {
		return err
	}

	// Phase 3 — DELETING_PIVOX_RECORDS: soft-delete the identity row.
	// Tx-wrapped lookup-then-update to close the TOCTOU window.
	// is_deleted=true on the lookup indicates a previous attempt
	// already soft-deleted; we skip the UPDATE and proceed.
	if err := db.RunInTxVoid(ctx, w.Pool, func(qtx db.Querier) error {
		ident, err := qtx.GetIdentityByID(ctx, args.IdentityID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				w.Logger.ErrorContext(ctx, "lro_delete_account: identity row already purged outside the LRO",
					"identity_id", args.IdentityID)
				return errIdentityVanished
			}
			w.Logger.ErrorContext(ctx, "lro_delete_account: lookup identity failed",
				"id", args.IdentityID, "error", err)
			return err
		}
		if ident.IsDeleted {
			// Resumption: a previous attempt already soft-deleted; nothing
			// more to do for this phase.
			w.Logger.InfoContext(ctx, "lro_delete_account: identity already soft-deleted, resuming",
				"id", args.IdentityID)
			return nil
		}
		if _, err := qtx.SoftDeleteIdentity(ctx, args.IdentityID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				w.Logger.ErrorContext(ctx, "lro_delete_account: soft-delete identity touched zero rows under tx — should be unreachable",
					"id", args.IdentityID)
				return errSoftDeleteRace
			}
			w.Logger.ErrorContext(ctx, "lro_delete_account: soft-delete identity failed",
				"id", args.IdentityID, "error", err)
			return err
		}
		return nil
	}); err != nil {
		if errors.Is(err, errIdentityVanished) {
			return w.failOp(ctx, args.OperationID, codes.Internal,
				"identity already removed from Pivox; nothing to delete")
		}
		if errors.Is(err, errSoftDeleteRace) {
			return w.failOp(ctx, args.OperationID, codes.Internal,
				"soft-delete identity: race detected")
		}
		return err
	}

	// Drop any cached Actor for this id so the next read on this
	// process sees the soft-deleted state immediately.
	if w.Audit != nil {
		w.Audit.Invalidate(args.IdentityID)
	}

	// Operation complete — empty response, just like the legacy
	// runDeleteAccount.
	resultBytes, err := marshalEmpty(&emptypb.Empty{})
	if err != nil {
		return err
	}
	if _, err := queries.CompleteOperation(ctx, db.CompleteOperationParams{
		ID:     args.OperationID,
		Result: resultBytes,
	}); err != nil {
		return err
	}
	return nil
}

// failOp is the terminal-failure shortcut. Marks the operation
// row done with the given code/message and returns nil so River
// completes the job (rather than retrying — the failure is
// terminal).
func (w *DeleteAccountWorker) failOp(ctx context.Context, opID uuid.UUID, code codes.Code, msg string) error {
	queries := db.New(w.Pool)
	if _, err := queries.FailOperation(ctx, db.FailOperationParams{
		ID:           opID,
		ErrorCode:    pgtype.Int4{Int32: int32(code), Valid: true},
		ErrorMessage: pgtype.Text{String: msg, Valid: true},
	}); err != nil {
		w.Logger.ErrorContext(ctx, "lro_delete_account: FailOperation write failed",
			"op", opID, "error", err)
		return err
	}
	return nil
}

// errIdentityVanished surfaces the Phase 3 "row purged outside
// the LRO" branch out of the closure for the caller to map onto
// FailOperation.
var errIdentityVanished = errors.New("identity vanished")

// errSoftDeleteRace surfaces the Phase 3 "tx-bound race" branch
// out of the closure for the caller to map onto FailOperation.
// Should be unreachable under the tx's row lock; defensive.
var errSoftDeleteRace = errors.New("soft-delete race")

// marshalEmpty packs *emptypb.Empty into operations.result.
// DeleteAccount has no payload — the success signal IS completion.
func marshalEmpty(p *emptypb.Empty) ([]byte, error) {
	a, err := anypb.New(p)
	if err != nil {
		return nil, err
	}
	return protojson.Marshal(a)
}

var _ river.Worker[DeleteAccountArgs] = (*DeleteAccountWorker)(nil)
