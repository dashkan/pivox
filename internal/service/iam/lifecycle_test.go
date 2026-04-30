package iam

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/dashkan/pivox/internal/authn"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/lro"
	iampb "github.com/dashkan/pivox/internal/pkg/gen/pivox/iam/v1"
	"github.com/dashkan/pivox/internal/server"
	"github.com/dashkan/pivox/internal/testutil/mocks"
)

// fakeAccountProgress captures DeleteAccount phase updates.
type fakeAccountProgress struct {
	phases []iampb.DeleteAccountMetadata_Phase
}

func (f *fakeAccountProgress) Update(_ context.Context, m proto.Message) {
	if md, ok := m.(*iampb.DeleteAccountMetadata); ok {
		f.phases = append(f.phases, md.GetPhase())
	}
}

// mockAuthService is a minimal authn.Service for DeleteAccount tests.
// VerifyToken / CreateCustomToken are stubbed to satisfy the
// interface and never called.
type mockAuthService struct{ mock.Mock }

func (m *mockAuthService) VerifyToken(context.Context, string) (*authn.Identity, error) {
	return nil, nil
}
func (m *mockAuthService) CreateCustomToken(context.Context, string) (string, error) { return "", nil }
func (m *mockAuthService) DeleteUser(ctx context.Context, uid string) error {
	args := m.Called(ctx, uid)
	return args.Error(0)
}

func (m *mockAuthService) CreateOidcProvider(ctx context.Context, cfg authn.OidcProviderConfig) error {
	args := m.Called(ctx, cfg)
	return args.Error(0)
}

func (m *mockAuthService) UpdateOidcProvider(ctx context.Context, cfg authn.OidcProviderConfig) error {
	args := m.Called(ctx, cfg)
	return args.Error(0)
}

func (m *mockAuthService) DeleteOidcProvider(ctx context.Context, providerID string) error {
	args := m.Called(ctx, providerID)
	return args.Error(0)
}

func (m *mockAuthService) CreateSamlProvider(ctx context.Context, cfg authn.SamlProviderConfig) error {
	args := m.Called(ctx, cfg)
	return args.Error(0)
}

func (m *mockAuthService) UpdateSamlProvider(ctx context.Context, cfg authn.SamlProviderConfig) error {
	args := m.Called(ctx, cfg)
	return args.Error(0)
}

func (m *mockAuthService) DeleteSamlProvider(ctx context.Context, providerID string) error {
	args := m.Called(ctx, providerID)
	return args.Error(0)
}

func silentLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// --- DeleteUser handler validation ---

var (
	testHandlerOrgID = uuid.MustParse("0192a000-aaaa-7000-8000-000000000001")
	testTargetUserID = uuid.MustParse("0192a000-cccc-7000-8000-000000000001")
)

func resolvedOrgCtx() context.Context {
	return server.WithResolvedOrgForTest(context.Background(), &server.ResolvedOrg{
		ID: testHandlerOrgID, Slug: "acme",
	})
}

func TestDeleteUser_RejectsMalformedPath(t *testing.T) {
	q := new(mocks.MockQuerier)
	srv := &IamServer{queries: q}
	_, err := srv.DeleteUser(resolvedOrgCtx(), &iampb.DeleteUserRequest{Name: "users/foo"})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestDeleteUser_RejectsOrgSlugMismatch(t *testing.T) {
	// Defense against gate-vs-handler name drift.
	q := new(mocks.MockQuerier)
	srv := &IamServer{queries: q}
	_, err := srv.DeleteUser(resolvedOrgCtx(), &iampb.DeleteUserRequest{
		Name: "organizations/different/users/" + testTargetUserID.String(),
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

// TestDeleteUser_RejectsMeTarget pins the v1 design: there's no
// self-leave-org capability. The literal `me` fails uuid.Parse and
// surfaces InvalidArgument with a generic "not a valid UUID"
// message — no special-casing or cross-RPC redirect.
func TestDeleteUser_RejectsMeTarget(t *testing.T) {
	q := new(mocks.MockQuerier)
	srv := &IamServer{queries: q}
	_, err := srv.DeleteUser(resolvedOrgCtx(), &iampb.DeleteUserRequest{
		Name: "organizations/acme/users/me",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestDeleteUser_RejectsBadUserSegment(t *testing.T) {
	q := new(mocks.MockQuerier)
	srv := &IamServer{queries: q}
	_, err := srv.DeleteUser(resolvedOrgCtx(), &iampb.DeleteUserRequest{
		Name: "organizations/acme/users/not-uuid",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

// TestDeleteUser_BlocksWhenLastOwner: refuses to remove the sole
// owner. Post-Phase-7 DeleteUser is sync (no LRO orchestrator) so
// this asserts the FAILED_PRECONDITION at the handler boundary
// directly.
func TestDeleteUser_BlocksWhenLastOwner(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("CountOrgOwnersExcludingUser", mock.Anything, db.CountOrgOwnersExcludingUserParams{
		OrgID: testHandlerOrgID, UserID: convert.PgUUID(testTargetUserID),
	}).Return(int64(0), nil)

	srv := &IamServer{queries: q}
	_, err := srv.DeleteUser(resolvedOrgCtx(), &iampb.DeleteUserRequest{
		Name: "organizations/acme/users/" + testTargetUserID.String(),
	})
	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	assert.Contains(t, err.Error(), "no owners")
}

// TestDeleteUser_FullCascade pins the sync hard-delete path: with
// remaining owners > 0, the handler fires the three deletes and
// returns Empty.
func TestDeleteUser_FullCascade(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("CountOrgOwnersExcludingUser", mock.Anything, mock.Anything).Return(int64(2), nil)
	q.On("DeleteOrgMembersForUserInOrg", mock.Anything, db.DeleteOrgMembersForUserInOrgParams{
		OrgID: testHandlerOrgID, UserID: convert.PgUUID(testTargetUserID),
	}).Return(nil)
	q.On("DeleteSpaceMembersForUserInOrg", mock.Anything, db.DeleteSpaceMembersForUserInOrgParams{
		OrgID: testHandlerOrgID, UserID: convert.PgUUID(testTargetUserID),
	}).Return(nil)
	q.On("DeleteGroupMembersForUserInOrg", mock.Anything, db.DeleteGroupMembersForUserInOrgParams{
		OrgID: testHandlerOrgID, UserID: testTargetUserID,
	}).Return(nil)

	srv := &IamServer{queries: q}
	resp, err := srv.DeleteUser(resolvedOrgCtx(), &iampb.DeleteUserRequest{
		Name: "organizations/acme/users/" + testTargetUserID.String(),
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	q.AssertExpectations(t)
}

// --- DeleteAccount handler validation ---

func TestDeleteAccount_RejectsNonMeName(t *testing.T) {
	srv := &IamServer{}
	_, err := srv.DeleteAccount(context.Background(), &iampb.DeleteAccountRequest{
		Name: "accounts/someone-else",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestDeleteAccount_FailsLoudOnNilDeps(t *testing.T) {
	srv := &IamServer{} // no lroManager / auth / caller
	_, err := srv.DeleteAccount(context.Background(), &iampb.DeleteAccountRequest{
		Name: "accounts/me",
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

// TestDeleteAccount_PropagatesCallerResolutionFailure pins that
// DeleteAccount surfaces caller-resolution errors directly rather
// than masking them. A common failure mode: Firebase token is valid
// (auth interceptor passed) but the identities row hasn't
// synced yet (race with the syncIdentity webhook). The
// caller resolver returns NotFound; the handler must surface that.
func TestDeleteAccount_PropagatesCallerResolutionFailure(t *testing.T) {
	srv := &IamServer{
		queries:    new(mocks.MockQuerier),
		auth:       new(mockAuthService),
		lroManager: lro.NewManager(new(mocks.MockQuerier), silentLogger()),
		caller: func(context.Context) (uuid.UUID, error) {
			return uuid.Nil, status.Error(codes.NotFound, "firebase identity not yet synced")
		},
	}
	_, err := srv.DeleteAccount(context.Background(), &iampb.DeleteAccountRequest{
		Name: "accounts/me",
	})
	require.Error(t, err)
	// Whatever code the resolver returned should surface unchanged.
	assert.Equal(t, codes.NotFound, status.Code(err))
}

// --- runDeleteAccount cross-org orchestrator ---

func TestRunDeleteAccount_BlocksSoleOwner(t *testing.T) {
	q := new(mocks.MockQuerier)
	identityID := uuid.MustParse("0192a000-bbbb-7000-8000-000000000002")
	q.On("ListSoleOwnerOrgsForIdentity", mock.Anything, convert.PgUUID(identityID)).
		Return([]db.Organization{{Name: "acme"}, {Name: "beta"}}, nil)

	srv := &IamServer{queries: q}
	_, err := srv.runDeleteAccount(context.Background(), &fakeAccountProgress{}, identityID)
	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	assert.Contains(t, err.Error(), "organizations/acme")
	assert.Contains(t, err.Error(), "organizations/beta")
}

func TestRunDeleteAccount_FullCascade(t *testing.T) {
	identityID := uuid.MustParse("0192a000-bbbb-7000-8000-000000000002")
	q := new(mocks.MockQuerier)
	q.On("ListSoleOwnerOrgsForIdentity", mock.Anything, convert.PgUUID(identityID)).
		Return([]db.Organization{}, nil)
	q.On("DeleteOrgMembersForIdentity", mock.Anything, convert.PgUUID(identityID)).Return(nil)
	q.On("DeleteSpaceMembersForIdentity", mock.Anything, convert.PgUUID(identityID)).Return(nil)
	q.On("GetIdentityByID", mock.Anything, identityID).
		Return(db.Identity{ID: identityID, FirebaseUid: "fb-abc"}, nil)
	q.On("SoftDeleteIdentity", mock.Anything, identityID).Return(identityID, nil)

	auth := new(mockAuthService)
	auth.On("DeleteUser", mock.Anything, "fb-abc").Return(nil)

	srv := &IamServer{queries: q, auth: auth}
	progress := &fakeAccountProgress{}
	_, err := srv.runDeleteAccount(context.Background(), progress, identityID)
	require.NoError(t, err)
	assert.Equal(t, []iampb.DeleteAccountMetadata_Phase{
		iampb.DeleteAccountMetadata_VALIDATING,
		iampb.DeleteAccountMetadata_REVOKING_MEMBERSHIPS,
		iampb.DeleteAccountMetadata_DELETING_PIVOX_RECORDS,
		iampb.DeleteAccountMetadata_DELETING_FIREBASE_IDENTITY,
		iampb.DeleteAccountMetadata_COMPLETED,
	}, progress.phases)
	q.AssertExpectations(t)
	auth.AssertExpectations(t)
}

func TestRunDeleteAccount_AlreadyGoneSurfacesInternal(t *testing.T) {
	// Pivox-side row already gone from a prior partial run, but
	// auth.DeleteUser hasn't run yet — the firebase_uid is lost.
	// The orchestrator MUST NOT silently complete: that would leave
	// a fully-functional Firebase Auth account orphaned with no
	// matching Pivox row. Expect Internal so the operator sees the
	// failure and reconciles manually.
	identityID := uuid.MustParse("0192a000-bbbb-7000-8000-000000000002")
	q := new(mocks.MockQuerier)
	q.On("ListSoleOwnerOrgsForIdentity", mock.Anything, mock.Anything).Return([]db.Organization{}, nil)
	q.On("DeleteOrgMembersForIdentity", mock.Anything, convert.PgUUID(identityID)).Return(nil)
	q.On("DeleteSpaceMembersForIdentity", mock.Anything, convert.PgUUID(identityID)).Return(nil)
	q.On("GetIdentityByID", mock.Anything, identityID).
		Return(db.Identity{}, pgx.ErrNoRows)

	srv := &IamServer{queries: q}
	_, err := srv.runDeleteAccount(context.Background(), &fakeAccountProgress{}, identityID)
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
	q.AssertNotCalled(t, "SoftDeleteIdentity", mock.Anything, mock.Anything)
}

func TestRunDeleteAccount_AuthFailureSurfaces(t *testing.T) {
	identityID := uuid.MustParse("0192a000-bbbb-7000-8000-000000000002")
	q := new(mocks.MockQuerier)
	q.On("ListSoleOwnerOrgsForIdentity", mock.Anything, mock.Anything).Return([]db.Organization{}, nil)
	q.On("DeleteOrgMembersForIdentity", mock.Anything, convert.PgUUID(identityID)).Return(nil)
	q.On("DeleteSpaceMembersForIdentity", mock.Anything, convert.PgUUID(identityID)).Return(nil)
	q.On("GetIdentityByID", mock.Anything, identityID).
		Return(db.Identity{ID: identityID, FirebaseUid: "fb-abc"}, nil)
	q.On("SoftDeleteIdentity", mock.Anything, identityID).Return(identityID, nil)

	auth := new(mockAuthService)
	auth.On("DeleteUser", mock.Anything, "fb-abc").Return(errors.New("firebase down"))

	srv := &IamServer{queries: q, auth: auth}
	_, err := srv.runDeleteAccount(context.Background(), &fakeAccountProgress{}, identityID)
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}
