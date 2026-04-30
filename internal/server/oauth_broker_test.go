package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/dashkan/pivox/internal/config"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/testutil/mocks"
)

// passthroughEncryptor satisfies crypto.Encryptor without depending
// on the dev-tagged NoOpEncryptor — keeps these tests buildable
// regardless of build tags.
type passthroughEncryptor struct{}

func (passthroughEncryptor) Encrypt(b []byte) ([]byte, error) { return b, nil }
func (passthroughEncryptor) Decrypt(b []byte) ([]byte, error) { return b, nil }

// brokerHarness builds an OAuthBroker with a mocked querier and an
// in-memory IdP (httptest.Server) for the discovery + token-exchange
// stages. Returns the broker, the mocked querier, and the IdP base
// URL the test can configure SsoConfig rows against.
type brokerHarness struct {
	broker *OAuthBroker
	q      *mocks.MockQuerier
	idp    *idpStub
}

// idpStub is a minimal OIDC IdP — discovery doc + token endpoint.
// Authorize URL is reflected back in discovery so the broker can
// build a redirect URL pointing at it; for the test we never
// actually drive the authorize request (we test broker→IdP only at
// the token-exchange step, which is the leg that handles secrets).
type idpStub struct {
	server  *httptest.Server
	idToken string
}

func newIDPStub(t *testing.T, idToken string) *idpStub {
	t.Helper()
	stub := &idpStub{idToken: idToken}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                 base,
			"authorization_endpoint": base + "/authorize",
			"token_endpoint":         base + "/token",
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		// Smoke-check the form has the broker's client_secret. We
		// don't strictly verify it (test may set any secret) but we
		// do verify the broker is sending one — i.e. the decryption
		// path worked.
		if r.PostFormValue("client_secret") == "" {
			http.Error(w, "missing client_secret", http.StatusBadRequest)
			return
		}
		if r.PostFormValue("grant_type") != "authorization_code" {
			http.Error(w, "wrong grant_type", http.StatusBadRequest)
			return
		}
		if r.PostFormValue("code") == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id_token":     stub.idToken,
			"access_token": "atok",
			"token_type":   "Bearer",
		})
	})
	stub.server = httptest.NewServer(mux)
	t.Cleanup(stub.server.Close)
	return stub
}

func newBrokerHarness(t *testing.T) *brokerHarness {
	t.Helper()
	q := new(mocks.MockQuerier)
	idp := newIDPStub(t, "fake-id-token")
	cfg := config.OAuthBrokerConfig{
		AppKey:             testAppKey,
		BaseURL:            "http://broker.test",
		GitHubClientID:     "gh-id",
		GitHubClientSecret: "gh-secret",
	}
	b := NewOAuthBroker(OAuthBrokerConfig{
		Queries:   q,
		Encryptor: passthroughEncryptor{},
		Broker:    cfg,
		Logger:    slog.Default(),
	})
	return &brokerHarness{broker: b, q: q, idp: idp}
}

func TestBrokerStart_GitHubBuildsAuthorizeURL(t *testing.T) {
	h := newBrokerHarness(t)

	req := httptest.NewRequest(
		"GET",
		"/internal/v1/auth/github/start?return=pivox://auth-complete",
		nil)
	req.SetPathValue("provider", "github")
	w := httptest.NewRecorder()

	h.broker.start(w, req)

	require.Equal(t, http.StatusFound, w.Code)
	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "github.com", loc.Host)
	assert.Equal(t, "/login/oauth/authorize", loc.Path)
	q := loc.Query()
	assert.Equal(t, "gh-id", q.Get("client_id"))
	assert.Equal(t, "code", q.Get("response_type"))
	assert.Equal(t, "http://broker.test/internal/v1/auth/github/callback", q.Get("redirect_uri"))
	assert.NotEmpty(t, q.Get("state"))
	// GitHub's allow_signup extra param survives.
	assert.Equal(t, "true", q.Get("allow_signup"))
	// No nonce on GitHub flow (only OIDC).
	assert.Empty(t, q.Get("nonce"))
}

func TestBrokerStart_OIDCAddsNonce(t *testing.T) {
	h := newBrokerHarness(t)

	orgID := uuid.New()
	h.q.On("GetSsoConfigByFirebaseProviderID", mock.Anything, "oidc.acme").Return(
		db.GetSsoConfigByFirebaseProviderIDRow{
			OrgID:                  orgID,
			FirebaseProviderID:     "oidc.acme",
			OidcConfig:             []byte(`{"issuer":"` + h.idp.server.URL + `","client_id":"pivox"}`),
			ClientSecretCiphertext: []byte("supersecret"),
			OrgSlug:                "acme",
		}, nil)

	req := httptest.NewRequest(
		"GET",
		"/internal/v1/auth/oidc.acme/start?return=pivox://auth-complete",
		nil)
	req.SetPathValue("provider", "oidc.acme")
	w := httptest.NewRecorder()

	h.broker.start(w, req)

	require.Equal(t, http.StatusFound, w.Code)
	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	q := loc.Query()
	assert.Equal(t, "pivox", q.Get("client_id"))
	assert.NotEmpty(t, q.Get("state"))
	assert.NotEmpty(t, q.Get("nonce"), "OIDC flow must include nonce in authorize")
	// FB OIDC convention: the IdP-facing nonce param is
	// hex(SHA256(rawNonce)). The rawNonce itself stays bound to the
	// signed state (`state.N`) so the callback can return it to
	// native unhashed for OAuthProvider.credential to verify.
	state, err := verifyOAuthState(testAppKey, q.Get("state"))
	require.NoError(t, err)
	sum := sha256.Sum256([]byte(state.N))
	assert.Equal(t, hex.EncodeToString(sum[:]), q.Get("nonce"),
		"nonce sent to IdP must be hex(SHA256(rawNonce)) per Firebase OIDC contract")
}

func TestBrokerStart_RejectsBadReturnURL(t *testing.T) {
	h := newBrokerHarness(t)

	tests := []struct {
		name string
		ret  string
	}{
		{"empty", ""},
		{"foreign origin", "https://evil.test/landing"},
		{"non-allowlisted scheme", "http://localhost:1234/x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(
				"GET",
				"/internal/v1/auth/github/start?return="+url.QueryEscape(tt.ret),
				nil)
			req.SetPathValue("provider", "github")
			w := httptest.NewRecorder()
			h.broker.start(w, req)
			assert.Equal(t, http.StatusBadRequest, w.Code)
		})
	}
}

func TestBrokerStart_UnknownProvider404(t *testing.T) {
	h := newBrokerHarness(t)

	h.q.On("GetSsoConfigByFirebaseProviderID", mock.Anything, "oidc.unknown").Return(
		db.GetSsoConfigByFirebaseProviderIDRow{}, pgxNoRows())

	req := httptest.NewRequest(
		"GET",
		"/internal/v1/auth/oidc.unknown/start?return=pivox://auth-complete",
		nil)
	req.SetPathValue("provider", "oidc.unknown")
	w := httptest.NewRecorder()
	h.broker.start(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestBrokerCallback_OIDCRoundTrip(t *testing.T) {
	h := newBrokerHarness(t)

	orgID := uuid.New()
	h.q.On("GetSsoConfigByFirebaseProviderID", mock.Anything, "oidc.acme").Return(
		db.GetSsoConfigByFirebaseProviderIDRow{
			OrgID:                  orgID,
			FirebaseProviderID:     "oidc.acme",
			OidcConfig:             []byte(`{"issuer":"` + h.idp.server.URL + `","client_id":"pivox"}`),
			ClientSecretCiphertext: []byte("supersecret"),
			OrgSlug:                "acme",
		}, nil)

	state, _, err := signOAuthState(testAppKey, "pivox://auth-complete", "oidc.acme")
	require.NoError(t, err)

	req := httptest.NewRequest(
		"GET",
		"/internal/v1/auth/oidc.acme/callback?code=fake-code&state="+url.QueryEscape(state),
		nil)
	req.SetPathValue("provider", "oidc.acme")
	w := httptest.NewRecorder()

	h.broker.callback(w, req)

	require.Equal(t, http.StatusFound, w.Code)
	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "pivox", loc.Scheme)
	assert.Equal(t, "auth-complete", loc.Host)

	// Fragment carries provider, kind, token, access_token, nonce.
	frag, err := url.ParseQuery(loc.Fragment)
	require.NoError(t, err)
	assert.Equal(t, "oidc.acme", frag.Get("provider"))
	assert.Equal(t, "oidc_id_token", frag.Get("kind"))
	assert.Equal(t, "fake-id-token", frag.Get("token"))
	assert.Equal(t, "atok", frag.Get("access_token"))
	assert.NotEmpty(t, frag.Get("nonce"), "OIDC callback must echo nonce for FB rawNonce binding")

	// Nonce in fragment must equal nonce in original state.
	originalState, err := verifyOAuthState(testAppKey, state)
	require.NoError(t, err)
	assert.Equal(t, originalState.N, frag.Get("nonce"))
}

func TestBrokerCallback_RejectsTamperedState(t *testing.T) {
	h := newBrokerHarness(t)

	req := httptest.NewRequest(
		"GET",
		"/internal/v1/auth/github/callback?code=fake&state=invalid.state.token",
		nil)
	req.SetPathValue("provider", "github")
	w := httptest.NewRecorder()

	h.broker.callback(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	// The reason code goes to logs, not the user-facing HTML — pin
	// the status only and verify the friendly copy is present.
	assert.Contains(t, w.Body.String(), "Couldn't complete sign-in")
	assert.NotEmpty(t, w.Header().Get("X-Correlation-Id"))
}

func TestBrokerCallback_RejectsProviderMismatch(t *testing.T) {
	h := newBrokerHarness(t)

	// State is signed for "github" but the URL provider is "oidc.acme".
	state, _, err := signOAuthState(testAppKey, "pivox://auth-complete", "github")
	require.NoError(t, err)

	req := httptest.NewRequest(
		"GET",
		"/internal/v1/auth/oidc.acme/callback?code=fake&state="+url.QueryEscape(state),
		nil)
	req.SetPathValue("provider", "oidc.acme")
	w := httptest.NewRecorder()

	h.broker.callback(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Couldn't complete sign-in")
	assert.NotEmpty(t, w.Header().Get("X-Correlation-Id"))
}

func TestBrokerCallback_BubblesProviderError(t *testing.T) {
	h := newBrokerHarness(t)

	state, _, err := signOAuthState(testAppKey, "pivox://auth-complete", "github")
	require.NoError(t, err)

	req := httptest.NewRequest(
		"GET",
		"/internal/v1/auth/github/callback?error=access_denied&error_description=user_denied&state="+url.QueryEscape(state),
		nil)
	req.SetPathValue("provider", "github")
	w := httptest.NewRecorder()

	h.broker.callback(w, req)
	require.Equal(t, http.StatusFound, w.Code)
	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	frag, err := url.ParseQuery(loc.Fragment)
	require.NoError(t, err)
	assert.Equal(t, "access_denied", frag.Get("error"))
	assert.Equal(t, "user_denied", frag.Get("error_description"))
}

func pgxNoRows() error { return pgx.ErrNoRows }

func TestRequireSecureIssuer(t *testing.T) {
	tests := []struct {
		name   string
		issuer string
		ok     bool
	}{
		{"https public", "https://login.example.com/realms/acme", true},
		{"https with port", "https://idp.example.com:8443/oidc", true},
		{"http localhost", "http://localhost:8080/realms/acme", true},
		{"http 127.0.0.1", "http://127.0.0.1:8080/realms/acme", true},
		{"http ::1", "http://[::1]:8080/realms/acme", true},
		{"http public — rejected", "http://idp.example.com/oidc", false},
		{"plain string — rejected", "idp.example.com", false},
		{"empty — rejected", "", false},
		{"file scheme — rejected", "file:///etc/passwd", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := requireSecureIssuer(tt.issuer)
			if tt.ok {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestBrokerStart_RejectsHttpIssuer(t *testing.T) {
	// An org admin who somehow set an http:// (non-localhost) issuer
	// must not be able to drive a flow that POSTs the client_secret
	// over plaintext. The broker collapses this to the same 404 a
	// missing row produces (existence-probe defense).
	h := newBrokerHarness(t)

	orgID := uuid.New()
	h.q.On("GetSsoConfigByFirebaseProviderID", mock.Anything, "oidc.evil").Return(
		db.GetSsoConfigByFirebaseProviderIDRow{
			OrgID:                  orgID,
			FirebaseProviderID:     "oidc.evil",
			OidcConfig:             []byte(`{"issuer":"http://idp.example.com","client_id":"x"}`),
			ClientSecretCiphertext: []byte("supersecret"),
			OrgSlug:                "evil",
		}, nil)

	req := httptest.NewRequest("GET",
		"/internal/v1/auth/oidc.evil/start?return=pivox://auth-complete",
		nil)
	req.SetPathValue("provider", "oidc.evil")
	w := httptest.NewRecorder()

	h.broker.start(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code,
		"http:// issuer must be rejected as 404 (existence-probe defense)")
}

func TestBroker_OnDiscoveryFailure_ReturnsConfigError(t *testing.T) {
	h := newBrokerHarness(t)

	// Issuer points at an unreachable host so discovery fails.
	orgID := uuid.New()
	h.q.On("GetSsoConfigByFirebaseProviderID", mock.Anything, "oidc.acme").Return(
		db.GetSsoConfigByFirebaseProviderIDRow{
			OrgID:                  orgID,
			FirebaseProviderID:     "oidc.acme",
			OidcConfig:             []byte(`{"issuer":"http://127.0.0.1:1/unreachable","client_id":"pivox"}`),
			ClientSecretCiphertext: []byte("supersecret"),
			OrgSlug:                "acme",
		}, nil)

	req := httptest.NewRequest(
		"GET",
		"/internal/v1/auth/oidc.acme/start?return=pivox://auth-complete",
		nil)
	req.SetPathValue("provider", "oidc.acme")
	w := httptest.NewRecorder()
	h.broker.start(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
