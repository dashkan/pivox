package iam

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"cloud.google.com/go/longrunning/autogen/longrunningpb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/dashkan/pivox/internal/apierr"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/lro"
	"github.com/dashkan/pivox/internal/permission"
	iampb "github.com/dashkan/pivox/internal/pkg/gen/pivox/iam/v1"
	"github.com/dashkan/pivox/internal/server"
)

// userLifecyclePrefix is the longrunningpb operation-name prefix
// for the DeleteUser LRO. Reflected in the operation resource name
// so polling clients can filter by category.
const userLifecyclePrefix = "users"

// DeleteUser is the global user-deletion LRO. The path is
// org-scoped (`organizations/{org}/users/{user}` — any org where
// the caller has `users.delete` is a valid entry point) but the
// cascade reaches every org the underlying firebase_identity is a
// member of. Use `users/me` for self-delete.
//
// Phases (DeleteUserMetadata.Phase):
//
//  1. VALIDATING — sole-owner check. If the firebase_identity is
//     the sole owner of any active org, return FAILED_PRECONDITION
//     listing the affected orgs. The caller resolves via
//     Organizations.TransferOwnership or
//     Organizations.DeleteOrganization first.
//  2. REVOKING_MEMBERSHIPS — removes all org_members and
//     space_members rows whose principal is a user owned by this
//     firebase_identity. (group_members rows cascade naturally
//     when the users rows go away in the next phase.)
//  3. DELETING_PIVOX_RECORDS — captures the Firebase UID, then
//     hard-deletes the firebase_identities row. ON DELETE CASCADE
//     removes per-org users rows transitively, and group_members
//     via the users FK.
//  4. DELETING_FIREBASE_IDENTITY — calls auth.DeleteUser(uid). The
//     Firebase Admin SDK call is idempotent (no error for already-
//     deleted UIDs) so a transient failure here is retry-safe.
//  5. COMPLETED.
//
// The Firebase identity is deleted LAST so a partial failure leaves
// a recoverable provider account rather than orphaned Pivox state.
//
// Permission: `users.delete` on the path's org. The interceptor's
// soft-delete gate explicitly allows this perm against a
// DELETE_REQUESTED org, but the handler doesn't carry an etag check
// today — the underlying record is immutable across the cascade
// once the LRO starts.
func (s *IamServer) DeleteUser(ctx context.Context, req *iampb.DeleteUserRequest) (*longrunningpb.Operation, error) {
	resolvedOrg := server.MustResolvedOrgFromContext(ctx)
	userSegment, err := parseUserSegment(req.GetName(), resolvedOrg.Slug)
	if err != nil {
		return nil, err
	}

	firebaseIdentityID, err := s.resolveFirebaseIdentityID(ctx, resolvedOrg.ID, userSegment)
	if err != nil {
		return nil, err
	}

	if s.lroManager == nil || s.auth == nil || s.caller == nil {
		// Fail loudly on a misconfigured wiring rather than null-
		// dereffing inside the work fn. Read-only deployments that
		// don't need DeleteUser construct IamServer with nil deps;
		// this guard surfaces the misconfiguration after path
		// validation runs (so caller-input errors take precedence).
		return nil, apierr.Internal("DeleteUser is not configured on this server (auth/caller/lroManager deps missing)")
	}

	// Two-tier permission. The proto annotation gates the RPC with
	// users.deleteSelf (granted to all roles) so any member can
	// leave. When the target is NOT the caller, escalate to
	// users.delete (owner-only) — that's the destructive verb
	// against arbitrary users. resolveFirebaseIdentityID has
	// already resolved "me" → caller's identity, so a UID match
	// here is the self-target signal.
	callerID, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	if firebaseIdentityID != callerID {
		if s.resolver == nil {
			return nil, apierr.Internal("DeleteUser requires permission resolver for cross-user deletes")
		}
		ok, err := s.resolver.HasPermission(ctx, callerID,
			permission.OrgTarget(resolvedOrg.ID), permission.UsersDelete)
		if err != nil {
			slog.ErrorContext(ctx, "delete user: resolve users.delete permission failed",
				"caller", callerID, "org_id", resolvedOrg.ID, "error", err)
			return nil, apierr.Internal("evaluate users.delete permission")
		}
		if !ok {
			return nil, apierr.PermissionDenied(
				"users.delete is required to delete another user; only the caller's own account can be deleted with this role")
		}
	}

	initialMeta := &iampb.DeleteUserMetadata{
		Phase: iampb.DeleteUserMetadata_VALIDATING,
		User:  req.GetName(),
	}

	return s.lroManager.CreateAndRun(ctx, userLifecyclePrefix, initialMeta,
		func(workCtx context.Context, progress lro.Progress) (proto.Message, error) {
			return s.runDeleteUser(workCtx, progress, firebaseIdentityID, req.GetName())
		})
}

// parseUserSegment pulls the `{user}` part out of a path of the
// form `organizations/{org}/users/{user}`. The org slug is checked
// against the interceptor-resolved scope to defend against name
// rewrites between gate-time and handler-time.
func parseUserSegment(name, expectedOrg string) (string, error) {
	parts := strings.Split(name, "/")
	if len(parts) != 4 || parts[0] != "organizations" || parts[2] != "users" || parts[1] == "" || parts[3] == "" {
		return "", apierr.InvalidArgument(apierr.FieldViolation("name",
			"expected organizations/{org}/users/{user_or_me}"))
	}
	if parts[1] != expectedOrg {
		return "", apierr.InvalidArgument(apierr.FieldViolation("name",
			"org slug in path does not match resolved scope"))
	}
	return parts[3], nil
}

// resolveFirebaseIdentityID maps the path's `{user}` segment to the
// firebase_identities row to delete. Two cases:
//
//   - "me": the caller's own identity, looked up via the supplied
//     CallerIdentityResolver. Self-delete bypasses the user-id
//     lookup entirely so a caller without a per-org users row in
//     the path's org can still self-delete (legitimate during
//     bootstrap-account-cleanup flows).
//   - <UUID>: a per-org users.id, scoped to the path's org. The
//     query refuses cross-org user lookups so a caller can't fish
//     for users across the boundary even if they have users.delete
//     in some org.
func (s *IamServer) resolveFirebaseIdentityID(ctx context.Context, orgID uuid.UUID, userSegment string) (uuid.UUID, error) {
	if userSegment == "me" {
		callerID, err := s.caller(ctx)
		if err != nil {
			return uuid.Nil, err
		}
		return callerID, nil
	}
	userID, err := uuid.Parse(userSegment)
	if err != nil {
		return uuid.Nil, apierr.InvalidArgument(apierr.FieldViolation("name",
			"user segment is neither 'me' nor a valid UUID"))
	}
	user, err := s.queries.GetUserByID(ctx, db.GetUserByIDParams{ID: userID, OrgID: orgID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, apierr.NotFound("User", userSegment)
		}
		slog.ErrorContext(ctx, "delete user: lookup user row failed", "user_id", userID, "org_id", orgID, "error", err)
		return uuid.Nil, apierr.Internal("lookup user")
	}
	return user.FirebaseIdentityID, nil
}

// runDeleteUser is the LRO orchestrator. Phase transitions report
// progress via `progress` so polling clients observe the cascade.
func (s *IamServer) runDeleteUser(
	ctx context.Context,
	progress lro.Progress,
	firebaseIdentityID uuid.UUID,
	userName string,
) (proto.Message, error) {
	updatePhase := func(phase iampb.DeleteUserMetadata_Phase) {
		progress.Update(ctx, &iampb.DeleteUserMetadata{
			Phase: phase,
			User:  userName,
		})
	}

	// VALIDATING: sole-owner check. If this user is the only owner
	// of any active org, deletion would leave that org without an
	// owner — refuse with FAILED_PRECONDITION pointing at the
	// affected orgs. The caller fixes the situation via
	// Organizations.TransferOwnership or DeleteOrganization first.
	//
	// Race window: a concurrent owner-demotion or owner-removal in
	// another org between this check and REVOKING_MEMBERSHIPS could
	// turn the caller into a new sole-owner mid-flight. The
	// "≥1 owner per org" boundary check on Member mutations
	// (`CountOwnersByOrg`) protects against losing the last owner
	// in steady state by counting our user-as-still-alive at the
	// concurrent demote's transaction time. v1 accepts the
	// theoretical window between Member's tx commit and our
	// REVOKING_MEMBERSHIPS step; concurrent promote/demote storms
	// hitting deletion are not a realistic v1 load shape.
	updatePhase(iampb.DeleteUserMetadata_VALIDATING)
	soleOwnerOrgs, err := s.queries.ListSoleOwnerOrgsForFirebaseIdentity(ctx, firebaseIdentityID)
	if err != nil {
		slog.ErrorContext(ctx, "delete user: sole-owner check failed", "firebase_identity_id", firebaseIdentityID, "error", err)
		return nil, apierr.Internal("sole-owner check")
	}
	if len(soleOwnerOrgs) > 0 {
		names := make([]string, len(soleOwnerOrgs))
		for i, o := range soleOwnerOrgs {
			names[i] = "organizations/" + o.Name
		}
		return nil, apierr.FailedPrecondition(
			"cannot delete user: caller is the sole owner of " + strings.Join(names, ", ") +
				"; transfer ownership or delete those orgs first")
	}

	// REVOKING_MEMBERSHIPS: drop all org-scope and space-scope role
	// bindings whose principal is a user owned by this firebase
	// identity. group_members rows survive briefly — they cascade
	// in DELETING_PIVOX_RECORDS when the users rows go away.
	updatePhase(iampb.DeleteUserMetadata_REVOKING_MEMBERSHIPS)
	if err := s.queries.DeleteOrgMembersForFirebaseIdentity(ctx, firebaseIdentityID); err != nil {
		slog.ErrorContext(ctx, "delete user: revoke org members failed", "firebase_identity_id", firebaseIdentityID, "error", err)
		return nil, apierr.Internal("revoke org memberships")
	}
	if err := s.queries.DeleteSpaceMembersForFirebaseIdentity(ctx, firebaseIdentityID); err != nil {
		slog.ErrorContext(ctx, "delete user: revoke space members failed", "firebase_identity_id", firebaseIdentityID, "error", err)
		return nil, apierr.Internal("revoke space memberships")
	}

	// DELETING_PIVOX_RECORDS: capture the Firebase UID before the
	// row is gone (the next phase needs it for auth.DeleteUser),
	// then hard-delete. Cascade removes per-org users + their
	// group_members.
	updatePhase(iampb.DeleteUserMetadata_DELETING_PIVOX_RECORDS)
	identity, err := s.queries.GetFirebaseIdentityByID(ctx, firebaseIdentityID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Pivox-side row already gone but auth.DeleteUser hasn't
			// run for this LRO — likely a partial earlier run that
			// died between HardDeleteFirebaseIdentity and the final
			// Firebase Auth call. We've lost the firebase_uid and
			// can't complete the cascade. Surface as Internal with a
			// loud log so operators clean up the dangling Firebase
			// account by hand. Returning success here would silently
			// strand a fully-functional Firebase identity that
			// matches no Pivox row — a data-leak class bug for a
			// deletion API.
			slog.ErrorContext(ctx, "delete user: firebase_identity already gone but uid unknown — Firebase Auth account likely orphaned, manual cleanup required",
				"firebase_identity_id", firebaseIdentityID,
				"user", userName)
			return nil, apierr.Internal(
				"firebase identity already removed from Pivox but its Firebase Auth UID is unknown; operator must reconcile manually")
		}
		slog.ErrorContext(ctx, "delete user: lookup firebase_identity failed", "id", firebaseIdentityID, "error", err)
		return nil, apierr.Internal("lookup firebase identity")
	}
	if err := s.queries.HardDeleteFirebaseIdentity(ctx, firebaseIdentityID); err != nil {
		slog.ErrorContext(ctx, "delete user: hard-delete firebase_identity failed", "id", firebaseIdentityID, "error", err)
		return nil, apierr.Internal("delete firebase identity")
	}

	// DELETING_FIREBASE_IDENTITY: last so a failure leaves Pivox
	// state already cleaned up while the Firebase identity remains
	// recoverable. The Firebase impl is idempotent on already-
	// deleted UIDs, so a retry-from-this-phase is safe.
	updatePhase(iampb.DeleteUserMetadata_DELETING_FIREBASE_IDENTITY)
	if err := s.auth.DeleteUser(ctx, identity.FirebaseUid); err != nil {
		slog.ErrorContext(ctx, "delete user: firebase auth deletion failed", "uid", identity.FirebaseUid, "error", err)
		return nil, apierr.Internal("delete firebase auth user")
	}

	updatePhase(iampb.DeleteUserMetadata_COMPLETED)
	return &emptypb.Empty{}, nil
}
