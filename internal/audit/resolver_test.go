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

	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
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
	q.On("GetFirebaseIdentitiesByIDs", mock.Anything, mock.MatchedBy(func(ids []uuid.UUID) bool {
		// Caller may pass IDs in any order — we only care about set equality.
		got := append([]uuid.UUID(nil), ids...)
		sort.Slice(got, func(i, j int) bool { return got[i].String() < got[j].String() })
		want := []uuid.UUID{idA, idB}
		sort.Slice(want, func(i, j int) bool { return want[i].String() < want[j].String() })
		return len(got) == len(want) && got[0] == want[0] && got[1] == want[1]
	})).Return([]db.FirebaseIdentity{
		{ID: idA, Email: "a@example.com", DisplayName: "Alice"},
		{ID: idB, Email: "b@example.com", DisplayName: "Bob"},
	}, nil)

	r := NewResolver(q)
	got, err := r.Resolve(context.Background(), []uuid.UUID{idA, idB})
	require.NoError(t, err)

	require.Len(t, got, 2)
	assert.Equal(t, &apiv1.Actor{Id: idA.String(), DisplayName: "Alice", Email: "a@example.com"}, got[idA])
	assert.Equal(t, &apiv1.Actor{Id: idB.String(), DisplayName: "Bob", Email: "b@example.com"}, got[idB])
	q.AssertExpectations(t)
}

func TestResolver_Resolve_DedupesInputIDs(t *testing.T) {
	// Callers typically build the ID slice from `created_by` /
	// `updated_by` / `deleted_by` columns across a page of rows, so
	// duplicates are common. The resolver must dedupe before hitting
	// the DB — otherwise the same identity is fetched N times.
	q := new(mocks.MockQuerier)
	q.On("GetFirebaseIdentitiesByIDs", mock.Anything, mock.MatchedBy(func(ids []uuid.UUID) bool {
		return len(ids) == 1 && ids[0] == idA
	})).Return([]db.FirebaseIdentity{
		{ID: idA, Email: "a@example.com", DisplayName: "Alice"},
	}, nil)

	r := NewResolver(q)
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
	q.On("GetFirebaseIdentitiesByIDs", mock.Anything, mock.MatchedBy(func(ids []uuid.UUID) bool {
		return len(ids) == 1 && ids[0] == idA
	})).Return([]db.FirebaseIdentity{
		{ID: idA, Email: "a@example.com", DisplayName: "Alice"},
	}, nil)

	r := NewResolver(q)
	got, err := r.Resolve(context.Background(), []uuid.UUID{uuid.Nil, idA, uuid.Nil})
	require.NoError(t, err)
	assert.Len(t, got, 1)
	assert.NotContains(t, got, uuid.Nil)
	q.AssertExpectations(t)
}

func TestResolver_Resolve_EmptyInput(t *testing.T) {
	// An entirely empty / all-zero slice should never hit the DB.
	q := new(mocks.MockQuerier)

	r := NewResolver(q)
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
	q.On("GetFirebaseIdentitiesByIDs", mock.Anything, mock.Anything).
		Return([]db.FirebaseIdentity{
			{ID: idA, Email: "a@example.com", DisplayName: "Alice"},
		}, nil)

	r := NewResolver(q)
	got, err := r.Resolve(context.Background(), []uuid.UUID{idA, idB})
	require.NoError(t, err)

	require.Len(t, got, 2)
	assert.Equal(t, &apiv1.Actor{Id: idA.String(), DisplayName: "Alice", Email: "a@example.com"}, got[idA])
	assert.Equal(t, &apiv1.Actor{Id: idB.String(), IsDeleted: true}, got[idB])
}

func TestResolver_Resolve_DBError(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("GetFirebaseIdentitiesByIDs", mock.Anything, mock.Anything).
		Return([]db.FirebaseIdentity{}, errors.New("connection reset"))

	r := NewResolver(q)
	_, err := r.Resolve(context.Background(), []uuid.UUID{idA})
	require.Error(t, err)
}

func TestResolver_ResolveOne_ZeroUUID(t *testing.T) {
	// Convenience for the common "single audit field" case. A zero
	// UUID returns nil so the caller can leave the proto field unset
	// (proto3 default) without an extra DB round-trip.
	q := new(mocks.MockQuerier)

	r := NewResolver(q)
	got, err := r.ResolveOne(context.Background(), uuid.Nil)
	require.NoError(t, err)
	assert.Nil(t, got)
	q.AssertExpectations(t)
}

func TestResolver_ResolveOne_HappyPath(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("GetFirebaseIdentitiesByIDs", mock.Anything, []uuid.UUID{idA}).
		Return([]db.FirebaseIdentity{
			{ID: idA, Email: "a@example.com", DisplayName: "Alice"},
		}, nil)

	r := NewResolver(q)
	got, err := r.ResolveOne(context.Background(), idA)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, idA.String(), got.GetId())
	assert.Equal(t, "Alice", got.GetDisplayName())
	q.AssertExpectations(t)
}
