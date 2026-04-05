package resource

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/testutil/mocks"
)

func TestParseSegment(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"valid org name", "organizations/meridian", "meridian", false},
		{"valid tag key", "tagKeys/550e8400-e29b-41d4-a716-446655440000", "550e8400-e29b-41d4-a716-446655440000", false},
		{"nested path", "organizations/acme/projects/p1", "acme/projects/p1", false},
		{"no slash", "invalid", "", true},
		{"trailing slash empty segment", "organizations/", "", true},
		{"empty string", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSegment(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCollectionFromName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"organizations collection", "organizations/meridian", "organizations"},
		{"tagKeys collection", "tagKeys/some-uuid", "tagKeys"},
		{"empty string", "", ""},
		{"no slash returns whole string", "singleword", "singleword"},
		{"slash at end", "trailing/", "trailing"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CollectionFromName(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResolveOrgParent_Success(t *testing.T) {
	ctx := context.Background()
	mock := new(mocks.MockQuerier)

	orgID := uuid.New()
	mock.On("GetOrganizationByName", ctx, "acme").Return(db.Organization{
		ID:   orgID,
		Name: "acme",
	}, nil)

	got, err := ResolveOrgParent(ctx, mock, "organizations/acme")
	require.NoError(t, err)
	assert.Equal(t, orgID, got)
	mock.AssertExpectations(t)
}

func TestResolveOrgParent_WrongCollection(t *testing.T) {
	ctx := context.Background()
	mock := new(mocks.MockQuerier)

	_, err := ResolveOrgParent(ctx, mock, "projects/acme")
	require.Error(t, err)
	st := status.Convert(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "invalid")
}

func TestResolveOrgParent_NotFound(t *testing.T) {
	ctx := context.Background()
	mock := new(mocks.MockQuerier)

	mock.On("GetOrganizationByName", ctx, "ghost-org").Return(db.Organization{}, pgx.ErrNoRows)

	_, err := ResolveOrgParent(ctx, mock, "organizations/ghost-org")
	require.Error(t, err)
	st := status.Convert(err)
	assert.Equal(t, codes.NotFound, st.Code())
	mock.AssertExpectations(t)
}

func TestCollectionFromName_EdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty string", "", ""},
		{"no slash", "noslash", "noslash"},
		{"single word with slash", "a/b", "a"},
		{"multiple slashes", "a/b/c/d", "a"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CollectionFromName(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}
