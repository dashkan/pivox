package organizations

import (
	"context"
	"errors"
	"log/slog"

	"cloud.google.com/go/longrunning/autogen/longrunningpb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/proto"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/lro"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	"github.com/dashkan/pivox/internal/server"
)

// orgLifecyclePrefix is the longrunningpb operation-name prefix for
// org lifecycle LROs (Delete/Undelete). Reflected in the operation
// resource name so polling clients can filter by category.
const orgLifecyclePrefix = "organizations"

// DeleteOrganization soft-deletes (or, with `force=true`,
// synchronously purges) an organization. Returns an LRO whose
// metadata progresses through DeleteOrganizationMetadata.Phase as
// the orchestrator validates preconditions, cancels in-flight
// org-scoped operations, and completes the soft-delete or
// cascade-purge.
//
// Soft-delete (default): transitions the org to DELETE_REQUESTED,
// sets `delete_time = now()` and `purge_time = now() + 30d`, and
// returns. The actual cascade happens later via the purge worker
// once `purge_time` elapses. The slug stays reserved during the
// grace window — `organizations.name` is globally UNIQUE — so the
// caller can recover via UndeleteOrganization without slug
// collisions.
//
// Force: cancels in-flight org-scoped LROs (those that opted in by
// passing the org id to lro.CreateAndRunForOrg), then hard-deletes
// the org row. FK ON DELETE CASCADE removes spaces, members,
// domains, SSO config, assets, requests, tags, API keys, and AI
// conversations transitively. The slug is freed at completion.
//
// Cancellation scope: today the only LROs that opt in to org-
// scoped cancellation are those wired through CreateAndRunForOrg.
// DeleteOrganization itself passes NULL (to avoid self-cancelling
// in CANCELLING_OPERATIONS); other deferred LROs (asset imports,
// domain verifications, gateway upgrades) will populate org_id when
// they're implemented. Until then, in-flight non-org-scoped LROs
// either run to completion or fail naturally when their FK targets
// are cascaded away on the force path.
//
// Permission: `organizations.delete` (owner-only). The interceptor's
// soft-delete gate explicitly allows this permission against a
// DELETE_REQUESTED org so a re-delete during the grace window
// surfaces as FAILED_PRECONDITION at this handler rather than
// passing through unchanged. Force=true requires a non-empty etag
// pinning the row revision the client read.
func (s *OrganizationsServer) DeleteOrganization(ctx context.Context, req *apiv1.DeleteOrganizationRequest) (*longrunningpb.Operation, error) {
	resolved := server.MustResolvedOrgFromContext(ctx)
	org := resolved.Row

	if org.State != db.ResourceStateACTIVE {
		return nil, apierr.FailedPrecondition(
			"organization is not in ACTIVE state; current state is " + string(org.State))
	}
	// Etag is optional in general but REQUIRED for force=true: a
	// destructive op that bypasses the 30-day grace window must
	// pin the row's revision to the one the client read. Catches
	// "I clicked Delete on a stale view of the org" foot-guns.
	if req.GetForce() && req.GetEtag() == "" {
		return nil, apierr.FailedPrecondition(
			"force=true requires a non-empty etag pinning the org revision")
	}
	if req.GetEtag() != "" && req.GetEtag() != org.Etag {
		return nil, apierr.FailedPrecondition(
			"etag mismatch; refresh the organization and retry")
	}

	caller, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}

	orgName := "organizations/" + resolved.Slug
	deletedBy := convert.PgUUID(caller)
	force := req.GetForce()

	initialMeta := &apiv1.DeleteOrganizationMetadata{
		Phase:        apiv1.DeleteOrganizationMetadata_VALIDATING,
		Organization: orgName,
	}

	// Pin the etag at handler time so the LRO's PURGING phase refuses
	// to fire if the row has been mutated since (e.g., a concurrent
	// soft-delete + undelete cycle bumped revision). The handler
	// already requires force=true to supply a non-empty etag; here we
	// pass through the row's actual etag, since either the request
	// etag or the live etag works (they're equal once the etag-match
	// check above passes).
	expectedEtag := org.Etag

	return s.lroManager.CreateAndRun(ctx, orgLifecyclePrefix, initialMeta,
		func(workCtx context.Context, progress lro.Progress) (proto.Message, error) {
			return s.runDeleteOrganization(workCtx, progress, org.ID, orgName, deletedBy, force, expectedEtag)
		})
}

// runDeleteOrganization is the LRO orchestrator. Each phase
// transition reports progress via `progress` so polling clients
// observe the cascade. DB errors map to gRPC codes via apierr —
// the LRO Manager translates them to the operation's error field.
func (s *OrganizationsServer) runDeleteOrganization(
	ctx context.Context,
	progress lro.Progress,
	orgID uuid.UUID,
	orgName string, deletedBy pgtype.UUID,
	force bool,
	expectedEtag string,
) (proto.Message, error) {
	updatePhase := func(phase apiv1.DeleteOrganizationMetadata_Phase) {
		progress.Update(ctx, &apiv1.DeleteOrganizationMetadata{
			Phase:        phase,
			Organization: orgName,
		})
	}

	// CANCELLING_OPERATIONS: interrupt any in-flight org-scoped LROs
	// (asset imports, domain verifications, gateway upgrades, etc.)
	// so they don't try to mutate child rows we're about to
	// cascade-delete or orphan-soft-delete. Cancellation matches
	// rows where operations.org_id equals this org. LROs that didn't
	// opt in (org_id NULL — including this LRO and other
	// DeleteOrganization invocations) are unaffected; for the
	// force path the FK cascade still cleans them up. Future LROs
	// populate org_id when they're implemented.
	updatePhase(apiv1.DeleteOrganizationMetadata_CANCELLING_OPERATIONS)
	cancelledIDs, err := s.queries.CancelRunningOpsForOrg(ctx, pgtype.UUID{Bytes: orgID, Valid: true})
	if err != nil {
		slog.ErrorContext(ctx, "delete org: cancel in-flight ops failed", "org", orgName, "error", err)
		return nil, apierr.Internal("cancel in-flight operations")
	}
	// Fire local cancel funcs for any of the cancelled ops that are
	// running on this replica. The SQL update marks them done for
	// cross-replica observers; this stops the in-replica goroutines
	// from running to completion before noticing.
	if s.lroManager != nil && len(cancelledIDs) > 0 {
		s.lroManager.CancelLocal(cancelledIDs...)
	}

	if force {
		// PURGING: hard-delete the org. FK cascades handle children.
		// The slug is freed once the row is gone. Etag-guarded so a
		// concurrent state mutation between handler validation and
		// LRO worker execution refuses to purge a row the caller
		// didn't actually approve.
		updatePhase(apiv1.DeleteOrganizationMetadata_PURGING)
		if _, err := s.queries.PurgeOrganization(ctx, db.PurgeOrganizationParams{
			ID:   orgID,
			Etag: expectedEtag,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, apierr.FailedPrecondition(
					"organization revision changed since delete was requested; refresh and retry")
			}
			slog.ErrorContext(ctx, "delete org: purge failed", "org", orgName, "error", err)
			return nil, apierr.Internal("purge organization")
		}
		updatePhase(apiv1.DeleteOrganizationMetadata_COMPLETED)
		// Force path: the row is gone. AIP-151 LROs return the
		// resource as the result; with no row to return we surface
		// just the resource name. State is left at its zero value
		// (STATE_UNSPECIFIED) rather than synthesizing
		// DELETE_REQUESTED — there's no row to be in any state.
		return &apiv1.Organization{Name: orgName}, nil
	}

	// MARKING_DELETED: soft-delete path. Sets state +
	// delete_time/purge_time atomically. The query refuses to fire
	// on a non-ACTIVE row, so a race with a concurrent delete
	// surfaces as a no-rows-affected error.
	updatePhase(apiv1.DeleteOrganizationMetadata_MARKING_DELETED)
	updated, err := s.queries.SoftDeleteOrganization(ctx, db.SoftDeleteOrganizationParams{
		ID:        orgID,
		DeletedBy: deletedBy,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// State changed between handler-time validation and the
			// LRO worker firing (e.g. another worker raced us).
			return nil, apierr.FailedPrecondition(
				"organization state changed; cannot soft-delete (was it already deleted?)")
		}
		slog.ErrorContext(ctx, "delete org: soft-delete failed", "org", orgName, "error", err)
		return nil, apierr.Internal("soft-delete organization")
	}

	updatePhase(apiv1.DeleteOrganizationMetadata_COMPLETED)
	// State transition has committed. Treat actor resolution as a
	// best-effort enrichment so a transient identity-lookup failure
	// doesn't poison the LRO and force the client to retry an
	// operation that has already taken effect.
	actors, resolveErr := s.resolveOrgActors(ctx, []db.Organization{updated})
	if resolveErr != nil {
		slog.WarnContext(ctx, "delete org: actor resolution failed; returning proto without audit actors",
			"org", orgName, "error", resolveErr)
		actors = nil
	}
	return convert.OrganizationToProto(updated, actors), nil
}

// UndeleteOrganization restores a soft-deleted organization back to
// ACTIVE. Only callable during the 30-day grace window — once
// `purge_time` elapses the row is purged by the worker and there's
// no way back. Returns an LRO mirroring the AIP-164 shape; the
// actual work is a single UPDATE so the LRO completes promptly.
func (s *OrganizationsServer) UndeleteOrganization(ctx context.Context, req *apiv1.UndeleteOrganizationRequest) (*longrunningpb.Operation, error) {
	resolved := server.MustResolvedOrgFromContext(ctx)
	org := resolved.Row

	if org.State != db.ResourceStateDELETEREQUESTED {
		return nil, apierr.FailedPrecondition(
			"organization is not in DELETE_REQUESTED state; current state is " + string(org.State))
	}
	if req.GetEtag() != "" && req.GetEtag() != org.Etag {
		return nil, apierr.FailedPrecondition(
			"etag mismatch; refresh the organization and retry")
	}

	orgName := "organizations/" + resolved.Slug
	initialMeta := &apiv1.UndeleteOrganizationMetadata{Organization: orgName}

	return s.lroManager.CreateAndRun(ctx, orgLifecyclePrefix, initialMeta,
		func(workCtx context.Context, _ lro.Progress) (proto.Message, error) {
			updated, err := s.queries.UndeleteOrganization(workCtx, org.ID)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					// Either the org left DELETE_REQUESTED in a race
					// (e.g. force-delete completed) or its purge_time
					// elapsed before the worker fired.
					return nil, apierr.FailedPrecondition(
						"organization is no longer eligible for undelete (purge window may have elapsed)")
				}
				slog.ErrorContext(workCtx, "undelete org: query failed", "org", orgName, "error", err)
				return nil, apierr.Internal("undelete organization")
			}
			// Best-effort enrichment: state has committed, don't fail
			// the LRO on a transient identity lookup error.
			actors, resolveErr := s.resolveOrgActors(workCtx, []db.Organization{updated})
			if resolveErr != nil {
				slog.WarnContext(workCtx, "undelete org: actor resolution failed; returning proto without audit actors",
					"org", orgName, "error", resolveErr)
				actors = nil
			}
			return convert.OrganizationToProto(updated, actors), nil
		})
}
