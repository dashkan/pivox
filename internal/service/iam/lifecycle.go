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
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/lro"
	iampb "github.com/dashkan/pivox/internal/pkg/gen/pivox/iam/v1"
	"github.com/dashkan/pivox/internal/server"
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

	return s.lroManager.CreateAndRun(ctx, req.GetName(), initialMeta,
		func(workCtx context.Context, progress lro.Progress) (proto.Message, error) {
			return s.runDeleteAccount(workCtx, progress, firebaseIdentityID)
		})
}

// runDeleteAccount executes the cross-org cascade. The
// identities row is hard-deleted second-to-last so a
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
	soleOwnerOrgs, err := s.queries.ListSoleOwnerOrgsForIdentity(ctx, convert.PgUUID(firebaseIdentityID))
	if err != nil {
		slog.ErrorContext(ctx, "delete account: sole-owner check failed",
			"identity_id", firebaseIdentityID, "error", err)
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
	// identity_id at the SQL level so the DELETE can't reach rows
	// that aren't this user's. Tx-wrapped so the org + space
	// revocations land atomically — without a tx, a partial failure
	// (e.g. session loss between the two DELETEs) leaves the
	// caller with no org_members but lingering space_members,
	// which is a confusing intermediate state for any retry
	// (the LRO resumes from the phase but a failed Phase 2 isn't
	// scoped narrower than "do all the revokes").
	updatePhase(iampb.DeleteAccountMetadata_REVOKING_MEMBERSHIPS)
	if err := db.RunInTxVoid(ctx, s.pool, func(qtx db.Querier) error {
		if err := qtx.DeleteOrgMembersForIdentity(ctx, convert.PgUUID(firebaseIdentityID)); err != nil {
			slog.ErrorContext(ctx, "delete account: revoke org members failed",
				"identity_id", firebaseIdentityID, "error", err)
			return apierr.Internal("revoke org memberships")
		}
		if err := qtx.DeleteSpaceMembersForIdentity(ctx, convert.PgUUID(firebaseIdentityID)); err != nil {
			slog.ErrorContext(ctx, "delete account: revoke space members failed",
				"identity_id", firebaseIdentityID, "error", err)
			return apierr.Internal("revoke space memberships")
		}
		return nil
	}); err != nil {
		return nil, err
	}

	// DELETING_PIVOX_RECORDS: capture the Firebase UID before
	// soft-deleting the identity row, so the next phase can call
	// auth.DeleteUser(uid). The identity row is preserved
	// (soft-deleted) so historical *_by audit references still
	// resolve — only the user-visible PII is blanked.
	//
	// Tx-wrapped: the lookup-then-soft-delete is a classic TOCTOU
	// pair. Without the tx, a sync-identity webhook (or a parallel
	// DeleteAccount retry) could soft-delete between our Get and
	// our SoftDelete; the SoftDelete then no-ops, our captured
	// firebase_uid is stale, and the next phase calls Firebase
	// DeleteUser on a possibly-mismatched uid. Inside the tx the
	// Get's row lock blocks concurrent mutations until our
	// SoftDelete commits.
	//
	// Resumption: if the LRO crashes between this tx commit and the
	// auth.DeleteUser call below, the persisted phase is still
	// DELETING_PIVOX_RECORDS and the next resume re-enters this
	// block. The identity row is already is_deleted=true; we detect
	// that on the GetIdentityByID and skip SoftDeleteIdentity. The
	// captured firebase_uid is still correct (soft delete blanks PII
	// but preserves firebase_uid), so the next phase can proceed.
	updatePhase(iampb.DeleteAccountMetadata_DELETING_PIVOX_RECORDS)
	identity, err := db.RunInTx(ctx, s.pool, func(qtx db.Querier) (db.Identity, error) {
		identity, err := qtx.GetIdentityByID(ctx, firebaseIdentityID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// Should not happen now that delete is soft —
				// GetIdentityByID returns soft-deleted rows too. Only
				// fires if an operator manually purged the row outside
				// of the LRO. Loud log + Internal so they reconcile.
				slog.ErrorContext(ctx, "delete account: identity row already purged outside the LRO — Firebase Auth account likely orphaned, manual cleanup required",
					"identity_id", firebaseIdentityID)
				return db.Identity{}, apierr.Internal(
					"identity already removed from Pivox but its Firebase Auth UID is unknown; operator must reconcile manually")
			}
			slog.ErrorContext(ctx, "delete account: lookup identity failed",
				"id", firebaseIdentityID, "error", err)
			return db.Identity{}, apierr.Internal("lookup identity")
		}
		if identity.IsDeleted {
			// LRO resumption: the previous attempt soft-deleted the
			// row but crashed before completing the Firebase phase.
			// firebase_uid is preserved through soft delete, so we
			// have what we need; skip the redundant UPDATE.
			slog.InfoContext(ctx, "delete account: identity already soft-deleted, resuming",
				"id", firebaseIdentityID)
			return identity, nil
		}
		// SoftDeleteIdentity returns the row's id when the UPDATE
		// actually landed; ErrNoRows means the row was soft-deleted
		// between our Get (which sees soft-deleted rows) and our
		// UPDATE (which excludes them). Inside this tx the Get took
		// a row lock so that's effectively impossible — but we still
		// surface it loudly rather than racing into auth.DeleteUser
		// with possibly-stale state.
		if _, err := qtx.SoftDeleteIdentity(ctx, firebaseIdentityID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				slog.ErrorContext(ctx, "delete account: soft-delete identity touched zero rows under tx — should be unreachable",
					"id", firebaseIdentityID)
				return db.Identity{}, apierr.Internal("soft-delete identity: race detected")
			}
			slog.ErrorContext(ctx, "delete account: soft-delete identity failed",
				"id", firebaseIdentityID, "error", err)
			return db.Identity{}, apierr.Internal("soft-delete identity")
		}
		return identity, nil
	})
	if err != nil {
		return nil, err
	}
	// PII just got blanked; drop any cached Actor for this id so the
	// next read on this instance sees the soft-deleted state
	// immediately. Other instances catch up via TTL expiry.
	if s.audit != nil {
		s.audit.Invalidate(firebaseIdentityID)
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
