// Package audit resolves UUID audit fields (created_by, updated_by,
// deleted_by) into proto-friendly Actor messages by batching lookups
// against identities. A single Resolver is bound to the server
// lifetime and shared across handlers.
//
// Caching: actor lookups are read-heavy and the underlying data
// (display_name, photo_url, email, is_deleted) mutates rarely. The
// resolver keeps an in-process LRU keyed by identity UUID, populated
// lazily on the first miss for each id. TTL bounds staleness across
// multi-instance deploys: a mutation on instance A invalidates A's
// cache locally; instance B sees the stale entry for at most TTL
// before it expires. For the actor-shaped fields this is invisible
// UX-wise (display_name going stale for 60s is a non-event), so
// in-process LRU is the right shape — no Redis dep, no inter-process
// coordination, no extra network hop on the hot path. Mutation
// handlers (UpdateUser, DeleteAccount, syncIdentity webhook) call
// `Invalidate` to drop stale entries on the local instance
// immediately; remote instances catch up via TTL expiry.
package audit

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/golang-lru/v2/expirable"

	db "github.com/dashkan/pivox/internal/db/generated"
	typespb "github.com/dashkan/pivox/internal/pkg/gen/pivox/types"
)

// Default cache parameters. 10k entries × ~200 bytes per Actor =
// ~2MB worst-case footprint per instance — trivial. 60s TTL is a
// pragmatic ceiling on cross-instance staleness for human-perceptible
// metadata (display_name, photo_url).
const (
	DefaultCacheSize = 10_000
	DefaultCacheTTL  = 60 * time.Second
)

// Config is the constructor input for Resolver.
type Config struct {
	// Queries is the sqlc query interface. Required.
	Queries db.Querier
	// CacheSize caps the in-process LRU. Zero ⇒ DefaultCacheSize.
	// Negative ⇒ disable caching (resolver behaves as a pure
	// batched-lookup wrapper, useful in tests that assert query
	// counts).
	CacheSize int
	// CacheTTL bounds per-entry staleness. Zero ⇒ DefaultCacheTTL.
	// Negative ⇒ disable caching (same as CacheSize < 0).
	CacheTTL time.Duration
}

// Resolver inflates audit-field UUIDs into Actor protos. Safe for
// concurrent use; the underlying LRU is goroutine-safe.
type Resolver struct {
	queries db.Querier
	// cache may be nil — caching is disabled when CacheSize or
	// CacheTTL is negative. A nil cache makes Resolve degrade
	// cleanly into the original batched-lookup behavior.
	cache *expirable.LRU[uuid.UUID, *typespb.Actor]
}

// NewResolver constructs a Resolver from cfg. Panics if Queries is
// nil — startup-time programmer error, fail loud on boot rather than
// nil-deref mid-RPC.
func NewResolver(cfg Config) *Resolver {
	if cfg.Queries == nil {
		panic("audit: Config.Queries is required")
	}

	size := cfg.CacheSize
	ttl := cfg.CacheTTL
	if size == 0 {
		size = DefaultCacheSize
	}
	if ttl == 0 {
		ttl = DefaultCacheTTL
	}

	r := &Resolver{queries: cfg.Queries}
	if size > 0 && ttl > 0 {
		// onEvict is unused — LRU evictions are silent. We don't
		// emit a metric here because the cache is small and reads
		// are dominant; eviction frequency is a poor signal for
		// operators compared with hit-rate (left as a follow-up if
		// needed).
		r.cache = expirable.NewLRU[uuid.UUID, *typespb.Actor](size, nil, ttl)
	}
	return r
}

// Resolve looks up the given identity IDs and returns a map keyed by
// id. Zero UUIDs are skipped before any cache or DB call.
//
// Three resolution outcomes per id:
//
//  1. Live identity row → Actor with id + display_name + email.
//  2. Soft-deleted identity row (is_deleted=true) → Actor with only
//     id and is_deleted=true; PII is already blanked at the DB layer.
//  3. Missing row (id was never persisted, or hard-purged) → Actor
//     with id and is_deleted=true placeholder so callers don't
//     silently drop the audit reference.
//
// All three outcomes are cached. The placeholder for outcome 3 is
// cached too — repeatedly missing on the same dangling UUID would
// otherwise issue a fresh DB call per page render.
func (r *Resolver) Resolve(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*typespb.Actor, error) {
	out := make(map[uuid.UUID]*typespb.Actor, len(ids))

	deduped := dedupeNonZero(ids)
	if len(deduped) == 0 {
		return out, nil
	}

	// Cache pass: short-circuit ids we've seen recently. Misses are
	// collected for a single batched DB call.
	misses := deduped
	if r.cache != nil {
		misses = misses[:0]
		for _, id := range deduped {
			if cached, ok := r.cache.Get(id); ok {
				out[id] = cached
				continue
			}
			misses = append(misses, id)
		}
		if len(misses) == 0 {
			return out, nil
		}
	}

	rows, err := r.queries.GetIdentitiesByIDs(ctx, misses)
	if err != nil {
		return nil, err
	}

	for _, row := range rows {
		actor := &typespb.Actor{
			Id:          row.ID.String(),
			DisplayName: row.DisplayName,
			Email:       row.Email,
			IsDeleted:   row.IsDeleted,
		}
		out[row.ID] = actor
		if r.cache != nil {
			r.cache.Add(row.ID, actor)
		}
	}

	for _, id := range misses {
		if _, ok := out[id]; ok {
			continue
		}
		placeholder := &typespb.Actor{Id: id.String(), IsDeleted: true}
		out[id] = placeholder
		if r.cache != nil {
			r.cache.Add(id, placeholder)
		}
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

// Invalidate drops cache entries for the given identity ids. Call
// this from any handler that mutates an identity row (display_name
// update, photo update, soft-delete, syncIdentity webhook upsert) so
// subsequent reads on this instance see the new state immediately.
// Other instances catch up via TTL expiry.
//
// No-op when caching is disabled or the entries aren't present.
// Safe to call with a zero UUID or no ids.
func (r *Resolver) Invalidate(ids ...uuid.UUID) {
	if r.cache == nil {
		return
	}
	for _, id := range ids {
		if id == uuid.Nil {
			continue
		}
		r.cache.Remove(id)
	}
}

// InvalidateAll drops every cache entry. Provided for tests and for
// emergency cache flush; production code should prefer the targeted
// Invalidate path.
func (r *Resolver) InvalidateAll() {
	if r.cache == nil {
		return
	}
	r.cache.Purge()
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
