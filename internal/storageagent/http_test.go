package storageagent

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testSigningKey is the HS256 secret shared across http test files.
// 32 bytes — enough entropy for HMAC test fixtures.
var testSigningKey = []byte("test-secret-key-for-jwt-signing!")

// newTestHTTPServer wires an HTTPServer with the standard test
// fixtures: in-memory session store, small cache, fresh endpoint
// store, fresh denied-pattern table, and a stderr-only error logger.
// Shared across http_test.go and http_auth_test.go.
func newTestHTTPServer(t *testing.T) (*HTTPServer, *SessionStore, *EndpointStore, *DeniedPatterns) {
	t.Helper()
	sessions := NewSessionStore(SessionStoreConfig{})
	cache := NewMemoryCache(100, 1024*1024)
	endpoints := NewEndpointStore(cache)
	denied := NewDeniedPatterns(DeniedPatternsConfig{})
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	srv := NewHTTPServer(Config{
		Sessions:   sessions,
		Endpoints:  endpoints,
		Denied:     denied,
		SigningKey: testSigningKey,
		CORSOrigin: "https://example.com",
		Logger:     logger,
	})
	return srv, sessions, endpoints, denied
}

// makeJWT builds a valid HS256 JWT around the given claims signed
// with key. Used by every test that needs a session cookie.
func makeJWT(claims map[string]any, key []byte) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payloadJSON, _ := json.Marshal(claims)
	payload := base64.RawURLEncoding.EncodeToString(payloadJSON)
	headerPayload := header + "." + payload
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(headerPayload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return headerPayload + "." + sig
}

// ---------------------------------------------------------------------------
// Constructor
// ---------------------------------------------------------------------------

func TestNewHTTPServer(t *testing.T) {
	srv, _, _, _ := newTestHTTPServer(t)
	require.NotNil(t, srv)
	assert.NotNil(t, srv.sessions)
	assert.NotNil(t, srv.endpoints)
	assert.NotNil(t, srv.denied)
	assert.Equal(t, "https://example.com", srv.corsOrigin)
}

// ---------------------------------------------------------------------------
// CORS — runs before auth so OPTIONS doesn't need a session cookie.
// ---------------------------------------------------------------------------

func TestHTTP_CORSPreflight(t *testing.T) {
	srv, _, _, _ := newTestHTTPServer(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodOptions, "/media/image.png", nil)
	srv.ServeHTTP(w, r)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Equal(t, "https://example.com", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "true", w.Header().Get("Access-Control-Allow-Credentials"))
	assert.Equal(t, "Content-Type", w.Header().Get("Access-Control-Allow-Headers"))
	assert.Equal(t, "GET, PUT, OPTIONS", w.Header().Get("Access-Control-Allow-Methods"))
}

func TestHTTP_CORSHeaders_OnGetRequest(t *testing.T) {
	srv, _, _, _ := newTestHTTPServer(t)

	// CORS headers are set before auth, so an unauthenticated GET
	// still leaks the CORS metadata browsers need.
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/ep/file.txt", nil)
	srv.ServeHTTP(w, r)

	assert.Equal(t, "https://example.com", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "true", w.Header().Get("Access-Control-Allow-Credentials"))
}

func TestHTTP_SetCORSOrigin(t *testing.T) {
	srv, _, _, _ := newTestHTTPServer(t)
	srv.SetCORSOrigin("https://new-origin.com")

	w := httptest.NewRecorder()
	srv.setCORSHeaders(w)
	assert.Equal(t, "https://new-origin.com", w.Header().Get("Access-Control-Allow-Origin"))
}

// ---------------------------------------------------------------------------
// SetSigningKey — verifies that subsequent JWTs validate with the new key.
// ---------------------------------------------------------------------------

func TestHTTP_SetSigningKey(t *testing.T) {
	srv, _, _, _ := newTestHTTPServer(t)
	newKey := []byte("new-signing-key-1234567890!!!!!!")
	srv.SetSigningKey(newKey)

	token := makeJWT(map[string]any{
		"sub": "user",
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	}, newKey)

	result, err := srv.validateJWT(token)
	require.NoError(t, err)
	assert.Equal(t, "user", result["sub"])
}

// ---------------------------------------------------------------------------
// validateJWT — table-driven malformed-input + happy path.
// ---------------------------------------------------------------------------

func TestValidateJWT_Valid(t *testing.T) {
	srv, _, _, _ := newTestHTTPServer(t)

	token := makeJWT(map[string]any{
		"sub":   "user-123",
		"token": "opaque-abc",
		"exp":   float64(time.Now().Add(time.Hour).Unix()),
	}, testSigningKey)

	result, err := srv.validateJWT(token)
	require.NoError(t, err)
	assert.Equal(t, "user-123", result["sub"])
	assert.Equal(t, "opaque-abc", result["token"])
}

func TestValidateJWT_Expired(t *testing.T) {
	srv, _, _, _ := newTestHTTPServer(t)

	token := makeJWT(map[string]any{
		"sub": "user-123",
		"exp": float64(time.Now().Add(-time.Hour).Unix()),
	}, testSigningKey)

	_, err := srv.validateJWT(token)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "token expired")
}

func TestValidateJWT_InvalidSignature(t *testing.T) {
	srv, _, _, _ := newTestHTTPServer(t)

	token := makeJWT(map[string]any{
		"sub": "user-123",
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	}, []byte("wrong-key-for-signing-12345678!"))

	_, err := srv.validateJWT(token)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid signature")
}

func TestValidateJWT_MalformedToken(t *testing.T) {
	srv, _, _, _ := newTestHTTPServer(t)

	cases := []struct {
		name  string
		token string
	}{
		{"empty", ""},
		{"one part", "abc"},
		{"two parts", "abc.def"},
		{"malformed", "not-a-jwt"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := srv.validateJWT(tc.token)
			require.Error(t, err)
		})
	}
}

func TestValidateJWT_MissingExp(t *testing.T) {
	srv, _, _, _ := newTestHTTPServer(t)

	token := makeJWT(map[string]any{"sub": "user-123"}, testSigningKey) // no exp

	_, err := srv.validateJWT(token)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing or invalid exp claim")
}

func TestValidateJWT_InvalidPayload(t *testing.T) {
	srv, _, _, _ := newTestHTTPServer(t)

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`not-json`))
	headerPayload := header + "." + payload
	mac := hmac.New(sha256.New, testSigningKey)
	mac.Write([]byte(headerPayload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	token := fmt.Sprintf("%s.%s", headerPayload, sig)

	_, err := srv.validateJWT(token)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal")
}
