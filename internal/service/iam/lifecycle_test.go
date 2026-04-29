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
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/lro"
	iampb "github.com/dashkan/pivox/internal/pkg/gen/pivox/iam/v1"
	"github.com/dashkan/pivox/internal/server"
	"github.com/dashkan/pivox/internal/testutil/mocks"
)

// fakeProgress captures DeleteUser phase updates for assertion.
type fakeProgress struct {
	phases []iampb.DeleteUserMetadata_Phase
}

func (f *fakeProgress) Update(_ context.Context, m proto.Message) {
	if md, ok := m.(*iampb.DeleteUserMetadata); ok {
		f.phases = append(f.phases, md.GetPhase())
	}
}

// mockAuthService is a minimal authn.Service for DeleteUser tests.
// Only DeleteUser is exercised; VerifyToken / CreateCustomToken
// are stubbed to satisfy the interface and never called.
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

func TestDeleteUser_RejectsMalformedPath(t *testing.T) {
	q := new(mocks.MockQuerier)
	srv := &IamServer{queries: q, lroManager: lro.NewManager(q, silentLogger())}
	ctx := server.WithResolvedOrgForTest(context.Background(), &server.ResolvedOrg{
		ID: uuid.MustParse("0192a000-aaaa-7000-8000-000000000001"), Slug: "acme",
	})
	_, err := srv.DeleteUser(ctx, &iampb.DeleteUserRequest{Name: "users/foo"})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestDeleteUser_RejectsOrgSlugMismatch(t *testing.T) {
	// Defense against gate-vs-handler name drift.
	q := new(mocks.MockQuerier)
	srv := &IamServer{queries: q, lroManager: lro.NewManager(q, silentLogger())}
	ctx := server.WithResolvedOrgForTest(context.Background(), &server.ResolvedOrg{
		ID: uuid.MustParse("0192a000-aaaa-7000-8000-000000000001"), Slug: "acme",
	})
	_, err := srv.DeleteUser(ctx, &iampb.DeleteUserRequest{
		Name: "organizations/different/users/me",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestDeleteUser_RejectsBadUserSegment(t *testing.T) {
	q := new(mocks.MockQuerier)
	srv := &IamServer{queries: q, lroManager: lro.NewManager(q, silentLogger())}
	ctx := server.WithResolvedOrgForTest(context.Background(), &server.ResolvedOrg{
		ID: uuid.MustParse("0192a000-aaaa-7000-8000-000000000001"), Slug: "acme",
	})
	_, err := srv.DeleteUser(ctx, &iampb.DeleteUserRequest{
		Name: "organizations/acme/users/not-uuid",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

// --- runDeleteUser orchestrator ---

func TestRunDeleteUser_BlocksSoleOwner(t *testing.T) {
	q := new(mocks.MockQuerier)
	identityID := uuid.MustParse("0192a000-bbbb-7000-8000-000000000002")
	q.On("ListSoleOwnerOrgsForFirebaseIdentity", mock.Anything, identityID).
		Return([]db.Organization{
			{Name: "acme"},
			{Name: "beta"},
		}, nil)

	srv := &IamServer{queries: q}
	_, err := srv.runDeleteUser(context.Background(), &fakeProgress{}, identityID, "organizations/acme/users/me")
	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	assert.Contains(t, err.Error(), "organizations/acme")
	assert.Contains(t, err.Error(), "organizations/beta")
}

func TestRunDeleteUser_FullCascade(t *testing.T) {
	identityID := uuid.MustParse("0192a000-bbbb-7000-8000-000000000002")
	q := new(mocks.MockQuerier)
	q.On("ListSoleOwnerOrgsForFirebaseIdentity", mock.Anything, identityID).
		Return([]db.Organization{}, nil)
	q.On("DeleteOrgMembersForFirebaseIdentity", mock.Anything, identityID).Return(nil)
	q.On("DeleteSpaceMembersForFirebaseIdentity", mock.Anything, identityID).Return(nil)
	q.On("GetFirebaseIdentityByID", mock.Anything, identityID).
		Return(db.FirebaseIdentity{ID: identityID, FirebaseUid: "fb-abc"}, nil)
	q.On("HardDeleteFirebaseIdentity", mock.Anything, identityID).Return(nil)

	auth := new(mockAuthService)
	auth.On("DeleteUser", mock.Anything, "fb-abc").Return(nil)

	srv := &IamServer{queries: q, auth: auth}
	progress := &fakeProgress{}
	_, err := srv.runDeleteUser(context.Background(), progress, identityID, "organizations/acme/users/me")
	require.NoError(t, err)
	assert.Equal(t, []iampb.DeleteUserMetadata_Phase{
		iampb.DeleteUserMetadata_VALIDATING,
		iampb.DeleteUserMetadata_REVOKING_MEMBERSHIPS,
		iampb.DeleteUserMetadata_DELETING_PIVOX_RECORDS,
		iampb.DeleteUserMetadata_DELETING_FIREBASE_IDENTITY,
		iampb.DeleteUserMetadata_COMPLETED,
	}, progress.phases)
	q.AssertExpectations(t)
	auth.AssertExpectations(t)
}

func TestRunDeleteUser_AlreadyGoneSurfacesInternal(t *testing.T) {
	// Pivox-side row already gone from a prior partial run, but
	// auth.DeleteUser hasn't run yet — the firebase_uid is lost.
	// The orchestrator MUST NOT silently complete: that would
	// leave a fully-functional Firebase Auth account orphaned with
	// no matching Pivox row. Expect Internal so the operator sees
	// the failure and reconciles manually.
	identityID := uuid.MustParse("0192a000-bbbb-7000-8000-000000000002")
	q := new(mocks.MockQuerier)
	q.On("ListSoleOwnerOrgsForFirebaseIdentity", mock.Anything, mock.Anything).Return([]db.Organization{}, nil)
	q.On("DeleteOrgMembersForFirebaseIdentity", mock.Anything, identityID).Return(nil)
	q.On("DeleteSpaceMembersForFirebaseIdentity", mock.Anything, identityID).Return(nil)
	q.On("GetFirebaseIdentityByID", mock.Anything, identityID).
		Return(db.FirebaseIdentity{}, pgx.ErrNoRows)

	srv := &IamServer{queries: q}
	_, err := srv.runDeleteUser(context.Background(), &fakeProgress{}, identityID, "organizations/acme/users/me")
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
	q.AssertNotCalled(t, "HardDeleteFirebaseIdentity", mock.Anything, mock.Anything)
}

func TestRunDeleteUser_AuthFailureSurfaces(t *testing.T) {
	// Pivox state is already gone when this fires; the LRO surfaces
	// the Firebase Auth error so the caller can retry. Idempotency
	// of authn.Service.DeleteUser makes the retry safe.
	identityID := uuid.MustParse("0192a000-bbbb-7000-8000-000000000002")
	q := new(mocks.MockQuerier)
	q.On("ListSoleOwnerOrgsForFirebaseIdentity", mock.Anything, mock.Anything).Return([]db.Organization{}, nil)
	q.On("DeleteOrgMembersForFirebaseIdentity", mock.Anything, identityID).Return(nil)
	q.On("DeleteSpaceMembersForFirebaseIdentity", mock.Anything, identityID).Return(nil)
	q.On("GetFirebaseIdentityByID", mock.Anything, identityID).
		Return(db.FirebaseIdentity{ID: identityID, FirebaseUid: "fb-abc"}, nil)
	q.On("HardDeleteFirebaseIdentity", mock.Anything, identityID).Return(nil)

	auth := new(mockAuthService)
	auth.On("DeleteUser", mock.Anything, "fb-abc").Return(errors.New("firebase down"))

	srv := &IamServer{queries: q, auth: auth}
	_, err := srv.runDeleteUser(context.Background(), &fakeProgress{}, identityID, "organizations/acme/users/me")
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}
