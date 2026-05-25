// Package identity holds cross-cutting helpers for the
// firebase-identity ↔ Pivox-identity binding. The cleanup helper
// here is the one place that knows how to tombstone an orphaned
// identity row + drop its memberships atomically — used by both
// `syncIdentity`'s defensive collision path and the periodic
// reconciliation worker so the two paths can't drift.
package identity

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
)

// TombstoneOrphaned soft-deletes the identity row and removes all
// its org_members + space_members rows in a single transaction.
// Idempotent: calling it against an already-tombstoned identity
// returns nil (the SoftDeleteIdentity predicate excludes tombstoned
// rows, surfaces pgx.ErrNoRows, which this function swallows). The
// final state matches what the DeleteAccount LRO produces in its
// REVOKING_MEMBERSHIPS + DELETING_PIVOX_RECORDS phases — the two
// paths converge.
//
// SECURITY: callers MUST first confirm via the auth provider that
// the corresponding Firebase user actually no longer exists.
// Tombstoning a still-active identity would clobber a real user's
// memberships and PII. Both call sites (`syncIdentity` and the
// reconciliation worker) gate this call on
// `authn.Service.UserExists` / `MissingUsers` reporting the UID as
// gone. Don't tombstone on a transient lookup failure.
func TombstoneOrphaned(ctx context.Context, pool db.TxBeginner, identityID uuid.UUID, logger *slog.Logger) error {
	return db.RunInTxVoid(ctx, pool, func(qtx db.Querier) error {
		// Drop memberships first so a partial failure leaves no
		// dangling rows pointing at a still-undeleted identity. The
		// reverse order — tombstone-then-drop — would leave the
		// memberships referencing a tombstoned row, which the audit
		// resolver renders as "deleted user" Actor placeholders;
		// harmless but noisy.
		if err := qtx.DeleteOrgMembersForIdentity(ctx, convert.PgUUID(identityID)); err != nil {
			logger.ErrorContext(ctx, "identity: drop org_members for orphan failed",
				"identity_id", identityID, "error", err)
			return err
		}
		if err := qtx.DeleteSpaceMembersForIdentity(ctx, convert.PgUUID(identityID)); err != nil {
			logger.ErrorContext(ctx, "identity: drop space_members for orphan failed",
				"identity_id", identityID, "error", err)
			return err
		}
		if _, err := qtx.SoftDeleteIdentity(ctx, identityID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// Already tombstoned — idempotent success.
				return nil
			}
			logger.ErrorContext(ctx, "identity: soft-delete orphan failed",
				"identity_id", identityID, "error", err)
			return err
		}
		logger.WarnContext(ctx, "identity: tombstoned orphaned identity",
			"identity_id", identityID)
		return nil
	})
}
