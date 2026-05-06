package workers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
)

// VerifyDomainArgs is the River job input for the CreateDomain LRO's
// verification poll. The job runs a long-running poll loop in
// Work() — same shape as the legacy in-process goroutine but on
// pivox-worker. River Pro Workflows + Activities will replace this
// with a per-tick activity later; for now we accept the
// "worker-slot held for the full grace window" cost in exchange for
// getting the work off pivox-cloud.
type VerifyDomainArgs struct {
	OperationID  uuid.UUID `json:"operation_id"`
	DomainID     uuid.UUID `json:"domain_id"`
	OrgID        uuid.UUID `json:"org_id"`
	OrgSlug      string    `json:"org_slug"`
	Resource     string    `json:"resource"`
	Deadline     time.Time `json:"deadline"`
	PollInterval int64     `json:"poll_interval_ns"`
}

// Kind implements river.JobArgs. Stable string — changing it would
// orphan in-flight rows.
func (VerifyDomainArgs) Kind() string { return "lro_verify_domain" }

// Timeout overrides River's default per-job timeout. The poll loop
// can run for up to the verification grace window (7 days in
// production); set the timeout to deadline + a small buffer so
// River doesn't kill the job before the loop's own deadline check.
//
// Returning a negative value disables River's timeout entirely. We
// rely on Args.Deadline + ctx cancellation (Manager shutdown) for
// termination instead.
func (VerifyDomainArgs) Timeout(_ *river.Job[VerifyDomainArgs]) time.Duration { return -1 }

// VerifyDomainWorker handles CreateDomain LROs. Long-running:
// holds a worker slot for the duration of the verification grace
// window (or until DNS verifies). Operation completion is its own
// short tx (NOT atomic with River JobCompleteTx) — the work runs
// for too long to fit one tx.
type VerifyDomainWorker struct {
	river.WorkerDefaults[VerifyDomainArgs]

	Pool   *pgxpool.Pool
	Logger *slog.Logger
}

// Work implements river.Worker[VerifyDomainArgs]. Polls the domains
// row for state transitions until terminal (VERIFIED, FAILED) or
// the deadline elapses (EXPIRED). Returns nil on terminal so River
// marks the job done; returns an error to let River retry the
// entire poll loop on transient DB errors.
func (w *VerifyDomainWorker) Work(ctx context.Context, job *river.Job[VerifyDomainArgs]) error {
	args := job.Args
	pollInterval := time.Duration(args.PollInterval)
	if pollInterval <= 0 {
		// Defensive default — should always be set by the handler.
		// Match the production constant to keep behavior sane if a
		// caller forgets.
		pollInterval = 30 * time.Second
	}

	queries := db.New(w.Pool)
	t := time.NewTicker(pollInterval)
	defer t.Stop()

	var attempts int32
	check := func() (terminal bool, err error) {
		attempts++
		d, err := queries.GetDomainByID(ctx, db.GetDomainByIDParams{ID: args.DomainID, OrgID: args.OrgID})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// DeleteDomain ran while we were polling. Treat the
				// row's disappearance as a normal cancellation —
				// terminal failure on the operation row.
				return true, w.failOp(ctx, args.OperationID, codes.FailedPrecondition,
					"domain row was deleted; verification cancelled")
			}
			// Transient — let River retry the whole job.
			return false, err
		}
		switch d.State {
		case db.DomainStateVERIFIED:
			return true, w.completeVerified(ctx, args, d, attempts)
		case db.DomainStateFAILED:
			return true, w.failOp(ctx, args.OperationID, codes.FailedPrecondition,
				fmt.Sprintf("domain %q verification failed", d.Domain))
		}
		// PENDING. Check for grace expiry; if elapsed, mark the
		// row FAILED and complete the LRO with EXPIRED phase.
		if time.Now().After(args.Deadline) {
			if _, err := queries.MarkDomainFailed(ctx, args.DomainID); err != nil {
				w.Logger.ErrorContext(ctx, "lro_verify_domain: mark failed on expiry",
					"id", args.DomainID, "error", err)
			}
			w.updateMeta(ctx, args, apiv1.CreateDomainMetadata_EXPIRED, attempts)
			return true, w.failOp(ctx, args.OperationID, codes.FailedPrecondition,
				"domain verification window elapsed without successful DNS check")
		}
		// Still pending; surface progress.
		w.updateMeta(ctx, args, apiv1.CreateDomainMetadata_AWAITING_DNS, attempts)
		return false, nil
	}

	// First check fires immediately so a verify that completed
	// before the worker started is observed without waiting one
	// poll interval.
	if terminal, err := check(); terminal || err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			// Worker shutdown / job timeout. Return ctx.Err() so
			// River marks the job for retry; a fresh worker will
			// resume the poll loop. The operation row stays
			// pending until terminal.
			return ctx.Err()
		case <-t.C:
			if terminal, err := check(); terminal || err != nil {
				return err
			}
		}
	}
}

// completeVerified marshals the verified domain and marks the
// operation done. The legacy work fn returned a *apiv1.Domain;
// AIP-151 LRO completion stores it in operations.result as a
// google.protobuf.Any.
func (w *VerifyDomainWorker) completeVerified(ctx context.Context, args VerifyDomainArgs, d db.Domain, attempts int32) error {
	w.updateMeta(ctx, args, apiv1.CreateDomainMetadata_VERIFIED, attempts)
	resultBytes, err := marshalDomain(d, args.OrgSlug)
	if err != nil {
		return err
	}
	queries := db.New(w.Pool)
	if _, err := queries.CompleteOperation(ctx, db.CompleteOperationParams{
		ID:     args.OperationID,
		Result: resultBytes,
	}); err != nil {
		return err
	}
	return nil
}

// failOp marks the operation row with a terminal error code/message.
// Always returns nil — failures here would be doubled if propagated
// to River (which would retry; our work is already terminal).
func (w *VerifyDomainWorker) failOp(ctx context.Context, opID uuid.UUID, code codes.Code, msg string) error {
	queries := db.New(w.Pool)
	if _, err := queries.FailOperation(ctx, db.FailOperationParams{
		ID:           opID,
		ErrorCode:    pgtype.Int4{Int32: int32(code), Valid: true},
		ErrorMessage: pgtype.Text{String: msg, Valid: true},
	}); err != nil {
		w.Logger.ErrorContext(ctx, "lro_verify_domain: FailOperation write failed",
			"op", opID, "error", err)
	}
	return nil
}

// updateMeta refreshes operations.metadata so polling clients can
// observe attempts/last_check_time/phase. Best-effort — a failed
// metadata update doesn't fail the LRO.
func (w *VerifyDomainWorker) updateMeta(ctx context.Context, args VerifyDomainArgs, phase apiv1.CreateDomainMetadata_Phase, attempts int32) {
	meta := &apiv1.CreateDomainMetadata{
		Phase:         phase,
		Domain:        args.Resource,
		LastCheckTime: timestamppb.Now(),
		AttemptCount:  attempts,
	}
	a, err := anypb.New(meta)
	if err != nil {
		w.Logger.WarnContext(ctx, "lro_verify_domain: meta marshal failed", "error", err)
		return
	}
	b, err := protojson.Marshal(a)
	if err != nil {
		w.Logger.WarnContext(ctx, "lro_verify_domain: meta json failed", "error", err)
		return
	}
	queries := db.New(w.Pool)
	if err := queries.UpdateOperationMetadata(ctx, db.UpdateOperationMetadataParams{
		ID:       args.OperationID,
		Metadata: json.RawMessage(b),
	}); err != nil {
		w.Logger.WarnContext(ctx, "lro_verify_domain: meta write failed", "op", args.OperationID, "error", err)
	}
}

// marshalDomain packs the verified domain proto into operations.result.
func marshalDomain(d db.Domain, orgSlug string) (json.RawMessage, error) {
	pb := convert.DomainToProto(d, orgSlug, nil)
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

var _ river.Worker[VerifyDomainArgs] = (*VerifyDomainWorker)(nil)
