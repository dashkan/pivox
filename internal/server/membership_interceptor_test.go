package server

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/testutil/mocks"
)

// Canonical caller used across the interceptor tests. The mock querier
// returns this firebase_identity from `GetIdentityByFirebaseUID` for
// the matching UID.
const testMemberUID = "fb-member-uid"

var testMemberIdentity = db.Identity{
	ID:          uuid.MustParse("0192a000-cccc-7000-8000-000000000001"),
	FirebaseUid: testMemberUID,
	Email:       "member@example.com",
}

// callHandler is a minimal grpc.UnaryHandler that records that it was
// invoked. Tests use it to distinguish "interceptor allowed the call
// through" from "interceptor blocked the call".
func callHandler(called *bool) grpc.UnaryHandler {
	return func(_ context.Context, _ any) (any, error) {
		*called = true
		return "ok", nil
	}
}

func authedCtx(uid string) context.Context {
	return context.WithValue(context.Background(), authContextKey{}, uid)
}

// --- Allowlist bypass ---

func TestMembershipInterceptor_AllowlistedMethodSkipsCheck(t *testing.T) {
	q := new(mocks.MockQuerier)
	// No mock expectations on q — allowlisted methods must not query
	// the DB at all. If the interceptor reaches GetIdentityByFirebaseUID
	// for an allowlisted method, the mock will fail the test.
	interceptor := MembershipRequiredInterceptor(q)

	called := false
	_, err := interceptor(
		context.Background(), // no auth context — even unauth allowed for allowlisted methods? No: must still be authed by the prior interceptor. But we're testing in isolation, so we provide auth ctx anyway.
		nil,
		&grpc.UnaryServerInfo{FullMethod: "/pivox.api.v1.Organizations/CreateOrganization"},
		callHandler(&called),
	)
	require.NoError(t, err)
	assert.True(t, called, "allowlisted method must reach the handler")
	q.AssertExpectations(t)
}

// --- Bootstrap allowlist coverage ---

func TestMembershipInterceptor_AllAllowlistedMethods(t *testing.T) {
	q := new(mocks.MockQuerier)
	interceptor := MembershipRequiredInterceptor(q)

	// The bootstrap allowlist is the set of RPCs a freshly-registered
	// account (zero memberships) must be able to call to recover into a
	// membership. Adding to this list is a security-sensitive change;
	// every method here must be safe to call without org context.
	want := []string{
		"/pivox.api.v1.Organizations/CreateOrganization",
		"/pivox.api.v1.Organizations/ListOrganizations",
		"/pivox.api.v1.Organizations/AcceptInvitation",
		"/pivox.api.v1.Organizations/GetInvitation",
	}
	for _, method := range want {
		called := false
		_, err := interceptor(
			authedCtx(testMemberUID),
			nil,
			&grpc.UnaryServerInfo{FullMethod: method},
			callHandler(&called),
		)
		require.NoError(t, err, "method %s should be allowlisted", method)
		assert.True(t, called, "method %s should reach handler", method)
	}
	// No DB calls — allowlisted methods bypass the whole lookup path.
	q.AssertExpectations(t)
}

// --- Membership-required path: caller has memberships ---

func TestMembershipInterceptor_MemberCaller(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("GetIdentityByFirebaseUID", mock.Anything, testMemberUID).
		Return(testMemberIdentity, nil).Once()
	q.On("ListOrganizationsForIdentity", mock.Anything, testMemberIdentity.ID).
		Return([]db.Organization{{
			ID:   uuid.New(),
			Name: "acme",
		}}, nil).Once()

	interceptor := MembershipRequiredInterceptor(q)

	called := false
	_, err := interceptor(
		authedCtx(testMemberUID),
		nil,
		&grpc.UnaryServerInfo{FullMethod: "/pivox.api.v1.SomeOtherService/SomeMethod"},
		callHandler(&called),
	)
	require.NoError(t, err)
	assert.True(t, called, "caller with memberships must reach handler")
	q.AssertExpectations(t)
}

// --- Membership-required path: caller has zero memberships ---

func TestMembershipInterceptor_NoMembershipsDeniesAccess(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("GetIdentityByFirebaseUID", mock.Anything, testMemberUID).
		Return(testMemberIdentity, nil).Once()
	q.On("ListOrganizationsForIdentity", mock.Anything, testMemberIdentity.ID).
		Return([]db.Organization{}, nil).Once()

	interceptor := MembershipRequiredInterceptor(q)

	called := false
	_, err := interceptor(
		authedCtx(testMemberUID),
		nil,
		&grpc.UnaryServerInfo{FullMethod: "/pivox.api.v1.SomeOtherService/SomeMethod"},
		callHandler(&called),
	)
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.PermissionDenied, st.Code(),
		"zero memberships must produce PermissionDenied")
	assert.False(t, called, "handler must not run")
	q.AssertExpectations(t)
}

// --- Auth context missing ---

func TestMembershipInterceptor_NoAuthContextRejected(t *testing.T) {
	q := new(mocks.MockQuerier)
	// No mock expectations — interceptor must reject before any DB call.
	interceptor := MembershipRequiredInterceptor(q)

	called := false
	_, err := interceptor(
		context.Background(), // no auth context
		nil,
		&grpc.UnaryServerInfo{FullMethod: "/pivox.api.v1.SomeOtherService/SomeMethod"},
		callHandler(&called),
	)
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unauthenticated, st.Code())
	assert.False(t, called)
	q.AssertExpectations(t)
}

// --- Account lookup fails ---

func TestMembershipInterceptor_AccountLookupErrorReturnsInternal(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("GetIdentityByFirebaseUID", mock.Anything, testMemberUID).
		Return(db.Identity{}, errors.New("db down")).Once()

	interceptor := MembershipRequiredInterceptor(q)

	called := false
	_, err := interceptor(
		authedCtx(testMemberUID),
		nil,
		&grpc.UnaryServerInfo{FullMethod: "/pivox.api.v1.SomeOtherService/SomeMethod"},
		callHandler(&called),
	)
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
	assert.False(t, called)
	q.AssertExpectations(t)
}

// --- Memberships lookup fails ---

func TestMembershipInterceptor_MembershipsLookupErrorReturnsInternal(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("GetIdentityByFirebaseUID", mock.Anything, testMemberUID).
		Return(testMemberIdentity, nil).Once()
	q.On("ListOrganizationsForIdentity", mock.Anything, testMemberIdentity.ID).
		Return(nil, errors.New("db down")).Once()

	interceptor := MembershipRequiredInterceptor(q)

	called := false
	_, err := interceptor(
		authedCtx(testMemberUID),
		nil,
		&grpc.UnaryServerInfo{FullMethod: "/pivox.api.v1.SomeOtherService/SomeMethod"},
		callHandler(&called),
	)
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
	assert.False(t, called)
	q.AssertExpectations(t)
}
