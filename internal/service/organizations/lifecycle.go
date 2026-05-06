package organizations

import (
	"context"

	"cloud.google.com/go/longrunning/autogen/longrunningpb"
	"github.com/google/uuid"

	"github.com/dashkan/pivox/internal/apierr"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/lro"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	"github.com/dashkan/pivox/internal/server"
	"github.com/dashkan/pivox/internal/workers"
)

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

	// River-backed: pivox-cloud enqueues + creates the operations
	// row in one tx; pivox-worker's DeleteOrgWorker does CancelOps +
	// (Purge|Soft) + CompleteOperation atomically.
	//
	// Note: org_id is intentionally NOT set on the operations row
	// (NewLroOpts.OrgID left zero) — DeleteOrganization itself MUST
	// NOT self-cancel via CancelRunningOpsForOrg. Other LROs link
	// to the org via OrgID; this one doesn't.
	opID := uuid.New()
	return s.lroManager.NewLro(ctx, orgName, lro.NewLroOpts{
		OperationID: opID,
		JobArgs: workers.DeleteOrgArgs{
			OperationID:  opID,
			OrgID:        org.ID,
			Resource:     orgName,
			DeletedBy:    caller,
			Force:        force,
			ExpectedEtag: expectedEtag,
		},
		Metadata: initialMeta,
	})
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

	// First handler ported off the legacy CreateAndRun + runWork
	// goroutine path onto River (#69 Phase 5). The actual SQL action
	// runs in pivox-worker's UndeleteOrgWorker; pivox-cloud just
	// enqueues + returns the Operation row immediately. NewLro inserts
	// the operations row and the river_job row in one tx — atomic.
	opID := uuid.New()
	return s.lroManager.NewLro(ctx, orgName, lro.NewLroOpts{
		OperationID: opID,
		JobArgs:     workers.UndeleteOrgArgs{OperationID: opID, OrgID: org.ID},
		Metadata:    initialMeta,
	})
}
