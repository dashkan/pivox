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
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

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

	h, err := NewInternalHooks(InternalHooksConfig{Queries: mockQ, SyncAuth: config.SyncAuthConfig{SharedSecret: "test-secret"}, DelegatedAuth: testDelegatedAuthConfig(), Logger: logger, Auth: auth})
	require.NoError(t, err)
	require.NotNil(t, h)
	assert.NotNil(t, h.syncAuth)
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
	mockQ.On("ResolveProviderByDomain", mock.Anything, mock.Anything).
		Return(db.ResolveProviderByDomainRow{}, pgx.ErrNoRows).Maybe()

	h, err := NewInternalHooks(InternalHooksConfig{Queries: mockQ, SyncAuth: config.SyncAuthConfig{SharedSecret: "s"}, DelegatedAuth: testDelegatedAuthConfig(), Logger: logger, Auth: auth})
	require.NoError(t, err)

	mux := http.NewServeMux()
	h.Register(mux)

	// Verify all four routes respond (not 404).
	routes := []struct {
		method string
		path   string
	}{
		{"POST", "/internal/v1/auth:syncIdentity"},
		{"POST", "/internal/v1/auth:exchangeToken"},
		{"POST", "/internal/v1/auth:depositToken"},
		{"POST", "/internal/v1/auth:consumeToken"},
		{"POST", "/internal/v1/auth:createDelegatedAuthSession"},
		{"POST", "/internal/v1/auth:completeDelegatedAuthSession"},
		{"POST", "/internal/v1/auth:pollDelegatedAuthSession"},
		{"POST", "/internal/v1/auth:resolveProvider"},
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
// syncIdentity
// ---------------------------------------------------------------------------

func newTestHooks(t *testing.T, mockQ *mocks.MockQuerier, auth *mockAuthService) *InternalHooks {
	t.Helper()
	return newTestHooksWithConfig(t, mockQ, auth, testDelegatedAuthConfig())
}

func newTestHooksWithConfig(t *testing.T, mockQ *mocks.MockQuerier, auth *mockAuthService, dcfg config.DelegatedAuthConfig) *InternalHooks {
	t.Helper()
	h, err := NewInternalHooks(InternalHooksConfig{Queries: mockQ, SyncAuth: config.SyncAuthConfig{SharedSecret: "s"}, DelegatedAuth: dcfg, Logger: slog.Default(), Auth: auth})
	require.NoError(t, err)
	return h
}

// --- resolveProvider ---

func TestResolveProvider_Success(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	auth := new(mockAuthService)
	h := newTestHooks(t, mockQ, auth)

	mockQ.On("ResolveProviderByDomain", mock.Anything, "acme.com").
		Return(db.ResolveProviderByDomainRow{FirebaseProviderID: "oidc.acme"}, nil)

	body := `{"email":"alice@acme.com"}`
	req := httptest.NewRequest("POST", "/internal/v1/auth:resolveProvider", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.resolveProvider(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	var resp resolveProviderResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.Equal(t, "oidc.acme", resp.ProviderID)
}

func TestResolveProvider_DomainCaseInsensitive(t *testing.T) {
	// Email-domain matching is case-insensitive on the request side.
	// Domains are stored lowercase (DB CHECK enforces it), so the
	// handler lowercases before lookup.
	mockQ := new(mocks.MockQuerier)
	auth := new(mockAuthService)
	h := newTestHooks(t, mockQ, auth)

	mockQ.On("ResolveProviderByDomain", mock.Anything, "acme.com").
		Return(db.ResolveProviderByDomainRow{FirebaseProviderID: "oidc.acme"}, nil)

	req := httptest.NewRequest("POST", "/internal/v1/auth:resolveProvider",
		strings.NewReader(`{"email":"Alice@ACME.com"}`))
	rr := httptest.NewRecorder()
	h.resolveProvider(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestResolveProvider_NotFoundReturns404(t *testing.T) {
	// Three failure modes collapse to 404 (avoid enumeration):
	// domain not claimed, domain not VERIFIED, SsoConfig disabled.
	// All surface as pgx.ErrNoRows from the join.
	mockQ := new(mocks.MockQuerier)
	auth := new(mockAuthService)
	h := newTestHooks(t, mockQ, auth)

	mockQ.On("ResolveProviderByDomain", mock.Anything, "unknown.com").
		Return(db.ResolveProviderByDomainRow{}, pgx.ErrNoRows)

	req := httptest.NewRequest("POST", "/internal/v1/auth:resolveProvider",
		strings.NewReader(`{"email":"alice@unknown.com"}`))
	rr := httptest.NewRecorder()
	h.resolveProvider(rr, req)
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestResolveProvider_MissingEmail400(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	auth := new(mockAuthService)
	h := newTestHooks(t, mockQ, auth)

	cases := []string{
		`{}`,                  // missing field
		`{"email":""}`,        // empty
		`{"email":"no-at"}`,   // no @
		`{"email":"@nohost"}`, // empty local part
		`{"email":"local@"}`,  // empty domain part
	}
	for _, body := range cases {
		t.Run(body, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/internal/v1/auth:resolveProvider",
				strings.NewReader(body))
			rr := httptest.NewRecorder()
			h.resolveProvider(rr, req)
			assert.Equal(t, http.StatusBadRequest, rr.Code)
		})
	}
}

func TestResolveProvider_MalformedJSON400(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	auth := new(mockAuthService)
	h := newTestHooks(t, mockQ, auth)

	req := httptest.NewRequest("POST", "/internal/v1/auth:resolveProvider",
		strings.NewReader("not-json"))
	rr := httptest.NewRecorder()
	h.resolveProvider(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestResolveProvider_DBErrorReturns500(t *testing.T) {
	// Real DB errors are 500, not 404 — operators see the failure
	// and clients can retry. The 404 path is reserved for "no
	// provider applies."
	mockQ := new(mocks.MockQuerier)
	auth := new(mockAuthService)
	h := newTestHooks(t, mockQ, auth)

	mockQ.On("ResolveProviderByDomain", mock.Anything, mock.Anything).
		Return(db.ResolveProviderByDomainRow{}, errors.New("connection refused"))

	req := httptest.NewRequest("POST", "/internal/v1/auth:resolveProvider",
		strings.NewReader(`{"email":"alice@acme.com"}`))
	rr := httptest.NewRecorder()
	h.resolveProvider(rr, req)
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}

// --- emailDomain helper ---

func TestEmailDomain(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"alice@example.com", "example.com"},
		{"Alice@EXAMPLE.com", "example.com"},
		{"a+tag@sub.example.com", "sub.example.com"},
		{"weird@@double.com", "double.com"}, // last @ wins
		{"", ""},
		{"no-at", ""},
		{"@only-at", ""},
		{"only-at@", ""},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			assert.Equal(t, c.want, emailDomain(c.in))
		})
	}
}

func TestSyncFirebaseIdentity_Success(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	auth := new(mockAuthService)
	h := newTestHooks(t, mockQ, auth)

	identityID := uuid.New()
	mockQ.On("UpsertIdentity", mock.Anything, mock.MatchedBy(func(p db.UpsertIdentityParams) bool {
		return p.FirebaseUid == "uid-123" && p.Email == "test@example.com"
	})).Return(db.Identity{ID: identityID, FirebaseUid: "uid-123"}, nil)

	body := `{"firebase_uid":"uid-123","email":"test@example.com","display_name":"Test User"}`
	req := httptest.NewRequest("POST", "/internal/v1/auth:syncIdentity", strings.NewReader(body))
	rr := httptest.NewRecorder()

	h.syncIdentity(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	var resp map[string]string
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.Equal(t, identityID.String(), resp["identity_id"])
	mockQ.AssertExpectations(t)
}

func TestSyncFirebaseIdentity_InvalidJSON(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	auth := new(mockAuthService)
	h := newTestHooks(t, mockQ, auth)

	req := httptest.NewRequest("POST", "/internal/v1/auth:syncIdentity", strings.NewReader("not json"))
	rr := httptest.NewRecorder()

	h.syncIdentity(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestSyncFirebaseIdentity_MissingUID(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	auth := new(mockAuthService)
	h := newTestHooks(t, mockQ, auth)

	body := `{"email":"test@example.com"}`
	req := httptest.NewRequest("POST", "/internal/v1/auth:syncIdentity", strings.NewReader(body))
	rr := httptest.NewRecorder()

	h.syncIdentity(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "firebase_uid is required")
}

func TestSyncFirebaseIdentity_DBError(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	auth := new(mockAuthService)
	h := newTestHooks(t, mockQ, auth)

	mockQ.On("UpsertIdentity", mock.Anything, mock.Anything).
		Return(db.Identity{}, errors.New("db down"))

	body := `{"firebase_uid":"uid-123","email":"test@example.com"}`
	req := httptest.NewRequest("POST", "/internal/v1/auth:syncIdentity", strings.NewReader(body))
	rr := httptest.NewRecorder()

	h.syncIdentity(rr, req)

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
