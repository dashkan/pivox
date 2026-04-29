package spaces

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	db "github.com/dashkan/pivox/internal/db/generated"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	"github.com/dashkan/pivox/internal/server"
	"github.com/dashkan/pivox/internal/testutil/mocks"
)

// Unit-test surface for the Spaces handlers. Successful CRUD paths
// are exercised end-to-end via the testcontainers-backed harness in
// the spaces e2e test suite (P5.2) — those tests run the production
// interceptor chain and the real DB, which gives stronger guarantees
// than mocking every query.
//
// What lives here is the validation surface that fires BEFORE the
// resolved-context lookups: path parsing and slug-vs-resolved-scope
// mismatch checks. These paths can be exercised cheaply with seeded
// resolved-context — no DB, no mocks for the happy path.

var (
	testOrgID  = uuid.MustParse("0192a000-0001-7000-8000-000000000001")
	testProjID = uuid.MustParse("0192a000-0003-7000-8000-000000000001")
	testOrg    = db.Organization{
		ID:          testOrgID,
		Name:        "acme",
		DisplayName: "Acme Corp",
		State:       db.ResourceStateACTIVE,
		CreateTime:  time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
		UpdateTime:  time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
	}
	testDBSpace = db.Space{
		ID:          testProjID,
		OrgID:       testOrgID,
		Name:        "my-space",
		DisplayName: "My Space",
		Labels:      json.RawMessage(`{"env":"prod"}`),
		State:       db.ResourceStateACTIVE,
		Etag:        "etag-proj",
		Revision:    1,
		CreateTime:  time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
		UpdateTime:  time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
	}
)

// memberTestCtx returns a context pre-populated with the same
// ResolvedOrg + ResolvedSpace the permission interceptor would attach
// in production. Member handlers (and now space-CRUD handlers) read
// these from context instead of issuing fresh slug-resolution
// lookups, so unit tests must seed them. spaceID is parameterized
// because most member tests pin a specific id to assert the right
// downstream queries fired.
func memberTestCtx(spaceID uuid.UUID) context.Context {
	ctx := server.WithResolvedOrgForTest(context.Background(), &server.ResolvedOrg{
		ID:   testOrgID,
		Slug: testOrg.Name,
		Row:  testOrg,
	})
	ctx = server.WithResolvedSpaceForTest(ctx, &server.ResolvedSpace{
		ID:   spaceID,
		Slug: "news",
	})
	return ctx
}

// spaceCRUDCtx is the equivalent for the GetSpace/UpdateSpace/
// DeleteSpace/UndeleteSpace handlers, where the resolved space
// is the one named in the request rather than a placeholder.
func spaceCRUDCtx() context.Context {
	ctx := server.WithResolvedOrgForTest(context.Background(), &server.ResolvedOrg{
		ID:   testOrgID,
		Slug: testOrg.Name,
		Row:  testOrg,
	})
	ctx = server.WithResolvedSpaceForTest(ctx, &server.ResolvedSpace{
		ID:   testProjID,
		Slug: testDBSpace.Name,
		Row:  testDBSpace,
	})
	return ctx
}

func TestUnit_GetSpace_Success(t *testing.T) {
	srv := NewSpacesServer(nil, nil, new(mocks.MockQuerier), nil, nil, nil, nil)
	resp, err := srv.GetSpace(spaceCRUDCtx(), &apiv1.GetSpaceRequest{
		Name: "organizations/acme/spaces/my-space",
	})
	require.NoError(t, err)
	assert.Equal(t, "organizations/acme/spaces/my-space", resp.GetName())
	assert.Equal(t, "My Space", resp.GetDisplayName())
	assert.Equal(t, apiv1.Space_ACTIVE, resp.GetState())
}

func TestUnit_GetSpace_InvalidName(t *testing.T) {
	srv := NewSpacesServer(nil, nil, new(mocks.MockQuerier), nil, nil, nil, nil)
	_, err := srv.GetSpace(context.Background(), &apiv1.GetSpaceRequest{
		Name: "invalid/format",
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.NotEqual(t, codes.OK, st.Code())
}

// TestUnit_GetSpace_SlugMismatch pins the path-vs-resolved-scope
// guard. In production this never fires (the interceptor 404s on an
// unknown slug before the handler runs); the assertion is paranoia
// against gate-vs-handler skew.
func TestUnit_GetSpace_SlugMismatch(t *testing.T) {
	srv := NewSpacesServer(nil, nil, new(mocks.MockQuerier), nil, nil, nil, nil)
	_, err := srv.GetSpace(spaceCRUDCtx(), &apiv1.GetSpaceRequest{
		Name: "organizations/acme/spaces/different-space",
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestUnit_CreateSpace_InvalidParent(t *testing.T) {
	srv := NewSpacesServer(nil, nil, new(mocks.MockQuerier), nil, nil, nil, nil)
	_, err := srv.CreateSpace(context.Background(), &apiv1.CreateSpaceRequest{
		Parent: "bad/parent/format",
		Space:  &apiv1.Space{DisplayName: "Test"},
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestUnit_UpdateSpace_InvalidName(t *testing.T) {
	srv := NewSpacesServer(nil, nil, new(mocks.MockQuerier), nil, nil, nil, nil)
	_, err := srv.UpdateSpace(context.Background(), &apiv1.UpdateSpaceRequest{
		Space: &apiv1.Space{Name: "bad/format"},
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.NotEqual(t, codes.OK, st.Code())
}

func TestUnit_UpdateSpace_SlugMismatch(t *testing.T) {
	srv := NewSpacesServer(nil, nil, new(mocks.MockQuerier), nil, nil, nil, nil)
	_, err := srv.UpdateSpace(spaceCRUDCtx(), &apiv1.UpdateSpaceRequest{
		Space: &apiv1.Space{
			Name:        "organizations/acme/spaces/different-space",
			DisplayName: "x",
		},
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

// State-guard for non-ACTIVE spaces lives at the gate
// (enforceSpaceSoftDeleteGate) so every mutating space-scoped RPC
// inherits the protection. The handler does not duplicate the check
// for UpdateSpace — the gate-level test in
// internal/server/permission_interceptor_test.go pins that behavior.

func TestUnit_DeleteSpace_InvalidName(t *testing.T) {
	srv := NewSpacesServer(nil, nil, new(mocks.MockQuerier), nil, nil, nil, nil)
	_, err := srv.DeleteSpace(context.Background(), &apiv1.DeleteSpaceRequest{
		Name: "bad/format",
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.NotEqual(t, codes.OK, st.Code())
}

func TestUnit_DeleteSpace_NotActive(t *testing.T) {
	deleted := testDBSpace
	deleted.State = db.ResourceStateDELETEREQUESTED
	ctx := server.WithResolvedOrgForTest(context.Background(), &server.ResolvedOrg{
		ID: testOrgID, Slug: testOrg.Name, Row: testOrg,
	})
	ctx = server.WithResolvedSpaceForTest(ctx, &server.ResolvedSpace{
		ID: testProjID, Slug: testDBSpace.Name, Row: deleted,
	})

	srv := NewSpacesServer(nil, nil, new(mocks.MockQuerier), nil, nil, nil, nil)
	_, err := srv.DeleteSpace(ctx, &apiv1.DeleteSpaceRequest{
		Name: "organizations/acme/spaces/my-space",
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
}

func TestUnit_UndeleteSpace_InvalidName(t *testing.T) {
	srv := NewSpacesServer(nil, nil, new(mocks.MockQuerier), nil, nil, nil, nil)
	_, err := srv.UndeleteSpace(context.Background(), &apiv1.UndeleteSpaceRequest{
		Name: "bad/format",
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.NotEqual(t, codes.OK, st.Code())
}

// TestUnit_UndeleteSpace_NotDeleted pins the symmetric guard:
// Undelete only fires on DELETE_REQUESTED, surfacing
// FailedPrecondition if the space is already ACTIVE.
func TestUnit_UndeleteSpace_NotDeleted(t *testing.T) {
	srv := NewSpacesServer(nil, nil, new(mocks.MockQuerier), nil, nil, nil, nil)
	_, err := srv.UndeleteSpace(spaceCRUDCtx(), &apiv1.UndeleteSpaceRequest{
		Name: "organizations/acme/spaces/my-space",
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
}

func TestUnit_ListSpaces_InvalidParent(t *testing.T) {
	srv := NewSpacesServer(nil, nil, new(mocks.MockQuerier), nil, nil, nil, nil)
	_, err := srv.ListSpaces(context.Background(), &apiv1.ListSpacesRequest{
		Parent: "badprefix/acme",
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	// ParseSegment surfaces InvalidArgument for malformed prefixes.
	assert.NotEqual(t, codes.OK, st.Code())
}

// TestUnit_ListSpaces_SlugMismatch covers the parent-vs-resolved
// scope guard. Production never reaches this because the interceptor
// 404s; the assertion is paranoia against gate-vs-handler skew.
func TestUnit_ListSpaces_SlugMismatch(t *testing.T) {
	srv := NewSpacesServer(nil, nil, new(mocks.MockQuerier), nil, nil, nil, nil)
	ctx := server.WithResolvedOrgForTest(context.Background(), &server.ResolvedOrg{
		ID: testOrgID, Slug: "acme", Row: testOrg,
	})
	_, err := srv.ListSpaces(ctx, &apiv1.ListSpacesRequest{
		Parent: "organizations/different-org",
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}
