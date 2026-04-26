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
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"

	"github.com/dashkan/pivox/internal/authn"
	"github.com/dashkan/pivox/internal/config"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/testutil/mocks"
)

func testDelegatedAuthConfig() config.DelegatedAuthConfig {
	return config.DelegatedAuthConfig{
		SessionTTL:   5 * time.Minute,
		PollInterval: 5 * time.Second,
	}
}

// ---------------------------------------------------------------------------
// NewInternalHooks (dev mode)
// ---------------------------------------------------------------------------

func TestNewInternalHooks_Dev(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	auth := new(mockAuthService)
	logger := slog.Default()

	h, err := NewInternalHooks(mockQ, config.SyncAuthConfig{SharedSecret: "test-secret"}, testDelegatedAuthConfig(), true, logger, auth)
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

	// These may or may not be invoked depending on the handler's early-exit
	// behavior — .Maybe() lets them pass through without failing the mock.
	mockQ.On("CreateDelegatedAuthSession", mock.Anything, mock.Anything).
		Return(db.DelegatedAuthSession{}, nil).Maybe()
	mockQ.On("ConsumeDelegatedAuthSession", mock.Anything, mock.Anything).
		Return(pgtype.Text{}, errors.New("no rows")).Maybe()
	mockQ.On("GetDelegatedAuthSessionState", mock.Anything, mock.Anything).
		Return(db.DelegatedAuthSessionState(""), errors.New("no rows")).Maybe()

	h, err := NewInternalHooks(mockQ, config.SyncAuthConfig{SharedSecret: "s"}, testDelegatedAuthConfig(), true, logger, auth)
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
		{"POST", "/internal/v1/auth:createDelegatedAuthSession"},
		{"POST", "/internal/v1/auth:completeDelegatedAuthSession"},
		{"POST", "/internal/v1/auth:pollDelegatedAuthSession"},
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
	return newTestHooksWithConfig(t, mockQ, auth, testDelegatedAuthConfig())
}

func newTestHooksWithConfig(t *testing.T, mockQ *mocks.MockQuerier, auth *mockAuthService, dcfg config.DelegatedAuthConfig) *InternalHooks {
	t.Helper()
	h, err := NewInternalHooks(mockQ, config.SyncAuthConfig{SharedSecret: "s"}, dcfg, true, slog.Default(), auth)
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
		logger:           slog.Default(),
		rateLimitEnabled: true,
		exchangeLimiter:  newIPRateLimiter(rate.Every(time.Hour), 1),
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
		logger:           slog.Default(),
		rateLimitEnabled: true,
		exchangeLimiter:  newIPRateLimiter(rate.Every(time.Hour), 1),
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

// TestRateLimit_DisabledPassthrough verifies that flipping rateLimitEnabled
// off makes the middleware a no-op even when the underlying limiter would
// reject the call.
func TestRateLimit_DisabledPassthrough(t *testing.T) {
	h := &InternalHooks{
		logger:           slog.Default(),
		rateLimitEnabled: false,
		// Burst 1, glacial refill — if the middleware touched this we'd see 429.
		exchangeLimiter: newIPRateLimiter(rate.Every(time.Hour), 1),
	}

	callCount := 0
	inner := func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
	}

	limited := h.rateLimit(inner)

	req := httptest.NewRequest("POST", "/test", nil)
	req.RemoteAddr = "10.0.0.1:9999"

	for range 5 {
		rr := httptest.NewRecorder()
		limited(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code)
	}
	assert.Equal(t, 5, callCount)
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

// TestIPRateLimiter_StaleEviction verifies that entries whose last activity
// was longer than ipStaleAfter ago are evicted on the next allow() call.
func TestIPRateLimiter_StaleEviction(t *testing.T) {
	l := newIPRateLimiter(rate.Every(time.Hour), 1)
	// Freeze virtual clock at t0.
	now := time.Unix(1_700_000_000, 0)
	l.now = func() time.Time { return now }

	// Seed three IPs at t0.
	l.allow("a")
	l.allow("b")
	l.allow("c")
	assert.Len(t, l.limiters, 3)

	// Advance past the eviction threshold. Every prior entry should disappear
	// on the next access.
	now = now.Add(ipStaleAfter + time.Second)
	l.allow("d")
	assert.Len(t, l.limiters, 1, "stale entries should be evicted")
	_, present := l.limiters["d"]
	assert.True(t, present)

	// A fresh entry within the window stays put alongside a newer one.
	now = now.Add(1 * time.Second)
	l.allow("e")
	assert.Len(t, l.limiters, 2)
}

// TestIPRateLimiter_LastSeenRefreshes verifies that repeated activity on a
// key keeps it alive past the eviction window.
func TestIPRateLimiter_LastSeenRefreshes(t *testing.T) {
	l := newIPRateLimiter(rate.Every(time.Hour), 10)
	now := time.Unix(1_700_000_000, 0)
	l.now = func() time.Time { return now }

	l.allow("active")

	// Half the window later, touch it again.
	now = now.Add(ipStaleAfter / 2)
	l.allow("active")

	// Another half-window — would be stale if lastSeen weren't refreshed.
	now = now.Add(ipStaleAfter/2 + time.Second)
	l.allow("trigger-eviction-scan")

	_, present := l.limiters["active"]
	assert.True(t, present, "active key should not be evicted")
}

// ---------------------------------------------------------------------------
// createDelegatedAuthSession
// ---------------------------------------------------------------------------

func TestCreateDelegatedAuthSession(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		setupMock  func(*mocks.MockQuerier)
		wantStatus int
		checkBody  func(*testing.T, map[string]any)
	}{
		{
			name: "happy path returns code and poll interval",
			body: "{}",
			setupMock: func(mq *mocks.MockQuerier) {
				mq.On("CreateDelegatedAuthSession", mock.Anything,
					mock.MatchedBy(func(p db.CreateDelegatedAuthSessionParams) bool {
						return p.Code != uuid.Nil && p.ExpireTime.After(time.Now())
					})).
					Return(db.DelegatedAuthSession{State: db.DelegatedAuthSessionStatePENDING}, nil)
			},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp map[string]any) {
				code, _ := resp["code"].(string)
				_, err := uuid.Parse(code)
				assert.NoError(t, err, "code should be a valid UUID")
				assert.Equal(t, float64(5), resp["pollInterval"])
			},
		},
		{
			name:       "database error",
			body:       "{}",
			wantStatus: http.StatusInternalServerError,
			setupMock: func(mq *mocks.MockQuerier) {
				mq.On("CreateDelegatedAuthSession", mock.Anything, mock.Anything).
					Return(db.DelegatedAuthSession{}, errors.New("db down"))
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockQ := new(mocks.MockQuerier)
			if tc.setupMock != nil {
				tc.setupMock(mockQ)
			}
			h := newTestHooks(t, mockQ, new(mockAuthService))

			req := httptest.NewRequest("POST", "/internal/v1/auth:createDelegatedAuthSession", strings.NewReader(tc.body))
			req.RemoteAddr = "10.0.0.1:1111"
			rr := httptest.NewRecorder()

			h.createDelegatedAuthSession(rr, req)

			assert.Equal(t, tc.wantStatus, rr.Code)
			if tc.checkBody != nil {
				var resp map[string]any
				require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
				tc.checkBody(t, resp)
			}
			mockQ.AssertExpectations(t)
		})
	}
}

func TestCreateDelegatedAuthSession_UsesConfiguredTTL(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	cfg := config.DelegatedAuthConfig{SessionTTL: 17 * time.Minute, PollInterval: 9 * time.Second}
	h := newTestHooksWithConfig(t, mockQ, new(mockAuthService), cfg)

	before := time.Now()
	mockQ.On("CreateDelegatedAuthSession", mock.Anything,
		mock.MatchedBy(func(p db.CreateDelegatedAuthSessionParams) bool {
			// Expiry should be roughly now + 17 minutes.
			delta := p.ExpireTime.Sub(before)
			return delta >= 17*time.Minute-time.Second && delta <= 17*time.Minute+time.Second
		})).
		Return(db.DelegatedAuthSession{}, nil)

	req := httptest.NewRequest("POST", "/internal/v1/auth:createDelegatedAuthSession", strings.NewReader("{}"))
	rr := httptest.NewRecorder()
	h.createDelegatedAuthSession(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.Equal(t, float64(9), resp["pollInterval"])
	mockQ.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// completeDelegatedAuthSession
// ---------------------------------------------------------------------------

func TestCompleteDelegatedAuthSession(t *testing.T) {
	validCode := uuid.New()

	tests := []struct {
		name       string
		authHeader string
		body       string
		setupAuth  func(*mockAuthService)
		setupDB    func(*mocks.MockQuerier)
		wantStatus int
	}{
		{
			name:       "happy path mints and stores custom token",
			authHeader: "Bearer valid-id-token",
			body:       fmt.Sprintf(`{"code":%q}`, validCode.String()),
			setupAuth: func(ma *mockAuthService) {
				ma.On("VerifyToken", mock.Anything, "valid-id-token").
					Return(&authn.Identity{UID: "uid-7"}, nil)
				ma.On("CreateCustomToken", mock.Anything, "uid-7").
					Return("minted-custom-token", nil)
			},
			setupDB: func(mq *mocks.MockQuerier) {
				mq.On("CompleteDelegatedAuthSession", mock.Anything,
					mock.MatchedBy(func(p db.CompleteDelegatedAuthSessionParams) bool {
						return p.Code == validCode &&
							p.CustomToken.Valid &&
							p.CustomToken.String == "minted-custom-token"
					})).
					Return(db.DelegatedAuthSession{Code: validCode, State: db.DelegatedAuthSessionStateAPPROVED}, nil)
			},
			wantStatus: http.StatusNoContent,
		},
		{
			name:       "missing authorization header",
			body:       fmt.Sprintf(`{"code":%q}`, validCode.String()),
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "invalid id token rejected before touching db",
			authHeader: "Bearer bogus",
			body:       fmt.Sprintf(`{"code":%q}`, validCode.String()),
			setupAuth: func(ma *mockAuthService) {
				ma.On("VerifyToken", mock.Anything, "bogus").
					Return(nil, errors.New("bad signature"))
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "malformed code",
			authHeader: "Bearer valid-id-token",
			body:       `{"code":"not-a-uuid"}`,
			setupAuth: func(ma *mockAuthService) {
				ma.On("VerifyToken", mock.Anything, "valid-id-token").
					Return(&authn.Identity{UID: "uid-7"}, nil)
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "session not found, already completed, or expired",
			authHeader: "Bearer valid-id-token",
			body:       fmt.Sprintf(`{"code":%q}`, validCode.String()),
			setupAuth: func(ma *mockAuthService) {
				ma.On("VerifyToken", mock.Anything, "valid-id-token").
					Return(&authn.Identity{UID: "uid-7"}, nil)
				ma.On("CreateCustomToken", mock.Anything, "uid-7").
					Return("minted-custom-token", nil)
			},
			setupDB: func(mq *mocks.MockQuerier) {
				mq.On("CompleteDelegatedAuthSession", mock.Anything, mock.Anything).
					Return(db.DelegatedAuthSession{}, errors.New("no rows"))
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "custom token mint failure",
			authHeader: "Bearer valid-id-token",
			body:       fmt.Sprintf(`{"code":%q}`, validCode.String()),
			setupAuth: func(ma *mockAuthService) {
				ma.On("VerifyToken", mock.Anything, "valid-id-token").
					Return(&authn.Identity{UID: "uid-7"}, nil)
				ma.On("CreateCustomToken", mock.Anything, "uid-7").
					Return("", errors.New("mint failed"))
			},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:       "invalid json body",
			authHeader: "Bearer valid-id-token",
			body:       "not json",
			setupAuth: func(ma *mockAuthService) {
				ma.On("VerifyToken", mock.Anything, "valid-id-token").
					Return(&authn.Identity{UID: "uid-7"}, nil)
			},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockQ := new(mocks.MockQuerier)
			auth := new(mockAuthService)
			if tc.setupAuth != nil {
				tc.setupAuth(auth)
			}
			if tc.setupDB != nil {
				tc.setupDB(mockQ)
			}
			h := newTestHooks(t, mockQ, auth)

			req := httptest.NewRequest("POST", "/internal/v1/auth:completeDelegatedAuthSession", strings.NewReader(tc.body))
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}
			rr := httptest.NewRecorder()

			h.completeDelegatedAuthSession(rr, req)

			assert.Equal(t, tc.wantStatus, rr.Code)
			mockQ.AssertExpectations(t)
			auth.AssertExpectations(t)
		})
	}
}

// ---------------------------------------------------------------------------
// pollDelegatedAuthSession
// ---------------------------------------------------------------------------

func TestPollDelegatedAuthSession(t *testing.T) {
	validCode := uuid.New()

	tests := []struct {
		name       string
		body       string
		setupDB    func(*mocks.MockQuerier)
		wantStatus int
		wantBody   map[string]any
	}{
		{
			name: "ready returns custom token and consumes session",
			body: fmt.Sprintf(`{"code":%q}`, validCode.String()),
			setupDB: func(mq *mocks.MockQuerier) {
				mq.On("ConsumeDelegatedAuthSession", mock.Anything, validCode).
					Return(pgtype.Text{String: "minted-custom-token", Valid: true}, nil)
			},
			wantStatus: http.StatusOK,
			wantBody:   map[string]any{"customToken": "minted-custom-token"},
		},
		{
			name: "pending returns status pending",
			body: fmt.Sprintf(`{"code":%q}`, validCode.String()),
			setupDB: func(mq *mocks.MockQuerier) {
				mq.On("ConsumeDelegatedAuthSession", mock.Anything, validCode).
					Return(pgtype.Text{}, errors.New("no rows"))
				mq.On("GetDelegatedAuthSessionState", mock.Anything, validCode).
					Return(db.DelegatedAuthSessionStatePENDING, nil)
			},
			wantStatus: http.StatusOK,
			wantBody:   map[string]any{"status": "pending"},
		},
		{
			name: "unknown or expired session returns 404",
			body: fmt.Sprintf(`{"code":%q}`, validCode.String()),
			setupDB: func(mq *mocks.MockQuerier) {
				mq.On("ConsumeDelegatedAuthSession", mock.Anything, validCode).
					Return(pgtype.Text{}, errors.New("no rows"))
				mq.On("GetDelegatedAuthSessionState", mock.Anything, validCode).
					Return(db.DelegatedAuthSessionState(""), errors.New("no rows"))
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "malformed code",
			body:       `{"code":"not-a-uuid"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid json",
			body:       "not json",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockQ := new(mocks.MockQuerier)
			if tc.setupDB != nil {
				tc.setupDB(mockQ)
			}
			h := newTestHooks(t, mockQ, new(mockAuthService))

			req := httptest.NewRequest("POST", "/internal/v1/auth:pollDelegatedAuthSession", strings.NewReader(tc.body))
			req.RemoteAddr = "10.0.0.2:2222"
			rr := httptest.NewRecorder()

			h.pollDelegatedAuthSession(rr, req)

			assert.Equal(t, tc.wantStatus, rr.Code)
			if tc.wantBody != nil {
				var resp map[string]any
				require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
				assert.Equal(t, tc.wantBody, resp)
			}
			mockQ.AssertExpectations(t)
		})
	}
}

// TestDelegatedAuth_DoubleConsume verifies the second poll after a successful
// consume returns 404 (single-use semantics).
func TestDelegatedAuth_DoubleConsume(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	code := uuid.New()

	mockQ.On("ConsumeDelegatedAuthSession", mock.Anything, code).
		Return(pgtype.Text{String: "tok", Valid: true}, nil).Once()
	mockQ.On("ConsumeDelegatedAuthSession", mock.Anything, code).
		Return(pgtype.Text{}, errors.New("no rows")).Once()
	mockQ.On("GetDelegatedAuthSessionState", mock.Anything, code).
		Return(db.DelegatedAuthSessionState(""), errors.New("no rows")).Once()

	h := newTestHooks(t, mockQ, new(mockAuthService))

	body := fmt.Sprintf(`{"code":%q}`, code.String())

	// First poll: ready → 200 with token.
	req1 := httptest.NewRequest("POST", "/internal/v1/auth:pollDelegatedAuthSession", strings.NewReader(body))
	rr1 := httptest.NewRecorder()
	h.pollDelegatedAuthSession(rr1, req1)
	assert.Equal(t, http.StatusOK, rr1.Code)

	// Second poll: gone → 404.
	req2 := httptest.NewRequest("POST", "/internal/v1/auth:pollDelegatedAuthSession", strings.NewReader(body))
	rr2 := httptest.NewRecorder()
	h.pollDelegatedAuthSession(rr2, req2)
	assert.Equal(t, http.StatusNotFound, rr2.Code)

	mockQ.AssertExpectations(t)
}

// TestDelegatedAuth_RateLimit verifies the create endpoint is rate-limited
// through its own limiter.
func TestDelegatedAuth_RateLimit(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	auth := new(mockAuthService)
	h := newTestHooks(t, mockQ, auth)
	// Replace the create limiter with a fixture that rejects after one call.
	h.delegatedCreateLimiter = newIPRateLimiter(rate.Every(time.Hour), 1)

	mockQ.On("CreateDelegatedAuthSession", mock.Anything, mock.Anything).
		Return(db.DelegatedAuthSession{}, nil).Maybe()

	mux := http.NewServeMux()
	h.Register(mux)

	makeReq := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/internal/v1/auth:createDelegatedAuthSession", strings.NewReader("{}"))
		req.RemoteAddr = "198.51.100.1:5555"
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		return rr
	}

	rr1 := makeReq()
	assert.NotEqual(t, http.StatusTooManyRequests, rr1.Code)

	rr2 := makeReq()
	assert.Equal(t, http.StatusTooManyRequests, rr2.Code)
}

// TestDelegatedAuth_LimitersAreIsolated verifies that exhausting the poll
// limiter does not affect the create limiter (and vice versa). This is the
// regression the per-endpoint split exists to prevent — a polling plugin
// should never be able to lock itself out of create/complete.
func TestDelegatedAuth_LimitersAreIsolated(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	auth := new(mockAuthService)
	h := newTestHooks(t, mockQ, auth)
	// Make the poll limiter reject after one call; leave the others permissive.
	h.delegatedPollLimiter = newIPRateLimiter(rate.Every(time.Hour), 1)
	h.delegatedCreateLimiter = newIPRateLimiter(rate.Every(time.Hour), 100)
	h.delegatedCompleteLimiter = newIPRateLimiter(rate.Every(time.Hour), 100)

	code := uuid.New()
	// First poll: ready. Second poll: limiter rejects before the handler runs,
	// so the mock should only see exactly one consume call.
	mockQ.On("ConsumeDelegatedAuthSession", mock.Anything, code).
		Return(pgtype.Text{String: "tok", Valid: true}, nil).Once()
	mockQ.On("CreateDelegatedAuthSession", mock.Anything, mock.Anything).
		Return(db.DelegatedAuthSession{}, nil).Maybe()

	mux := http.NewServeMux()
	h.Register(mux)

	poll := func(ip string) int {
		req := httptest.NewRequest("POST", "/internal/v1/auth:pollDelegatedAuthSession",
			strings.NewReader(fmt.Sprintf(`{"code":%q}`, code.String())))
		req.RemoteAddr = ip + ":5555"
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		return rr.Code
	}
	create := func(ip string) int {
		req := httptest.NewRequest("POST", "/internal/v1/auth:createDelegatedAuthSession",
			strings.NewReader("{}"))
		req.RemoteAddr = ip + ":5555"
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		return rr.Code
	}

	// Exhaust poll.
	assert.NotEqual(t, http.StatusTooManyRequests, poll("203.0.113.10"))
	assert.Equal(t, http.StatusTooManyRequests, poll("203.0.113.10"))

	// Create from the same IP should still succeed — different limiter.
	assert.NotEqual(t, http.StatusTooManyRequests, create("203.0.113.10"))
	assert.NotEqual(t, http.StatusTooManyRequests, create("203.0.113.10"))

	mockQ.AssertExpectations(t)
}

// TestDelegatedAuth_PollSustainsCadence verifies the poll limiter can
// handle the default poll cadence without 429-ing under normal use. This
// is the regression that motivated the fix — the old single limiter at
// rate.Every(10s) exhausted its burst before the refill could keep up.
func TestDelegatedAuth_PollSustainsCadence(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	auth := new(mockAuthService)
	h := newTestHooks(t, mockQ, auth)

	code := uuid.New()
	mockQ.On("ConsumeDelegatedAuthSession", mock.Anything, code).
		Return(pgtype.Text{}, errors.New("no rows")).Maybe()
	mockQ.On("GetDelegatedAuthSessionState", mock.Anything, code).
		Return(db.DelegatedAuthSessionStatePENDING, nil).Maybe()

	mux := http.NewServeMux()
	h.Register(mux)

	// Burst 5 at the default config — five rapid polls should all pass.
	body := fmt.Sprintf(`{"code":%q}`, code.String())
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("POST", "/internal/v1/auth:pollDelegatedAuthSession", strings.NewReader(body))
		req.RemoteAddr = "198.51.100.50:5555"
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		assert.NotEqual(t, http.StatusTooManyRequests, rr.Code, "poll %d should not be rate limited", i+1)
	}
}
