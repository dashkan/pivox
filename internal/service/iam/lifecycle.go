package iam

import (
	"context"
	"log/slog"
	"strings"

	"cloud.google.com/go/longrunning/autogen/longrunningpb"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/lro"
	iampb "github.com/dashkan/pivox/internal/pkg/gen/pivox/iam/v1"
	"github.com/dashkan/pivox/internal/server"
	"github.com/dashkan/pivox/internal/workers"
)

// ===========================================================================
// DeleteUser — org-scoped removal of a user from one org. Sync;
// hard-delete. Recovery from a mistake is just re-creating the
// org_members row via Iam.CreateMember (the user's Pivox identity
// and content survive the membership removal because everything
// is keyed on `identities.id`).
// ===========================================================================

// DeleteUser hard-deletes the membership rows binding a user to a
// single organization. Touches `org_members`, `space_members` (for
// spaces in this org), and `group_members` (for groups in this
// org) — no LRO, no grace window. The user's Pivox account, their
// other-org memberships, and any content they own (audit columns
// reference `identities.id` which survives) are unaffected.
// Use Iam.DeleteAccount for global account deletion.
//
// Refuses if removing the user would leave the org with zero owners
// (counts user-owner principals AND active group-owner principals;
// a group-owner keeps the org covered).
//
// Permission: `users.delete` on the path's org (interceptor-gated).
// The {user} segment is `identities.id`.
func (s *IamServer) DeleteUser(ctx context.Context, req *iampb.DeleteUserRequest) (*emptypb.Empty, error) {
	resolvedOrg := server.MustResolvedOrgFromContext(ctx)
	userID, err := parseUserUUID(req.GetName(), resolvedOrg.Slug)
	if err != nil {
		return nil, err
	}

	// Sole-owner check + hard-delete cascade run inside a single tx.
	//
	// The check + cascade need atomicity: outside a tx, two
	// concurrent DeleteUser calls against different sibling owners
	// could both observe "remaining owners > 0" before either
	// commits, then both proceed and leave the org with zero
	// owners — directly defeating the precondition. Inside a tx
	// the row locks acquired by the DELETEs serialize concurrent
	// drops, and the count sees a consistent snapshot.
	//
	// The DELETE-cascade idempotency that the previous comment
	// relied on still holds; the tx is added for the read-then-
	// write invariant, not for the cascade itself.
	if err := db.RunInTxVoid(ctx, s.pool, func(qtx db.Querier) error {
		remainingOwners, err := qtx.CountOrgOwnersExcludingUser(ctx,
			db.CountOrgOwnersExcludingUserParams{OrgID: resolvedOrg.ID, UserID: convert.PgUUID(userID)})
		if err != nil {
			slog.ErrorContext(ctx, "delete user: sole-owner check failed",
				"org_id", resolvedOrg.ID, "user_id", userID, "error", err)
			return apierr.Internal("sole-owner check")
		}
		if remainingOwners == 0 {
			// Two cases: either the user IS the sole owner (refuse) or
			// the org has zero owners period (server-invariant violation
			// — surface as Internal). The latter shouldn't happen for an
			// active org since CreateOrganization establishes ≥1 owner.
			// Differentiating cleanly would need an extra query; for v1
			// we surface the more common case and keep the message
			// actionable.
			return apierr.FailedPrecondition(
				"cannot delete user: would leave the organization with no owners; transfer ownership first")
		}

		if err := qtx.DeleteOrgMembersForUserInOrg(ctx,
			db.DeleteOrgMembersForUserInOrgParams{OrgID: resolvedOrg.ID, UserID: convert.PgUUID(userID)}); err != nil {
			slog.ErrorContext(ctx, "delete user: revoke org members failed",
				"org_id", resolvedOrg.ID, "user_id", userID, "error", err)
			return apierr.Internal("revoke org memberships")
		}
		if err := qtx.DeleteSpaceMembersForUserInOrg(ctx,
			db.DeleteSpaceMembersForUserInOrgParams{OrgID: resolvedOrg.ID, UserID: convert.PgUUID(userID)}); err != nil {
			slog.ErrorContext(ctx, "delete user: revoke space members failed",
				"org_id", resolvedOrg.ID, "user_id", userID, "error", err)
			return apierr.Internal("revoke space memberships")
		}
		if err := qtx.DeleteGroupMembersForUserInOrg(ctx,
			db.DeleteGroupMembersForUserInOrgParams{OrgID: resolvedOrg.ID, UserID: userID}); err != nil {
			slog.ErrorContext(ctx, "delete user: revoke group memberships failed",
				"org_id", resolvedOrg.ID, "user_id", userID, "error", err)
			return apierr.Internal("revoke group memberships")
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

// parseUserUUID pulls the {user} part out of `organizations/{org}/
// users/{user}` and parses it as a UUID. Post-Phase-7 the {user}
// segment is `identities.id`. There's no v1 self-leave-org
// capability so the literal `me` doesn't get a special case here —
// uuid.Parse rejects it generically.
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
//     soft-delete the identities row (PII blanked, is_deleted=true).
//     The row itself stays so created_by/updated_by/deleted_by
//     references on other tables still resolve to an Actor proto;
//     org_members and space_members for this identity were already
//     removed in Phase 2 (REVOKING_MEMBERSHIPS), and group_members
//     ride on those (FK + same-tx).
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
	if s.lroManager == nil {
		// Read-only deployments construct IamServer with nil
		// LROManager; fail loudly here rather than null-deref'ing
		// inside the work fn.
		return nil, apierr.Internal("DeleteAccount is not configured on this server (lroManager dep missing)")
	}

	identityID := server.MustUserID(ctx)

	initialMeta := &iampb.DeleteAccountMetadata{
		Phase:   iampb.DeleteAccountMetadata_VALIDATING,
		Account: req.GetName(),
	}

	// River-backed: pivox-cloud enqueues + creates the operations
	// row in one tx; pivox-worker's DeleteAccountWorker runs the
	// cascade (sole-owner check, membership revocation, identity
	// soft-delete).
	opID := uuid.New()
	return s.lroManager.NewLro(ctx, req.GetName(), lro.NewLroOpts{
		OperationID: opID,
		// Account-scoped op (no org/space). created_by IS the authz
		// signal here: only the owner of accounts/me can read this op.
		CreatedBy: convert.PgUUID(identityID),
		JobArgs: workers.DeleteAccountArgs{
			OperationID: opID,
			IdentityID:  identityID,
		},
		Metadata: initialMeta,
	})
}
