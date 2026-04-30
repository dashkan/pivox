package convert

import (
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// UUIDString returns the canonical hyphenated string form of a
// pgtype.UUID, or empty when the value is NULL. Used by the convert
// layer to populate string-typed audit proto fields (`Creator`,
// `Updater`) from UUID-typed DB columns.
//
// This is a transitional helper: once issue #6 lands the Actor
// resolver, callers will inflate a structured `Actor` (id +
// display_name + email + is_deleted) instead of stuffing the bare
// UUID into a string. Keeping the surface narrow so the eventual
// migration is a search-and-replace rather than a refactor.
func UUIDString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	return u.String()
}

// PgUUID lifts a `uuid.UUID` into the pgtype shape sqlc expects on
// nullable UUID columns. Trivial wrapper kept here so handlers
// don't sprinkle `pgtype.UUID{Bytes: id, Valid: true}` literals
// everywhere.
func PgUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}
