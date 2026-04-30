package audit

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	typespb "github.com/dashkan/pivox/internal/pkg/gen/pivox/types"
	"github.com/dashkan/pivox/internal/testutil/mocks"

	db "github.com/dashkan/pivox/internal/db/generated"
)

// idA, idB, idC are stable UUIDs used across tests so failure
// messages are easier to read.
var (
	idA = uuid.MustParse("0192a000-aaaa-7000-8000-000000000001")
	idB = uuid.MustParse("0192a000-aaaa-7000-8000-000000000002")
	idC = uuid.MustParse("0192a000-aaaa-7000-8000-000000000003")
)

func TestResolver_Resolve_HappyPath(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("GetIdentitiesByIDs", mock.Anything, mock.MatchedBy(func(ids []uuid.UUID) bool {
		// Caller may pass IDs in any order — we only care about set equality.
		got := append([]uuid.UUID(nil), ids...)
		sort.Slice(got, func(i, j int) bool { return got[i].String() < got[j].String() })
		want := []uuid.UUID{idA, idB}
		sort.Slice(want, func(i, j int) bool { return want[i].String() < want[j].String() })
		return len(got) == len(want) && got[0] == want[0] && got[1] == want[1]
	})).Return([]db.Identity{
		{ID: idA, Email: "a@example.com", DisplayName: "Alice"},
		{ID: idB, Email: "b@example.com", DisplayName: "Bob"},
	}, nil)

	r := NewResolver(Config{Queries: q, CacheSize: -1})
	got, err := r.Resolve(context.Background(), []uuid.UUID{idA, idB})
	require.NoError(t, err)

	require.Len(t, got, 2)
	assert.Equal(t, &typespb.Actor{Id: idA.String(), DisplayName: "Alice", Email: "a@example.com"}, got[idA])
	assert.Equal(t, &typespb.Actor{Id: idB.String(), DisplayName: "Bob", Email: "b@example.com"}, got[idB])
	q.AssertExpectations(t)
}

func TestResolver_Resolve_DedupesInputIDs(t *testing.T) {
	// Callers typically build the ID slice from `created_by` /
	// `updated_by` / `deleted_by` columns across a page of rows, so
	// duplicates are common. The resolver must dedupe before hitting
	// the DB — otherwise the same identity is fetched N times.
	q := new(mocks.MockQuerier)
	q.On("GetIdentitiesByIDs", mock.Anything, mock.MatchedBy(func(ids []uuid.UUID) bool {
		return len(ids) == 1 && ids[0] == idA
	})).Return([]db.Identity{
		{ID: idA, Email: "a@example.com", DisplayName: "Alice"},
	}, nil)

	r := NewResolver(Config{Queries: q, CacheSize: -1})
	got, err := r.Resolve(context.Background(), []uuid.UUID{idA, idA, idA})
	require.NoError(t, err)
	assert.Len(t, got, 1)
	q.AssertExpectations(t)
}

func TestResolver_Resolve_SkipsZeroUUIDs(t *testing.T) {
	// Audit columns are nullable. A NULL `created_by` surfaces as a
	// zero `uuid.UUID` from the converter layer; passing it through
	// to a DB lookup would be wasted work and could mask real bugs.
	q := new(mocks.MockQuerier)
	q.On("GetIdentitiesByIDs", mock.Anything, mock.MatchedBy(func(ids []uuid.UUID) bool {
		return len(ids) == 1 && ids[0] == idA
	})).Return([]db.Identity{
		{ID: idA, Email: "a@example.com", DisplayName: "Alice"},
	}, nil)

	r := NewResolver(Config{Queries: q, CacheSize: -1})
	got, err := r.Resolve(context.Background(), []uuid.UUID{uuid.Nil, idA, uuid.Nil})
	require.NoError(t, err)
	assert.Len(t, got, 1)
	assert.NotContains(t, got, uuid.Nil)
	q.AssertExpectations(t)
}

func TestResolver_Resolve_EmptyInput(t *testing.T) {
	// An entirely empty / all-zero slice should never hit the DB.
	q := new(mocks.MockQuerier)

	r := NewResolver(Config{Queries: q, CacheSize: -1})
	got, err := r.Resolve(context.Background(), nil)
	require.NoError(t, err)
	assert.Empty(t, got)

	got, err = r.Resolve(context.Background(), []uuid.UUID{uuid.Nil, uuid.Nil})
	require.NoError(t, err)
	assert.Empty(t, got)

	q.AssertExpectations(t)
}

func TestResolver_Resolve_MissingIDsReturnPlaceholder(t *testing.T) {
	// If the DB returns fewer rows than requested (identity row
	// purged or never existed), the resolver must still return an
	// Actor for each requested id with id+is_deleted populated and
	// PII blank. Callers consuming the map by ID would otherwise
	// silently drop the audit field.
	q := new(mocks.MockQuerier)
	q.On("GetIdentitiesByIDs", mock.Anything, mock.Anything).
		Return([]db.Identity{
			{ID: idA, Email: "a@example.com", DisplayName: "Alice"},
		}, nil)

	r := NewResolver(Config{Queries: q, CacheSize: -1})
	got, err := r.Resolve(context.Background(), []uuid.UUID{idA, idB})
	require.NoError(t, err)

	require.Len(t, got, 2)
	assert.Equal(t, &typespb.Actor{Id: idA.String(), DisplayName: "Alice", Email: "a@example.com"}, got[idA])
	assert.Equal(t, &typespb.Actor{Id: idB.String(), IsDeleted: true}, got[idB])
}

func TestResolver_Resolve_SoftDeletedIdentity(t *testing.T) {
	// Soft-deleted identity rows are returned by GetIdentitiesByIDs
	// with PII already blanked. The resolver propagates is_deleted
	// onto the Actor so consumers know the audit reference points
	// at a tombstoned identity. Distinct from the missing-row case
	// (covered by TestResolver_Resolve_MissingIDsReturnPlaceholder)
	// — here the row exists, just deleted.
	q := new(mocks.MockQuerier)
	q.On("GetIdentitiesByIDs", mock.Anything, mock.Anything).
		Return([]db.Identity{
			{ID: idA, Email: "", DisplayName: "", IsDeleted: true},
			{ID: idB, Email: "b@example.com", DisplayName: "Bob"},
		}, nil)

	r := NewResolver(Config{Queries: q, CacheSize: -1})
	got, err := r.Resolve(context.Background(), []uuid.UUID{idA, idB})
	require.NoError(t, err)

	require.Len(t, got, 2)
	// Soft-deleted: id preserved, PII blank, is_deleted=true.
	assert.Equal(t, &typespb.Actor{Id: idA.String(), IsDeleted: true}, got[idA])
	// Live: full Actor.
	assert.Equal(t, &typespb.Actor{Id: idB.String(), DisplayName: "Bob", Email: "b@example.com"}, got[idB])
}

func TestResolver_Resolve_DBError(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("GetIdentitiesByIDs", mock.Anything, mock.Anything).
		Return([]db.Identity{}, errors.New("connection reset"))

	r := NewResolver(Config{Queries: q, CacheSize: -1})
	_, err := r.Resolve(context.Background(), []uuid.UUID{idA})
	require.Error(t, err)
}

func TestResolver_ResolveOne_ZeroUUID(t *testing.T) {
	// Convenience for the common "single audit field" case. A zero
	// UUID returns nil so the caller can leave the proto field unset
	// (proto3 default) without an extra DB round-trip.
	q := new(mocks.MockQuerier)

	r := NewResolver(Config{Queries: q, CacheSize: -1})
	got, err := r.ResolveOne(context.Background(), uuid.Nil)
	require.NoError(t, err)
	assert.Nil(t, got)
	q.AssertExpectations(t)
}

func TestResolver_ResolveOne_HappyPath(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("GetIdentitiesByIDs", mock.Anything, []uuid.UUID{idA}).
		Return([]db.Identity{
			{ID: idA, Email: "a@example.com", DisplayName: "Alice"},
		}, nil)

	r := NewResolver(Config{Queries: q, CacheSize: -1})
	got, err := r.ResolveOne(context.Background(), idA)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, idA.String(), got.GetId())
	assert.Equal(t, "Alice", got.GetDisplayName())
	q.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// LRU cache
// ---------------------------------------------------------------------------

// TestResolver_Cache_HitSkipsQuery is the load-bearing assertion for
// the cache path: a second Resolve call with overlapping ids must not
// re-issue the DB query for the cached entries. We pin this with
// .Once() — the second call would fail expectations if it tried to
// re-query.
func TestResolver_Cache_HitSkipsQuery(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("GetIdentitiesByIDs", mock.Anything, []uuid.UUID{idA}).
		Return([]db.Identity{
			{ID: idA, Email: "a@example.com", DisplayName: "Alice"},
		}, nil).Once()

	r := NewResolver(Config{Queries: q})

	// First call: idA misses, queries the DB.
	_, err := r.Resolve(context.Background(), []uuid.UUID{idA})
	require.NoError(t, err)
	// Second call: idA hits the cache → no DB call. The Once()
	// expectation above asserts this; the AssertExpectations below
	// is the verification.
	got, err := r.Resolve(context.Background(), []uuid.UUID{idA})
	require.NoError(t, err)
	assert.Equal(t, "Alice", got[idA].GetDisplayName())
	q.AssertExpectations(t)
}

// TestResolver_Cache_PartialMissOnlyQueriesMisses pins that a mixed
// hit/miss page hits the DB only for the misses — not the whole set.
func TestResolver_Cache_PartialMissOnlyQueriesMisses(t *testing.T) {
	q := new(mocks.MockQuerier)
	// First call warms idA.
	q.On("GetIdentitiesByIDs", mock.Anything, []uuid.UUID{idA}).
		Return([]db.Identity{{ID: idA, DisplayName: "Alice"}}, nil).Once()
	// Second call should query ONLY idB — idA is already cached.
	q.On("GetIdentitiesByIDs", mock.Anything, []uuid.UUID{idB}).
		Return([]db.Identity{{ID: idB, DisplayName: "Bob"}}, nil).Once()

	r := NewResolver(Config{Queries: q})
	_, err := r.Resolve(context.Background(), []uuid.UUID{idA})
	require.NoError(t, err)
	got, err := r.Resolve(context.Background(), []uuid.UUID{idA, idB})
	require.NoError(t, err)
	assert.Equal(t, "Alice", got[idA].GetDisplayName())
	assert.Equal(t, "Bob", got[idB].GetDisplayName())
	q.AssertExpectations(t)
}

// TestResolver_Cache_PlaceholderCached pins that the missing-row
// placeholder Actor is also cached — repeatedly resolving a dangling
// UUID must not issue a fresh DB call per page render.
func TestResolver_Cache_PlaceholderCached(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("GetIdentitiesByIDs", mock.Anything, []uuid.UUID{idA}).
		Return([]db.Identity{}, nil).Once()

	r := NewResolver(Config{Queries: q})
	got1, err := r.Resolve(context.Background(), []uuid.UUID{idA})
	require.NoError(t, err)
	assert.True(t, got1[idA].GetIsDeleted())

	got2, err := r.Resolve(context.Background(), []uuid.UUID{idA})
	require.NoError(t, err)
	assert.True(t, got2[idA].GetIsDeleted())
	q.AssertExpectations(t)
}

// TestResolver_Invalidate_ForcesRefetch pins that Invalidate drops a
// cached entry so the next Resolve re-queries.
func TestResolver_Invalidate_ForcesRefetch(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("GetIdentitiesByIDs", mock.Anything, []uuid.UUID{idA}).
		Return([]db.Identity{{ID: idA, DisplayName: "Alice"}}, nil).Once()
	q.On("GetIdentitiesByIDs", mock.Anything, []uuid.UUID{idA}).
		Return([]db.Identity{{ID: idA, DisplayName: "Alice (renamed)"}}, nil).Once()

	r := NewResolver(Config{Queries: q})
	got1, err := r.Resolve(context.Background(), []uuid.UUID{idA})
	require.NoError(t, err)
	assert.Equal(t, "Alice", got1[idA].GetDisplayName())

	r.Invalidate(idA)

	got2, err := r.Resolve(context.Background(), []uuid.UUID{idA})
	require.NoError(t, err)
	assert.Equal(t, "Alice (renamed)", got2[idA].GetDisplayName(),
		"post-Invalidate Resolve must see the new display_name")
	q.AssertExpectations(t)
}

// TestResolver_DisabledCache_AlwaysQueries verifies CacheSize=-1
// degrades to the original batched-lookup behavior — every call hits
// the DB. Useful for tests that assert on query counts.
func TestResolver_DisabledCache_AlwaysQueries(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("GetIdentitiesByIDs", mock.Anything, []uuid.UUID{idA}).
		Return([]db.Identity{{ID: idA, DisplayName: "Alice"}}, nil).Twice()

	r := NewResolver(Config{Queries: q, CacheSize: -1})
	for i := 0; i < 2; i++ {
		_, err := r.Resolve(context.Background(), []uuid.UUID{idA})
		require.NoError(t, err)
	}
	q.AssertExpectations(t)
}
