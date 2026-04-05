//go:build dev

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
	"path/filepath"
	"testing"
	"time"

	agentv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/agent/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testSigningKey = []byte("test-secret-key-for-jwt-signing!")

func newTestHTTPServer(t *testing.T) (*HTTPServer, *SessionStore, *EndpointStore, *DeniedPatterns) {
	t.Helper()
	sessions := NewSessionStore()
	cache := NewMemoryCache(100, 1024*1024)
	endpoints := NewEndpointStore(cache)
	denied := NewDeniedPatterns()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	srv := NewHTTPServer(sessions, endpoints, denied, testSigningKey, "https://example.com", logger)
	return srv, sessions, endpoints, denied
}

// makeJWT creates a valid HS256 JWT for testing.
func makeJWT(claims map[string]interface{}, key []byte) string {
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
// CORS
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
// Dev mode skips auth — test denied patterns + endpoint routing
// ---------------------------------------------------------------------------

func TestHTTP_DeniedPattern(t *testing.T) {
	srv, _, _, denied := newTestHTTPServer(t)
	denied.Update([]string{"/secret/*"})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/secret/data", nil)
	srv.ServeHTTP(w, r)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHTTP_NoEndpoint(t *testing.T) {
	srv, _, _, _ := newTestHTTPServer(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/media/image.png", nil)
	srv.ServeHTTP(w, r)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHTTP_ServeFileRouting(t *testing.T) {
	srv, _, endpoints, _ := newTestHTTPServer(t)

	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "video.mp4"), []byte("video data"), 0o644)
	require.NoError(t, err)

	err = endpoints.Update([]*agentv1.EndpointConfig{
		{
			Name: "organizations/acme/storageGateways/gw1/endpoints/media",
			Configuration: &agentv1.EndpointConfig_Filesystem{
				Filesystem: &agentv1.FileSystemEndpointConfig{Path: dir},
			},
		},
	})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/media/video.mp4", nil)
	srv.ServeHTTP(w, r)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "video data", w.Body.String())
}

// ---------------------------------------------------------------------------
// SetSigningKey
// ---------------------------------------------------------------------------

func TestHTTP_SetSigningKey(t *testing.T) {
	srv, _, _, _ := newTestHTTPServer(t)
	newKey := []byte("new-signing-key-1234567890!!!!!!")
	srv.SetSigningKey(newKey)

	// Build a JWT with the new key and verify it works.
	token := makeJWT(map[string]interface{}{
		"sub": "user",
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	}, newKey)

	result, err := srv.validateJWT(token)
	require.NoError(t, err)
	assert.Equal(t, "user", result["sub"])
}

// ---------------------------------------------------------------------------
// JWT validation
// ---------------------------------------------------------------------------

func TestValidateJWT_Valid(t *testing.T) {
	srv, _, _, _ := newTestHTTPServer(t)

	token := makeJWT(map[string]interface{}{
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

	token := makeJWT(map[string]interface{}{
		"sub": "user-123",
		"exp": float64(time.Now().Add(-time.Hour).Unix()),
	}, testSigningKey)

	_, err := srv.validateJWT(token)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "token expired")
}

func TestValidateJWT_InvalidSignature(t *testing.T) {
	srv, _, _, _ := newTestHTTPServer(t)

	token := makeJWT(map[string]interface{}{
		"sub": "user-123",
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	}, []byte("wrong-key-for-signing-12345678!"))

	_, err := srv.validateJWT(token)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid signature")
}

func TestValidateJWT_MalformedToken(t *testing.T) {
	srv, _, _, _ := newTestHTTPServer(t)

	tests := []struct {
		name  string
		token string
	}{
		{"empty", ""},
		{"one part", "abc"},
		{"two parts", "abc.def"},
		{"malformed", "not-a-jwt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := srv.validateJWT(tt.token)
			require.Error(t, err)
		})
	}
}

func TestValidateJWT_MissingExp(t *testing.T) {
	srv, _, _, _ := newTestHTTPServer(t)

	token := makeJWT(map[string]interface{}{
		"sub": "user-123",
		// no "exp" claim
	}, testSigningKey)

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

// ---------------------------------------------------------------------------
// ServeHTTP: nil denied patterns (s.denied == nil path)
// ---------------------------------------------------------------------------

func TestHTTP_NilDeniedPatterns_SkipsDeniedCheck(t *testing.T) {
	// Build a server with nil denied patterns to exercise the
	// s.denied != nil guard in ServeHTTP.
	sessions := NewSessionStore()
	cache := NewMemoryCache(100, 1024*1024)
	endpoints := NewEndpointStore(cache)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	srv := NewHTTPServer(sessions, endpoints, nil, testSigningKey, "https://example.com", logger)

	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("content"), 0o644)
	require.NoError(t, err)

	err = endpoints.Update([]*agentv1.EndpointConfig{
		{
			Name: "organizations/acme/storageGateways/gw1/endpoints/ep",
			Configuration: &agentv1.EndpointConfig_Filesystem{
				Filesystem: &agentv1.FileSystemEndpointConfig{Path: dir},
			},
		},
	})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/ep/file.txt", nil)
	srv.ServeHTTP(w, r)

	assert.Equal(t, http.StatusOK, w.Code,
		"with nil denied patterns, request should proceed to endpoint")
	assert.Equal(t, "content", w.Body.String())
}

// ---------------------------------------------------------------------------
// ServeHTTP: denied pattern does NOT match (falls through to endpoint)
// ---------------------------------------------------------------------------

func TestHTTP_DeniedPattern_NoMatch_FallsThrough(t *testing.T) {
	srv, _, endpoints, denied := newTestHTTPServer(t)
	denied.Update([]string{"/secret/*"})

	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "public.txt"), []byte("ok"), 0o644)
	require.NoError(t, err)

	err = endpoints.Update([]*agentv1.EndpointConfig{
		{
			Name: "organizations/acme/storageGateways/gw1/endpoints/media",
			Configuration: &agentv1.EndpointConfig_Filesystem{
				Filesystem: &agentv1.FileSystemEndpointConfig{Path: dir},
			},
		},
	})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/media/public.txt", nil)
	srv.ServeHTTP(w, r)

	assert.Equal(t, http.StatusOK, w.Code,
		"non-denied path should be served from endpoint")
	assert.Equal(t, "ok", w.Body.String())
}
