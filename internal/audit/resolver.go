// Package audit resolves UUID audit fields (created_by, updated_by,
// deleted_by) into proto-friendly Actor messages by batching lookups
// against identities. A single Resolver is bound to the
// server lifetime and shared across handlers.
package audit

import (
	"context"

	"github.com/google/uuid"

	db "github.com/dashkan/pivox/internal/db/generated"
	typespb "github.com/dashkan/pivox/internal/pkg/gen/pivox/types"
)

// Resolver inflates audit-field UUIDs into Actor protos. The current
// implementation is a thin batched-lookup wrapper; caching is tracked
// as a follow-up.
type Resolver struct {
	queries db.Querier
}

// NewResolver constructs a Resolver bound to the given querier.
func NewResolver(queries db.Querier) *Resolver {
	return &Resolver{queries: queries}
}

// Resolve looks up the given identity IDs and returns a map keyed by
// id. Zero UUIDs are skipped before the DB call. IDs that resolve to
// no row are still returned in the map as `is_deleted=true`
// placeholders so callers can render audit fields without losing the
// reference.
func (r *Resolver) Resolve(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*typespb.Actor, error) {
	out := make(map[uuid.UUID]*typespb.Actor, len(ids))

	deduped := dedupeNonZero(ids)
	if len(deduped) == 0 {
		return out, nil
	}

	rows, err := r.queries.GetIdentitiesByIDs(ctx, deduped)
	if err != nil {
		return nil, err
	}

	for _, row := range rows {
		out[row.ID] = &typespb.Actor{
			Id:          row.ID.String(),
			DisplayName: row.DisplayName,
			Email:       row.Email,
		}
	}

	for _, id := range deduped {
		if _, ok := out[id]; ok {
			continue
		}
		out[id] = &typespb.Actor{Id: id.String(), IsDeleted: true}
	}

	return out, nil
}

// ResolveOne is a convenience for handlers that need a single Actor.
// Returns nil for the zero UUID so the caller can leave the proto
// field unset rather than render an empty Actor.
func (r *Resolver) ResolveOne(ctx context.Context, id uuid.UUID) (*typespb.Actor, error) {
	if id == uuid.Nil {
		return nil, nil
	}
	got, err := r.Resolve(ctx, []uuid.UUID{id})
	if err != nil {
		return nil, err
	}
	return got[id], nil
}

func dedupeNonZero(ids []uuid.UUID) []uuid.UUID {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[uuid.UUID]struct{}, len(ids))
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if id == uuid.Nil {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
