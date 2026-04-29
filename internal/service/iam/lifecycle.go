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
	iampb "github.com/dashkan/pivox/internal/pkg/gen/pivox/iam/v1"
	"github.com/dashkan/pivox/internal/server"
)

// Operation prefixes. The user prefix covers the org-scoped
// DeleteUser LRO; account covers the global DeleteAccount LRO.
// Distinct prefixes let polling clients filter by operation class.
const (
	userLifecyclePrefix    = "users"
	accountLifecyclePrefix = "accounts"
)

// ===========================================================================
// DeleteUser — org-scoped removal of a user from one org.
// ===========================================================================

// DeleteUser removes a user from a single organization. The cascade
// touches only this org's bindings + the per-org users row; the
// user's Pivox account, Firebase Auth identity, and other-org
// memberships are not affected. Use Iam.DeleteAccount for global
// account deletion.
//
// Phases (DeleteUserMetadata.Phase):
//
//  1. VALIDATING — org-local sole-owner check via
//     CountOrgOwnersExcludingUser. If removing this user would leave
//     0 owners in this org, return FAILED_PRECONDITION.
//  2. REVOKING_MEMBERSHIPS — drop the user's org_members,
//     space_members (for spaces in this org), and group_members
//     (for groups in this org) rows.
//  3. SOFT_DELETING_USER — soft-delete the per-org users row with
//     30-day grace + purge_time. Mirrors the org's own soft-delete
//     pattern; the purge worker hard-deletes after grace.
//  4. COMPLETED.
//
// Permission: `users.delete` on the path's org (interceptor-gated).
// The literal `me` is not a valid {user} segment for this RPC —
// uuid.Parse fails with InvalidArgument.
func (s *IamServer) DeleteUser(ctx context.Context, req *iampb.DeleteUserRequest) (*longrunningpb.Operation, error) {
	resolvedOrg := server.MustResolvedOrgFromContext(ctx)
	userID, err := parseUserUUID(req.GetName(), resolvedOrg.Slug)
	if err != nil {
		return nil, err
	}

	// Lookup the user row to confirm it belongs to this org. The
	// query is org-bounded so a caller can't fish for users in
	// other orgs even if they have users.delete somewhere.
	user, err := s.queries.GetUserByID(ctx, db.GetUserByIDParams{
		ID:    userID,
		OrgID: resolvedOrg.ID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierr.NotFound("User", req.GetName())
		}
		slog.ErrorContext(ctx, "delete user: lookup user row failed",
			"user_id", userID, "org_id", resolvedOrg.ID, "error", err)
		return nil, apierr.Internal("lookup user")
	}

	if s.lroManager == nil {
		return nil, apierr.Internal("DeleteUser is not configured on this server (lroManager dep missing)")
	}

	initialMeta := &iampb.DeleteUserMetadata{
		Phase: iampb.DeleteUserMetadata_VALIDATING,
		User:  req.GetName(),
	}

	return s.lroManager.CreateAndRunForOrg(ctx, userLifecyclePrefix, resolvedOrg.ID, initialMeta,
		func(workCtx context.Context, progress lro.Progress) (proto.Message, error) {
			return s.runDeleteUser(workCtx, progress, resolvedOrg.ID, user.ID, req.GetName())
		})
}

// parseUserUUID pulls the {user} part out of `organizations/{org}/
// users/{user}` and parses it as a UUID. The literal `me` rejects
// here generically (uuid.Parse fails) — there's no v1 self-leave-org
// capability, so no special-casing.
func parseUserUUID(name, expectedOrg string) (uuid.UUID, error) {
	parts := strings.Split(name, "/")
	if len(parts) != 4 || parts[0] != "organizations" || parts[2] != "users" || parts[1] == "" || parts[3] == "" {
		return uuid.Nil, apierr.InvalidArgument(apierr.FieldViolation("name",
			"expected organizations/{org}/users/{user}"))
	}
	if parts[1] != expectedOrg {
		return uuid.Nil, apierr.InvalidArgument(apierr.FieldViolation("name",
			"org slug in path does not match resolved scope"))
	}
	id, err := uuid.Parse(parts[3])
	if err != nil {
		return uuid.Nil, apierr.InvalidArgument(apierr.FieldViolation("name",
			"user segment is not a valid UUID"))
	}
	return id, nil
}

// runDeleteUser is the org-scoped cascade orchestrator. Each phase
// is bounded to the path's org by query design — DeleteUser cannot
// reach into other orgs even if a future bug tried.
func (s *IamServer) runDeleteUser(
	ctx context.Context,
	progress lro.Progress,
	orgID, userID uuid.UUID,
	userName string,
) (proto.Message, error) {
	updatePhase := func(phase iampb.DeleteUserMetadata_Phase) {
		progress.Update(ctx, &iampb.DeleteUserMetadata{
			Phase: phase,
			User:  userName,
		})
	}

	// VALIDATING: org-local sole-owner check. Refuses if removing
	// this user would leave the org with zero owners. Counts both
	// user and group principals so a group-owner keeps the org
	// covered even when the only user-owner is being removed.
	updatePhase(iampb.DeleteUserMetadata_VALIDATING)
	remainingOwners, err := s.queries.CountOrgOwnersExcludingUser(ctx,
		db.CountOrgOwnersExcludingUserParams{OrgID: orgID, PrincipalID: userID})
	if err != nil {
		slog.ErrorContext(ctx, "delete user: sole-owner check failed",
			"org_id", orgID, "user_id", userID, "error", err)
		return nil, apierr.Internal("sole-owner check")
	}
	if remainingOwners == 0 {
		return nil, apierr.FailedPrecondition(
			"cannot delete user: would leave the organization with no owners; transfer ownership first")
	}

	// REVOKING_MEMBERSHIPS: drop org_members, space_members, and
	// group_members rows for this user, all bounded to this org by
	// the underlying query joins.
	updatePhase(iampb.DeleteUserMetadata_REVOKING_MEMBERSHIPS)
	if err := s.queries.DeleteOrgMembersForUserInOrg(ctx,
		db.DeleteOrgMembersForUserInOrgParams{OrgID: orgID, PrincipalID: userID}); err != nil {
		slog.ErrorContext(ctx, "delete user: revoke org members failed",
			"org_id", orgID, "user_id", userID, "error", err)
		return nil, apierr.Internal("revoke org memberships")
	}
	if err := s.queries.DeleteSpaceMembersForUserInOrg(ctx,
		db.DeleteSpaceMembersForUserInOrgParams{OrgID: orgID, PrincipalID: userID}); err != nil {
		slog.ErrorContext(ctx, "delete user: revoke space members failed",
			"org_id", orgID, "user_id", userID, "error", err)
		return nil, apierr.Internal("revoke space memberships")
	}
	if err := s.queries.DeleteGroupMembersForUserInOrg(ctx,
		db.DeleteGroupMembersForUserInOrgParams{OrgID: orgID, UserID: userID}); err != nil {
		slog.ErrorContext(ctx, "delete user: revoke group memberships failed",
			"org_id", orgID, "user_id", userID, "error", err)
		return nil, apierr.Internal("revoke group memberships")
	}

	// SOFT_DELETING_USER: 30-day grace + purge_time. The purge
	// worker hard-deletes after grace expires.
	updatePhase(iampb.DeleteUserMetadata_SOFT_DELETING_USER)
	if err := s.queries.SoftDeleteUserInOrg(ctx, userID); err != nil {
		slog.ErrorContext(ctx, "delete user: soft-delete users row failed",
			"user_id", userID, "error", err)
		return nil, apierr.Internal("soft-delete user")
	}

	updatePhase(iampb.DeleteUserMetadata_COMPLETED)
	return &emptypb.Empty{}, nil
}

// ===========================================================================
// DeleteAccount — global Pivox + Firebase account deletion (singleton).
// ===========================================================================

// DeleteAccount deletes the authenticated caller's global Pivox
// account, cascading through every org they're in, then deletes the
// underlying Firebase Auth identity. The path is always
// `accounts/me`; the caller is implicit from the auth context.
//
// On the membership-exempt list (no permission required) so a
// memberless caller stuck in a half-bootstrapped state can still
// recover by deleting their account.
//
// Cascade phases (DeleteAccountMetadata.Phase):
//
//  1. VALIDATING — cross-org sole-owner check. If the caller is
//     sole owner of any active org, return FAILED_PRECONDITION
//     listing them. Resolve via Organizations.TransferOwnership
//     or Organizations.DeleteOrganization on each.
//  2. REVOKING_MEMBERSHIPS — drop every org_members and
//     space_members row whose principal is a per-org users row
//     owned by this firebase_identity. Cross-org.
//  3. DELETING_PIVOX_RECORDS — capture the Firebase UID, then
//     hard-delete the firebase_identities row. ON DELETE CASCADE
//     removes per-org users + their group_members.
//  4. DELETING_FIREBASE_IDENTITY — Firebase Admin SDK DeleteUser.
//     Idempotent on already-deleted UIDs so retry-from-this-phase
//     is safe.
//  5. COMPLETED.
//
// Why Pivox owns this verb: Firebase Auth has no blocking
// pre-delete trigger, so server-side validation of the sole-owner
// check requires Pivox to be the entry point. The webhook for
// direct-Firebase-Console bypass is a separate fallback.
func (s *IamServer) DeleteAccount(ctx context.Context, req *iampb.DeleteAccountRequest) (*longrunningpb.Operation, error) {
	if req.GetName() != "accounts/me" {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("name",
			"expected accounts/me; the caller is implicit from authentication context"))
	}
	if s.lroManager == nil || s.auth == nil || s.caller == nil {
		// Read-only deployments construct IamServer with nil deps;
		// fail loudly here rather than null-deref'ing inside the
		// work fn.
		return nil, apierr.Internal("DeleteAccount is not configured on this server (auth/caller/lroManager deps missing)")
	}

	firebaseIdentityID, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}

	initialMeta := &iampb.DeleteAccountMetadata{
		Phase:   iampb.DeleteAccountMetadata_VALIDATING,
		Account: req.GetName(),
	}

	return s.lroManager.CreateAndRun(ctx, accountLifecyclePrefix, initialMeta,
		func(workCtx context.Context, progress lro.Progress) (proto.Message, error) {
			return s.runDeleteAccount(workCtx, progress, firebaseIdentityID)
		})
}

// runDeleteAccount executes the cross-org cascade. The
// firebase_identities row is hard-deleted second-to-last so a
// partial failure leaves a recoverable Firebase identity rather
// than orphaned Pivox state.
func (s *IamServer) runDeleteAccount(
	ctx context.Context,
	progress lro.Progress,
	firebaseIdentityID uuid.UUID,
) (proto.Message, error) {
	updatePhase := func(phase iampb.DeleteAccountMetadata_Phase) {
		progress.Update(ctx, &iampb.DeleteAccountMetadata{
			Phase:   phase,
			Account: "accounts/me",
		})
	}

	// VALIDATING: sole-owner check across every active org the
	// caller is in. The query also excludes orgs with active
	// group-owners (a group-owner keeps the org covered even when
	// this user is the sole user-owner — see the query's NOT
	// EXISTS clause).
	updatePhase(iampb.DeleteAccountMetadata_VALIDATING)
	soleOwnerOrgs, err := s.queries.ListSoleOwnerOrgsForFirebaseIdentity(ctx, firebaseIdentityID)
	if err != nil {
		slog.ErrorContext(ctx, "delete account: sole-owner check failed",
			"firebase_identity_id", firebaseIdentityID, "error", err)
		return nil, apierr.Internal("sole-owner check")
	}
	if len(soleOwnerOrgs) > 0 {
		names := make([]string, len(soleOwnerOrgs))
		for i, o := range soleOwnerOrgs {
			names[i] = "organizations/" + o.Name
		}
		return nil, apierr.FailedPrecondition(
			"cannot delete account: caller is the sole owner of " + strings.Join(names, ", ") +
				"; transfer ownership or delete those orgs first")
	}

	// REVOKING_MEMBERSHIPS: cross-org drop. Bounded by
	// firebase_identity_id at the SQL level so the DELETE can't
	// reach rows that aren't this user's.
	updatePhase(iampb.DeleteAccountMetadata_REVOKING_MEMBERSHIPS)
	if err := s.queries.DeleteOrgMembersForFirebaseIdentity(ctx, firebaseIdentityID); err != nil {
		slog.ErrorContext(ctx, "delete account: revoke org members failed",
			"firebase_identity_id", firebaseIdentityID, "error", err)
		return nil, apierr.Internal("revoke org memberships")
	}
	if err := s.queries.DeleteSpaceMembersForFirebaseIdentity(ctx, firebaseIdentityID); err != nil {
		slog.ErrorContext(ctx, "delete account: revoke space members failed",
			"firebase_identity_id", firebaseIdentityID, "error", err)
		return nil, apierr.Internal("revoke space memberships")
	}

	// DELETING_PIVOX_RECORDS: capture the Firebase UID before the
	// row is gone (next phase needs it for auth.DeleteUser), then
	// hard-delete. FK cascade removes per-org users +
	// group_members.
	updatePhase(iampb.DeleteAccountMetadata_DELETING_PIVOX_RECORDS)
	identity, err := s.queries.GetFirebaseIdentityByID(ctx, firebaseIdentityID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Pivox-side row already gone but auth.DeleteUser
			// hasn't run for this LRO — likely a partial earlier
			// run that died mid-cascade. We've lost the
			// firebase_uid and can't complete the cascade. Loud
			// log + Internal so operators reconcile by hand.
			slog.ErrorContext(ctx, "delete account: firebase_identity already gone but uid unknown — Firebase Auth account likely orphaned, manual cleanup required",
				"firebase_identity_id", firebaseIdentityID)
			return nil, apierr.Internal(
				"firebase identity already removed from Pivox but its Firebase Auth UID is unknown; operator must reconcile manually")
		}
		slog.ErrorContext(ctx, "delete account: lookup firebase_identity failed",
			"id", firebaseIdentityID, "error", err)
		return nil, apierr.Internal("lookup firebase identity")
	}
	if err := s.queries.HardDeleteFirebaseIdentity(ctx, firebaseIdentityID); err != nil {
		slog.ErrorContext(ctx, "delete account: hard-delete firebase_identity failed",
			"id", firebaseIdentityID, "error", err)
		return nil, apierr.Internal("delete firebase identity")
	}

	// DELETING_FIREBASE_IDENTITY: last so a failure leaves Pivox
	// state already cleaned up while the Firebase identity remains
	// recoverable. Implementation is idempotent on already-deleted
	// UIDs so a retry-from-this-phase is safe.
	updatePhase(iampb.DeleteAccountMetadata_DELETING_FIREBASE_IDENTITY)
	if err := s.auth.DeleteUser(ctx, identity.FirebaseUid); err != nil {
		slog.ErrorContext(ctx, "delete account: firebase auth deletion failed",
			"uid", identity.FirebaseUid, "error", err)
		return nil, apierr.Internal("delete firebase auth user")
	}

	updatePhase(iampb.DeleteAccountMetadata_COMPLETED)
	return &emptypb.Empty{}, nil
}
