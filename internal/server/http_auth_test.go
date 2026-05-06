package server

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/dashkan/pivox/internal/authn"
	"github.com/dashkan/pivox/internal/testutil/authnmock"
)

// silentLogger discards log output so test runs stay quiet.
func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRequireAuth_MissingAuthHeader_Returns401(t *testing.T) {
	auth := authnmock.NewMockService(t)
	mw := RequireAuth(auth, silentLogger())

	called := false
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/protected", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.False(t, called, "handler must not run when auth fails")
}

func TestRequireAuth_MalformedBearer_Returns401(t *testing.T) {
	auth := authnmock.NewMockService(t)
	mw := RequireAuth(auth, silentLogger())

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/protected", nil)
	req.Header.Set("Authorization", "BearerNoSpace token") // missing the space
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	auth.AssertNotCalled(t, "VerifyToken", mock.Anything, mock.Anything)
}

func TestRequireAuth_InvalidToken_Returns401(t *testing.T) {
	auth := authnmock.NewMockService(t)
	auth.EXPECT().VerifyToken(mock.Anything, "bad-token").
		Return(nil, fmt.Errorf("invalid signature"))

	mw := RequireAuth(auth, silentLogger())
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/protected", nil)
	req.Header.Set("Authorization", "Bearer bad-token")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestRequireAuth_TokenWithoutPivoxClaim_Returns401(t *testing.T) {
	auth := authnmock.NewMockService(t)
	auth.EXPECT().VerifyToken(mock.Anything, "no-claim-token").
		Return(&authn.Identity{
			UID:    "fb-uid",
			Claims: map[string]any{}, // no pivox_user_id claim
		}, nil)

	mw := RequireAuth(auth, silentLogger())
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/protected", nil)
	req.Header.Set("Authorization", "Bearer no-claim-token")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestRequireAuth_ValidToken_AugmentsContext(t *testing.T) {
	uid := uuid.New()
	auth := authnmock.NewMockService(t)
	auth.EXPECT().VerifyToken(mock.Anything, "good-token").
		Return(&authn.Identity{
			UID:    "fb-uid",
			Claims: map[string]any{"pivox_user_id": uid.String()},
		}, nil)

	mw := RequireAuth(auth, silentLogger())

	var (
		gotUID   string
		gotPivox uuid.UUID
		gotUIDOK bool
		gotPivOK bool
		bodyRan  bool
	)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUID, gotUIDOK = AuthenticatedUID(r.Context())
		gotPivox, gotPivOK = PivoxUserID(r.Context())
		bodyRan = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/protected", nil)
	req.Header.Set("Authorization", "Bearer good-token")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	assert.True(t, bodyRan, "downstream handler must run on success")
	assert.True(t, gotUIDOK, "AuthenticatedUID must be populated for downstream handler")
	assert.Equal(t, "fb-uid", gotUID)
	assert.True(t, gotPivOK, "PivoxUserID must be populated for downstream handler")
	assert.Equal(t, uid, gotPivox)
}
