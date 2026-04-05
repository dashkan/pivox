//go:build dev

package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"

	"github.com/dashkan/pivox/internal/authn"
	"github.com/dashkan/pivox/internal/config"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/testutil/mocks"
)

// ---------------------------------------------------------------------------
// NewInternalHooks (dev mode)
// ---------------------------------------------------------------------------

func TestNewInternalHooks_Dev(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	auth := new(mockAuthService)
	logger := slog.Default()

	h, err := NewInternalHooks(mockQ, config.SyncAuthConfig{SharedSecret: "test-secret"}, logger, auth)
	require.NoError(t, err)
	require.NotNil(t, h)
	assert.NotNil(t, h.syncAuth)
	assert.NotNil(t, h.exchangeLimiter)
}

// ---------------------------------------------------------------------------
// Register
// ---------------------------------------------------------------------------

func TestRegister_AllRoutes(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	auth := new(mockAuthService)
	logger := slog.Default()

	h, err := NewInternalHooks(mockQ, config.SyncAuthConfig{SharedSecret: "s"}, logger, auth)
	require.NoError(t, err)

	mux := http.NewServeMux()
	h.Register(mux)

	// Verify all four routes respond (not 404).
	routes := []struct {
		method string
		path   string
	}{
		{"POST", "/internal/v1/accounts:sync"},
		{"POST", "/internal/v1/auth:exchangeToken"},
		{"POST", "/internal/v1/auth:depositToken"},
		{"POST", "/internal/v1/auth:consumeToken"},
	}

	for _, rt := range routes {
		t.Run(fmt.Sprintf("%s_%s", rt.method, rt.path), func(t *testing.T) {
			req := httptest.NewRequest(rt.method, rt.path, strings.NewReader("{}"))
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)
			// Should NOT be 404 — the route exists.
			assert.NotEqual(t, http.StatusNotFound, rr.Code, "route %s %s should be registered", rt.method, rt.path)
		})
	}
}

// ---------------------------------------------------------------------------
// requireSecret (dev mode)
// ---------------------------------------------------------------------------

func TestRequireSecret_CorrectSecret(t *testing.T) {
	handler := requireSecret("my-secret")
	inner := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}

	req := httptest.NewRequest("POST", "/test", nil)
	req.Header.Set("Authorization", "Bearer my-secret")
	rr := httptest.NewRecorder()
	handler(inner)(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestRequireSecret_WrongSecret(t *testing.T) {
	handler := requireSecret("my-secret")
	inner := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}

	req := httptest.NewRequest("POST", "/test", nil)
	req.Header.Set("Authorization", "Bearer wrong-secret")
	rr := httptest.NewRecorder()
	handler(inner)(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestRequireSecret_MissingHeader(t *testing.T) {
	handler := requireSecret("my-secret")
	inner := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}

	req := httptest.NewRequest("POST", "/test", nil)
	rr := httptest.NewRecorder()
	handler(inner)(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

// ---------------------------------------------------------------------------
// syncAccount
// ---------------------------------------------------------------------------

func newTestHooks(t *testing.T, mockQ *mocks.MockQuerier, auth *mockAuthService) *InternalHooks {
	t.Helper()
	h, err := NewInternalHooks(mockQ, config.SyncAuthConfig{SharedSecret: "s"}, slog.Default(), auth)
	require.NoError(t, err)
	return h
}

func TestSyncAccount_Success(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	auth := new(mockAuthService)
	h := newTestHooks(t, mockQ, auth)

	accountID := uuid.New()
	mockQ.On("UpsertAccount", mock.Anything, mock.MatchedBy(func(p db.UpsertAccountParams) bool {
		return p.FirebaseUid == "uid-123" && p.Email == "test@example.com"
	})).Return(db.Account{ID: accountID, FirebaseUid: "uid-123"}, nil)

	body := `{"firebase_uid":"uid-123","email":"test@example.com","display_name":"Test User"}`
	req := httptest.NewRequest("POST", "/internal/v1/accounts:sync", strings.NewReader(body))
	rr := httptest.NewRecorder()

	h.syncAccount(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	var resp map[string]string
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.Equal(t, accountID.String(), resp["account_id"])
	mockQ.AssertExpectations(t)
}

func TestSyncAccount_InvalidJSON(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	auth := new(mockAuthService)
	h := newTestHooks(t, mockQ, auth)

	req := httptest.NewRequest("POST", "/internal/v1/accounts:sync", strings.NewReader("not json"))
	rr := httptest.NewRecorder()

	h.syncAccount(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestSyncAccount_MissingUID(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	auth := new(mockAuthService)
	h := newTestHooks(t, mockQ, auth)

	body := `{"email":"test@example.com"}`
	req := httptest.NewRequest("POST", "/internal/v1/accounts:sync", strings.NewReader(body))
	rr := httptest.NewRecorder()

	h.syncAccount(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "firebase_uid is required")
}

func TestSyncAccount_DBError(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	auth := new(mockAuthService)
	h := newTestHooks(t, mockQ, auth)

	mockQ.On("UpsertAccount", mock.Anything, mock.Anything).
		Return(db.Account{}, errors.New("db down"))

	body := `{"firebase_uid":"uid-123","email":"test@example.com"}`
	req := httptest.NewRequest("POST", "/internal/v1/accounts:sync", strings.NewReader(body))
	rr := httptest.NewRecorder()

	h.syncAccount(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	mockQ.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// exchangeToken
// ---------------------------------------------------------------------------

func TestExchangeToken_Success(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	auth := new(mockAuthService)
	h := newTestHooks(t, mockQ, auth)

	auth.On("VerifyToken", mock.Anything, "valid-id-token").
		Return(&authn.Identity{UID: "user-1"}, nil)
	auth.On("CreateCustomToken", mock.Anything, "user-1").
		Return("custom-token-abc", nil)

	req := httptest.NewRequest("POST", "/internal/v1/auth:exchangeToken", nil)
	req.Header.Set("Authorization", "Bearer valid-id-token")
	rr := httptest.NewRecorder()

	h.exchangeToken(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	var resp map[string]string
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.Equal(t, "custom-token-abc", resp["custom_token"])
	auth.AssertExpectations(t)
}

func TestExchangeToken_MissingHeader(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	auth := new(mockAuthService)
	h := newTestHooks(t, mockQ, auth)

	req := httptest.NewRequest("POST", "/internal/v1/auth:exchangeToken", nil)
	rr := httptest.NewRecorder()

	h.exchangeToken(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestExchangeToken_InvalidToken(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	auth := new(mockAuthService)
	h := newTestHooks(t, mockQ, auth)

	auth.On("VerifyToken", mock.Anything, "bad-token").
		Return(nil, errors.New("invalid"))

	req := httptest.NewRequest("POST", "/internal/v1/auth:exchangeToken", nil)
	req.Header.Set("Authorization", "Bearer bad-token")
	rr := httptest.NewRecorder()

	h.exchangeToken(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	auth.AssertExpectations(t)
}

func TestExchangeToken_CreateCustomTokenError(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	auth := new(mockAuthService)
	h := newTestHooks(t, mockQ, auth)

	auth.On("VerifyToken", mock.Anything, "valid-token").
		Return(&authn.Identity{UID: "user-1"}, nil)
	auth.On("CreateCustomToken", mock.Anything, "user-1").
		Return("", errors.New("mint failed"))

	req := httptest.NewRequest("POST", "/internal/v1/auth:exchangeToken", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	rr := httptest.NewRecorder()

	h.exchangeToken(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	auth.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// depositToken
// ---------------------------------------------------------------------------

func TestDepositToken_Success(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	auth := new(mockAuthService)
	h := newTestHooks(t, mockQ, auth)

	code := uuid.New()
	auth.On("VerifyToken", mock.Anything, "valid-id-token").
		Return(&authn.Identity{UID: "user-1"}, nil)
	mockQ.On("CreateAuthTokenCode", mock.Anything, "valid-id-token").
		Return(db.AuthTokenCode{Code: code, IDToken: "valid-id-token"}, nil)

	body := `{"id_token":"valid-id-token"}`
	req := httptest.NewRequest("POST", "/internal/v1/auth:depositToken", strings.NewReader(body))
	rr := httptest.NewRecorder()

	h.depositToken(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	var resp map[string]string
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.Equal(t, code.String(), resp["code"])
	mockQ.AssertExpectations(t)
	auth.AssertExpectations(t)
}

func TestDepositToken_InvalidToken(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	auth := new(mockAuthService)
	h := newTestHooks(t, mockQ, auth)

	auth.On("VerifyToken", mock.Anything, "bad-token").
		Return(nil, errors.New("invalid"))

	body := `{"id_token":"bad-token"}`
	req := httptest.NewRequest("POST", "/internal/v1/auth:depositToken", strings.NewReader(body))
	rr := httptest.NewRecorder()

	h.depositToken(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	auth.AssertExpectations(t)
}

func TestDepositToken_EmptyBody(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	auth := new(mockAuthService)
	h := newTestHooks(t, mockQ, auth)

	body := `{"id_token":""}`
	req := httptest.NewRequest("POST", "/internal/v1/auth:depositToken", strings.NewReader(body))
	rr := httptest.NewRecorder()

	h.depositToken(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestDepositToken_InvalidJSON(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	auth := new(mockAuthService)
	h := newTestHooks(t, mockQ, auth)

	req := httptest.NewRequest("POST", "/internal/v1/auth:depositToken", strings.NewReader("not json"))
	rr := httptest.NewRecorder()

	h.depositToken(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestDepositToken_DBError(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	auth := new(mockAuthService)
	h := newTestHooks(t, mockQ, auth)

	auth.On("VerifyToken", mock.Anything, "valid-id-token").
		Return(&authn.Identity{UID: "user-1"}, nil)
	mockQ.On("CreateAuthTokenCode", mock.Anything, "valid-id-token").
		Return(db.AuthTokenCode{}, errors.New("db error"))

	body := `{"id_token":"valid-id-token"}`
	req := httptest.NewRequest("POST", "/internal/v1/auth:depositToken", strings.NewReader(body))
	rr := httptest.NewRecorder()

	h.depositToken(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	mockQ.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// consumeToken
// ---------------------------------------------------------------------------

func TestConsumeToken_Success(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	auth := new(mockAuthService)
	h := newTestHooks(t, mockQ, auth)

	code := uuid.New()
	mockQ.On("ConsumeAuthTokenCode", mock.Anything, code).
		Return(db.AuthTokenCode{Code: code, IDToken: "recovered-token"}, nil)

	body := fmt.Sprintf(`{"code":"%s"}`, code)
	req := httptest.NewRequest("POST", "/internal/v1/auth:consumeToken", strings.NewReader(body))
	rr := httptest.NewRecorder()

	h.consumeToken(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	var resp map[string]string
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.Equal(t, "recovered-token", resp["id_token"])
	mockQ.AssertExpectations(t)
}

func TestConsumeToken_InvalidCode(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	auth := new(mockAuthService)
	h := newTestHooks(t, mockQ, auth)

	code := uuid.New()
	mockQ.On("ConsumeAuthTokenCode", mock.Anything, code).
		Return(db.AuthTokenCode{}, errors.New("no rows"))

	body := fmt.Sprintf(`{"code":"%s"}`, code)
	req := httptest.NewRequest("POST", "/internal/v1/auth:consumeToken", strings.NewReader(body))
	rr := httptest.NewRecorder()

	h.consumeToken(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	mockQ.AssertExpectations(t)
}

func TestConsumeToken_BadUUID(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	auth := new(mockAuthService)
	h := newTestHooks(t, mockQ, auth)

	body := `{"code":"not-a-uuid"}`
	req := httptest.NewRequest("POST", "/internal/v1/auth:consumeToken", strings.NewReader(body))
	rr := httptest.NewRecorder()

	h.consumeToken(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestConsumeToken_InvalidJSON(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	auth := new(mockAuthService)
	h := newTestHooks(t, mockQ, auth)

	req := httptest.NewRequest("POST", "/internal/v1/auth:consumeToken", strings.NewReader("not json"))
	rr := httptest.NewRecorder()

	h.consumeToken(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

// ---------------------------------------------------------------------------
// rateLimit
// ---------------------------------------------------------------------------

func TestRateLimit_UnderLimit(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	auth := new(mockAuthService)
	h := newTestHooks(t, mockQ, auth)

	handlerCalled := false
	inner := func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	}

	limited := h.rateLimit(inner)

	req := httptest.NewRequest("POST", "/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	rr := httptest.NewRecorder()

	limited(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.True(t, handlerCalled)
}

func TestRateLimit_OverLimit(t *testing.T) {
	// Create hooks with a very restrictive rate limiter.
	h := &InternalHooks{
		logger:          slog.Default(),
		exchangeLimiter: newIPRateLimiter(rate.Every(time.Hour), 1),
	}

	inner := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}

	limited := h.rateLimit(inner)

	// First request: allowed.
	req := httptest.NewRequest("POST", "/test", nil)
	req.RemoteAddr = "10.0.0.1:9999"
	rr1 := httptest.NewRecorder()
	limited(rr1, req)
	assert.Equal(t, http.StatusOK, rr1.Code)

	// Second request from same IP: rate limited.
	rr2 := httptest.NewRecorder()
	limited(rr2, req)
	assert.Equal(t, http.StatusTooManyRequests, rr2.Code)
}

func TestRateLimit_XForwardedFor(t *testing.T) {
	h := &InternalHooks{
		logger:          slog.Default(),
		exchangeLimiter: newIPRateLimiter(rate.Every(time.Hour), 1),
	}

	inner := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}

	limited := h.rateLimit(inner)

	// Use X-Forwarded-For to identify the client.
	req := httptest.NewRequest("POST", "/test", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.42")
	rr1 := httptest.NewRecorder()
	limited(rr1, req)
	assert.Equal(t, http.StatusOK, rr1.Code)

	// Second request from same XFF IP.
	rr2 := httptest.NewRecorder()
	limited(rr2, req)
	assert.Equal(t, http.StatusTooManyRequests, rr2.Code)
}

// ---------------------------------------------------------------------------
// ipRateLimiter
// ---------------------------------------------------------------------------

func TestNewIPRateLimiter(t *testing.T) {
	l := newIPRateLimiter(rate.Limit(10), 5)
	require.NotNil(t, l)
	assert.NotNil(t, l.limiters)
	assert.Equal(t, rate.Limit(10), l.r)
	assert.Equal(t, 5, l.burst)
}

func TestIPRateLimiter_Allow(t *testing.T) {
	l := newIPRateLimiter(rate.Every(time.Hour), 1)

	// First call: allowed.
	assert.True(t, l.allow("key-a"))

	// Second call same key: denied.
	assert.False(t, l.allow("key-a"))

	// Different key: allowed.
	assert.True(t, l.allow("key-b"))
}

