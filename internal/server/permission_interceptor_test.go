package server

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/permission"
	"github.com/dashkan/pivox/internal/testutil/mocks"
)

// Test fixtures shared across the permission-interceptor tests.
var (
	testPermCallerID = uuid.MustParse("0192a000-aaaa-7000-8000-000000000001")
	testPermOrgID    = uuid.MustParse("0192a000-bbbb-7000-8000-000000000002")
	testPermOrgSlug  = "acme"
	testPermOrgRow   = db.Organization{ID: testPermOrgID, Name: testPermOrgSlug, DisplayName: "Acme"}
)

// stubIdentity returns a CallerIdentityResolver that always resolves to
// testPermCallerID. Tests that exercise the unauth path swap in a stub
// returning a status error.
func stubIdentity(id uuid.UUID, err error) CallerIdentityResolver {
	return func(_ context.Context) (uuid.UUID, error) { return id, err }
}

// orgScopeFromRequest is a minimal extractor used in the tests. The
// "request" is just a string holding the org slug, so tests can drive
// the interceptor without depending on real proto request types.
func orgScopeFromRequest(req any) (ScopeRef, error) {
	s, ok := req.(string)
	if !ok {
		return ScopeRef{}, status.Error(codes.InvalidArgument, "test extractor requires string request")
	}
	return OrgScope(s), nil
}

// recordingHandler returns a UnaryHandler that records its invocation
// and stashes the ctx for assertions on what the interceptor attached.
func recordingHandler(called *bool, captured *context.Context) grpc.UnaryHandler {
	return func(ctx context.Context, _ any) (any, error) {
		*called = true
		*captured = ctx
		return "ok", nil
	}
}

// --- Happy path ---

func TestPermissionInterceptor_AllowsWhenPermissionGranted(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("GetOrganizationByName", mock.Anything, testPermOrgSlug).Return(testPermOrgRow, nil)
	q.On("GetEffectiveOrgRoles", mock.Anything, db.GetEffectiveOrgRolesParams{
		OrgID: testPermOrgID, FirebaseIdentityID: testPermCallerID,
	}).Return([]string{permission.RoleAdmin}, nil)

	registry := Registry{
		"/svc/UpdateOrg": {Permission: permission.OrganizationsUpdate, Extract: orgScopeFromRequest},
	}
	resolver := permission.NewResolver(q)
	interceptor := PermissionInterceptor(registry, nil, q, resolver, stubIdentity(testPermCallerID, nil))

	called := false
	var captured context.Context
	resp, err := interceptor(
		context.Background(),
		testPermOrgSlug,
		&grpc.UnaryServerInfo{FullMethod: "/svc/UpdateOrg"},
		recordingHandler(&called, &captured),
	)

	require.NoError(t, err)
	assert.Equal(t, "ok", resp)
	assert.True(t, called)
	got, ok := ResolvedOrgFromContext(captured)
	require.True(t, ok, "interceptor must attach resolved org to ctx")
	assert.Equal(t, testPermOrgID, got.ID)
	assert.Equal(t, testPermOrgSlug, got.Slug)
	q.AssertExpectations(t)
}

// --- Default deny ---

func TestPermissionInterceptor_UnregisteredMethodIsInternal(t *testing.T) {
	// An unregistered method is a server-side configuration bug, not
	// an authorization decision about the caller. Returning Internal
	// (rather than PermissionDenied) keeps real auth denials and
	// missed-registration bugs distinguishable in logs and metrics.
	q := new(mocks.MockQuerier) // no expectations — must not touch DB
	resolver := permission.NewResolver(q)
	interceptor := PermissionInterceptor(Registry{}, nil, q, resolver, stubIdentity(testPermCallerID, nil))

	called := false
	var captured context.Context
	_, err := interceptor(
		context.Background(),
		nil,
		&grpc.UnaryServerInfo{FullMethod: "/svc/Mystery"},
		recordingHandler(&called, &captured),
	)
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
	assert.False(t, called)
	q.AssertExpectations(t)
}

func TestPermissionInterceptor_DeniesWhenPermissionMissing(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("GetOrganizationByName", mock.Anything, testPermOrgSlug).Return(testPermOrgRow, nil)
	q.On("GetEffectiveOrgRoles", mock.Anything, mock.Anything).Return([]string{permission.RoleViewer}, nil)

	registry := Registry{
		"/svc/UpdateOrg": {Permission: permission.OrganizationsUpdate, Extract: orgScopeFromRequest},
	}
	resolver := permission.NewResolver(q)
	interceptor := PermissionInterceptor(registry, nil, q, resolver, stubIdentity(testPermCallerID, nil))

	called := false
	var captured context.Context
	_, err := interceptor(
		context.Background(),
		testPermOrgSlug,
		&grpc.UnaryServerInfo{FullMethod: "/svc/UpdateOrg"},
		recordingHandler(&called, &captured),
	)
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.False(t, called)
	q.AssertExpectations(t)
}

// --- Exempt list ---

func TestPermissionInterceptor_ExemptMethodPassesThrough(t *testing.T) {
	q := new(mocks.MockQuerier) // no expectations — exempt methods skip all checks
	resolver := permission.NewResolver(q)
	exempt := map[string]bool{"/svc/CreateOrg": true}
	interceptor := PermissionInterceptor(Registry{}, exempt, q, resolver, stubIdentity(testPermCallerID, nil))

	called := false
	var captured context.Context
	_, err := interceptor(
		context.Background(),
		nil,
		&grpc.UnaryServerInfo{FullMethod: "/svc/CreateOrg"},
		recordingHandler(&called, &captured),
	)
	require.NoError(t, err)
	assert.True(t, called)
	q.AssertExpectations(t)
}

// --- Failure modes ---

func TestPermissionInterceptor_ExtractorErrorPropagates(t *testing.T) {
	q := new(mocks.MockQuerier)
	resolver := permission.NewResolver(q)
	wantErr := status.Error(codes.InvalidArgument, "bad shape")
	registry := Registry{
		"/svc/UpdateOrg": {
			Permission: permission.OrganizationsUpdate,
			Extract:    func(any) (ScopeRef, error) { return ScopeRef{}, wantErr },
		},
	}
	interceptor := PermissionInterceptor(registry, nil, q, resolver, stubIdentity(testPermCallerID, nil))

	called := false
	var captured context.Context
	_, err := interceptor(
		context.Background(),
		nil,
		&grpc.UnaryServerInfo{FullMethod: "/svc/UpdateOrg"},
		recordingHandler(&called, &captured),
	)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.False(t, called)
}

func TestPermissionInterceptor_OrgNotFoundReturns404(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("GetOrganizationByName", mock.Anything, "missing").Return(db.Organization{}, pgx.ErrNoRows)

	registry := Registry{
		"/svc/UpdateOrg": {Permission: permission.OrganizationsUpdate, Extract: orgScopeFromRequest},
	}
	resolver := permission.NewResolver(q)
	interceptor := PermissionInterceptor(registry, nil, q, resolver, stubIdentity(testPermCallerID, nil))

	called := false
	var captured context.Context
	_, err := interceptor(
		context.Background(),
		"missing",
		&grpc.UnaryServerInfo{FullMethod: "/svc/UpdateOrg"},
		recordingHandler(&called, &captured),
	)
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
	assert.False(t, called)
	q.AssertExpectations(t)
}

func TestPermissionInterceptor_OrgLookupDBErrorIsInternal(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("GetOrganizationByName", mock.Anything, testPermOrgSlug).Return(db.Organization{}, errors.New("db down"))

	registry := Registry{
		"/svc/UpdateOrg": {Permission: permission.OrganizationsUpdate, Extract: orgScopeFromRequest},
	}
	resolver := permission.NewResolver(q)
	interceptor := PermissionInterceptor(registry, nil, q, resolver, stubIdentity(testPermCallerID, nil))

	called := false
	var captured context.Context
	_, err := interceptor(
		context.Background(),
		testPermOrgSlug,
		&grpc.UnaryServerInfo{FullMethod: "/svc/UpdateOrg"},
		recordingHandler(&called, &captured),
	)
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
	assert.False(t, called)
	q.AssertExpectations(t)
}

func TestPermissionInterceptor_ResolverErrorIsInternal(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("GetOrganizationByName", mock.Anything, testPermOrgSlug).Return(testPermOrgRow, nil)
	q.On("GetEffectiveOrgRoles", mock.Anything, mock.Anything).Return([]string(nil), errors.New("db transient"))

	registry := Registry{
		"/svc/UpdateOrg": {Permission: permission.OrganizationsUpdate, Extract: orgScopeFromRequest},
	}
	resolver := permission.NewResolver(q)
	interceptor := PermissionInterceptor(registry, nil, q, resolver, stubIdentity(testPermCallerID, nil))

	called := false
	var captured context.Context
	_, err := interceptor(
		context.Background(),
		testPermOrgSlug,
		&grpc.UnaryServerInfo{FullMethod: "/svc/UpdateOrg"},
		recordingHandler(&called, &captured),
	)
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
	assert.False(t, called)
	q.AssertExpectations(t)
}

func TestPermissionInterceptor_CallerIdentityErrorPropagates(t *testing.T) {
	q := new(mocks.MockQuerier) // identity resolution fails before any DB lookup
	resolver := permission.NewResolver(q)
	wantErr := status.Error(codes.Unauthenticated, "no caller")
	registry := Registry{
		"/svc/UpdateOrg": {Permission: permission.OrganizationsUpdate, Extract: orgScopeFromRequest},
	}
	interceptor := PermissionInterceptor(registry, nil, q, resolver, stubIdentity(uuid.Nil, wantErr))

	called := false
	var captured context.Context
	_, err := interceptor(
		context.Background(),
		testPermOrgSlug,
		&grpc.UnaryServerInfo{FullMethod: "/svc/UpdateOrg"},
		recordingHandler(&called, &captured),
	)
	require.Error(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
	assert.False(t, called)
	q.AssertExpectations(t)
}

// --- Space scope ---

var (
	testPermSpaceID   = uuid.MustParse("0192a000-cccc-7000-8000-000000000003")
	testPermSpaceSlug = "design"
	testPermSpaceRow  = db.Space{ID: testPermSpaceID, OrgID: testPermOrgID, Name: testPermSpaceSlug}
)

func spaceScopeFromRequest(req any) (ScopeRef, error) {
	// Test extractor: req is a [2]string array {orgSlug, spaceSlug}.
	s, ok := req.([2]string)
	if !ok {
		return ScopeRef{}, status.Error(codes.InvalidArgument, "test extractor requires [2]string request")
	}
	return SpaceScope(s[0], s[1]), nil
}

func TestPermissionInterceptor_SpaceScope_AllowsWhenPermissionGranted(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("GetOrganizationByName", mock.Anything, testPermOrgSlug).Return(testPermOrgRow, nil)
	q.On("GetSpaceByName", mock.Anything, db.GetSpaceByNameParams{
		OrgID: testPermOrgID, Name: testPermSpaceSlug,
	}).Return(testPermSpaceRow, nil)
	// Resolver path for SpaceTarget: GetSpaceParentOrg + GetEffectiveSpaceRoles + GetEffectiveOrgRoles.
	q.On("GetSpaceParentOrg", mock.Anything, testPermSpaceID).Return(testPermOrgID, nil)
	q.On("GetEffectiveSpaceRoles", mock.Anything, db.GetEffectiveSpaceRolesParams{
		SpaceID: testPermSpaceID, FirebaseIdentityID: testPermCallerID,
	}).Return([]string{permission.RoleEditor}, nil)
	q.On("GetEffectiveOrgRoles", mock.Anything, db.GetEffectiveOrgRolesParams{
		OrgID: testPermOrgID, FirebaseIdentityID: testPermCallerID,
	}).Return([]string(nil), nil)

	registry := Registry{
		"/svc/GetSpace": {Permission: permission.SpacesRead, Extract: spaceScopeFromRequest},
	}
	resolver := permission.NewResolver(q)
	interceptor := PermissionInterceptor(registry, nil, q, resolver, stubIdentity(testPermCallerID, nil))

	called := false
	var captured context.Context
	_, err := interceptor(
		context.Background(),
		[2]string{testPermOrgSlug, testPermSpaceSlug},
		&grpc.UnaryServerInfo{FullMethod: "/svc/GetSpace"},
		recordingHandler(&called, &captured),
	)
	require.NoError(t, err)
	assert.True(t, called)

	gotOrg, ok := ResolvedOrgFromContext(captured)
	require.True(t, ok, "interceptor must attach resolved parent org for space-scoped RPCs")
	assert.Equal(t, testPermOrgID, gotOrg.ID)
	gotSpace, ok := ResolvedSpaceFromContext(captured)
	require.True(t, ok, "interceptor must attach resolved space")
	assert.Equal(t, testPermSpaceID, gotSpace.ID)
	assert.Equal(t, testPermSpaceSlug, gotSpace.Slug)
	q.AssertExpectations(t)
}

func TestPermissionInterceptor_SpaceScope_DeniesWhenPermissionMissing(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("GetOrganizationByName", mock.Anything, testPermOrgSlug).Return(testPermOrgRow, nil)
	q.On("GetSpaceByName", mock.Anything, mock.Anything).Return(testPermSpaceRow, nil)
	q.On("GetSpaceParentOrg", mock.Anything, testPermSpaceID).Return(testPermOrgID, nil)
	q.On("GetEffectiveSpaceRoles", mock.Anything, mock.Anything).Return([]string{permission.RoleViewer}, nil)
	q.On("GetEffectiveOrgRoles", mock.Anything, mock.Anything).Return([]string(nil), nil)

	registry := Registry{
		"/svc/UpdateSpace": {Permission: permission.SpacesUpdate, Extract: spaceScopeFromRequest},
	}
	resolver := permission.NewResolver(q)
	interceptor := PermissionInterceptor(registry, nil, q, resolver, stubIdentity(testPermCallerID, nil))

	called := false
	var captured context.Context
	_, err := interceptor(
		context.Background(),
		[2]string{testPermOrgSlug, testPermSpaceSlug},
		&grpc.UnaryServerInfo{FullMethod: "/svc/UpdateSpace"},
		recordingHandler(&called, &captured),
	)
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.False(t, called)
	q.AssertExpectations(t)
}

func TestPermissionInterceptor_SpaceScope_OrgInheritanceGrants(t *testing.T) {
	// Space-level role list is empty; org-level admin role inherits
	// down and grants SpacesUpdate via the resolver's union.
	q := new(mocks.MockQuerier)
	q.On("GetOrganizationByName", mock.Anything, testPermOrgSlug).Return(testPermOrgRow, nil)
	q.On("GetSpaceByName", mock.Anything, mock.Anything).Return(testPermSpaceRow, nil)
	q.On("GetSpaceParentOrg", mock.Anything, testPermSpaceID).Return(testPermOrgID, nil)
	q.On("GetEffectiveSpaceRoles", mock.Anything, mock.Anything).Return([]string(nil), nil)
	q.On("GetEffectiveOrgRoles", mock.Anything, mock.Anything).Return([]string{permission.RoleAdmin}, nil)

	registry := Registry{
		"/svc/UpdateSpace": {Permission: permission.SpacesUpdate, Extract: spaceScopeFromRequest},
	}
	resolver := permission.NewResolver(q)
	interceptor := PermissionInterceptor(registry, nil, q, resolver, stubIdentity(testPermCallerID, nil))

	called := false
	var captured context.Context
	_, err := interceptor(
		context.Background(),
		[2]string{testPermOrgSlug, testPermSpaceSlug},
		&grpc.UnaryServerInfo{FullMethod: "/svc/UpdateSpace"},
		recordingHandler(&called, &captured),
	)
	require.NoError(t, err)
	assert.True(t, called)
}

func TestPermissionInterceptor_SpaceScope_OrgNotFoundReturns404(t *testing.T) {
	// Parent org missing on a space-scope RPC must short-circuit at
	// the org lookup with NotFound — same code path as the org-scope
	// 404, just on the first of two slug resolutions.
	q := new(mocks.MockQuerier)
	q.On("GetOrganizationByName", mock.Anything, "ghost-org").Return(db.Organization{}, pgx.ErrNoRows)

	registry := Registry{
		"/svc/GetSpace": {Permission: permission.SpacesRead, Extract: spaceScopeFromRequest},
	}
	resolver := permission.NewResolver(q)
	interceptor := PermissionInterceptor(registry, nil, q, resolver, stubIdentity(testPermCallerID, nil))

	called := false
	var captured context.Context
	_, err := interceptor(
		context.Background(),
		[2]string{"ghost-org", testPermSpaceSlug},
		&grpc.UnaryServerInfo{FullMethod: "/svc/GetSpace"},
		recordingHandler(&called, &captured),
	)
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
	assert.False(t, called)
	q.AssertExpectations(t)
}

func TestPermissionInterceptor_SpaceScope_SpaceNotFoundReturns404(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("GetOrganizationByName", mock.Anything, testPermOrgSlug).Return(testPermOrgRow, nil)
	q.On("GetSpaceByName", mock.Anything, mock.Anything).Return(db.Space{}, pgx.ErrNoRows)

	registry := Registry{
		"/svc/GetSpace": {Permission: permission.SpacesRead, Extract: spaceScopeFromRequest},
	}
	resolver := permission.NewResolver(q)
	interceptor := PermissionInterceptor(registry, nil, q, resolver, stubIdentity(testPermCallerID, nil))

	called := false
	var captured context.Context
	_, err := interceptor(
		context.Background(),
		[2]string{testPermOrgSlug, "ghost"},
		&grpc.UnaryServerInfo{FullMethod: "/svc/GetSpace"},
		recordingHandler(&called, &captured),
	)
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
	assert.False(t, called)
	q.AssertExpectations(t)
}

// --- Empty slug pinned: extractor produces empty Slug → 404 ---

func TestPermissionInterceptor_EmptySlugFromExtractorReturns404(t *testing.T) {
	// Interceptor performs no slug-shape validation on its own; an
	// empty slug from a buggy extractor produces a NotFound on the
	// org lookup. Per-extractor validation is the right layer for
	// shape checks; this test pins the fallback behavior.
	q := new(mocks.MockQuerier)
	q.On("GetOrganizationByName", mock.Anything, "").Return(db.Organization{}, pgx.ErrNoRows)

	registry := Registry{
		"/svc/UpdateOrg": {
			Permission: permission.OrganizationsUpdate,
			Extract:    func(any) (ScopeRef, error) { return OrgScope(""), nil },
		},
	}
	resolver := permission.NewResolver(q)
	interceptor := PermissionInterceptor(registry, nil, q, resolver, stubIdentity(testPermCallerID, nil))

	called := false
	var captured context.Context
	_, err := interceptor(
		context.Background(),
		nil,
		&grpc.UnaryServerInfo{FullMethod: "/svc/UpdateOrg"},
		recordingHandler(&called, &captured),
	)
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
	assert.False(t, called)
	q.AssertExpectations(t)
}

// --- ResolvedOrg.Slug uses the slug we resolved against ---

func TestPermissionInterceptor_ResolvedOrgSlugMatchesRequest(t *testing.T) {
	// Defensive: even if the org row's Name field were renamed/aliased
	// in a future migration, ResolvedOrg.Slug must equal the slug the
	// caller addressed (i.e. the one already validated by the
	// permission check).
	q := new(mocks.MockQuerier)
	rowWithDifferentName := testPermOrgRow
	rowWithDifferentName.Name = "renamed-by-some-future-migration"
	q.On("GetOrganizationByName", mock.Anything, testPermOrgSlug).Return(rowWithDifferentName, nil)
	q.On("GetEffectiveOrgRoles", mock.Anything, mock.Anything).Return([]string{permission.RoleAdmin}, nil)

	registry := Registry{
		"/svc/UpdateOrg": {Permission: permission.OrganizationsUpdate, Extract: orgScopeFromRequest},
	}
	resolver := permission.NewResolver(q)
	interceptor := PermissionInterceptor(registry, nil, q, resolver, stubIdentity(testPermCallerID, nil))

	called := false
	var captured context.Context
	_, err := interceptor(
		context.Background(),
		testPermOrgSlug,
		&grpc.UnaryServerInfo{FullMethod: "/svc/UpdateOrg"},
		recordingHandler(&called, &captured),
	)
	require.NoError(t, err)
	got, ok := ResolvedOrgFromContext(captured)
	require.True(t, ok)
	assert.Equal(t, testPermOrgSlug, got.Slug,
		"ResolvedOrg.Slug must echo the slug used for the permission check, not whatever happens to be in org.Name")
}

// --- Stream interceptor: default-deny for unregistered streaming methods ---

func TestPermissionStreamInterceptor_UnregisteredMethodIsInternal(t *testing.T) {
	q := new(mocks.MockQuerier)
	resolver := permission.NewResolver(q)
	interceptor := PermissionStreamInterceptor(Registry{}, nil, q, resolver, stubIdentity(testPermCallerID, nil))

	called := false
	err := interceptor(
		nil,
		&permTestStream{ctx: context.Background()},
		&grpc.StreamServerInfo{FullMethod: "/svc/SomeStream"},
		func(srv any, ss grpc.ServerStream) error {
			called = true
			return nil
		},
	)
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
	assert.False(t, called)
}

func TestPermissionStreamInterceptor_ExemptStreamPassesThrough(t *testing.T) {
	q := new(mocks.MockQuerier)
	resolver := permission.NewResolver(q)
	exempt := map[string]bool{"/svc/AllowedStream": true}
	interceptor := PermissionStreamInterceptor(Registry{}, exempt, q, resolver, stubIdentity(testPermCallerID, nil))

	called := false
	err := interceptor(
		nil,
		&permTestStream{ctx: context.Background()},
		&grpc.StreamServerInfo{FullMethod: "/svc/AllowedStream"},
		func(srv any, ss grpc.ServerStream) error {
			called = true
			return nil
		},
	)
	require.NoError(t, err)
	assert.True(t, called)
}

// permTestStream is a minimal grpc.ServerStream stub used only to
// drive the streaming interceptor in unit tests.
type permTestStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *permTestStream) Context() context.Context { return s.ctx }

// --- Registry / Exempt overlap is a misconfiguration ---

func TestPermissionInterceptor_RejectsOverlappingRegistryAndExempt(t *testing.T) {
	q := new(mocks.MockQuerier)
	resolver := permission.NewResolver(q)
	// Same method in both registry and exempt is a programming error —
	// the interceptor must refuse to start in that state rather than
	// silently picking one over the other.
	registry := Registry{
		"/svc/UpdateOrg": {Permission: permission.OrganizationsUpdate, Extract: orgScopeFromRequest},
	}
	exempt := map[string]bool{"/svc/UpdateOrg": true}

	assert.Panics(t, func() {
		PermissionInterceptor(registry, exempt, q, resolver, stubIdentity(testPermCallerID, nil))
	})
}
