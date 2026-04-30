package convert

import (
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	typespb "github.com/dashkan/pivox/internal/pkg/gen/pivox/types"
)

// actorOrNil looks up a pre-resolved Actor for the given pgtype.UUID
// and returns nil when the column is NULL or the lookup map is nil
// (e.g. partial responses that intentionally skip audit). Returning
// nil leaves the proto field unset rather than rendering an empty
// Actor envelope.
func actorOrNil(actors map[uuid.UUID]*typespb.Actor, u pgtype.UUID) *typespb.Actor {
	if !u.Valid || actors == nil {
		return nil
	}
	return actors[u.Bytes]
}

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
