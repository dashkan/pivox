//go:build !dev

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

var testAuthSigningKey = []byte("test-secret-key-for-jwt-signing!")

// makeAuthJWT creates a valid HS256 JWT for testing.
func makeAuthJWT(claims map[string]interface{}, key []byte) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payloadJSON, _ := json.Marshal(claims)
	payload := base64.RawURLEncoding.EncodeToString(payloadJSON)

	headerPayload := header + "." + payload
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(headerPayload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return headerPayload + "." + sig
}

func newAuthTestHTTPServer(t *testing.T) (*HTTPServer, *SessionStore, *EndpointStore, *DeniedPatterns) {
	t.Helper()
	sessions := NewSessionStore()
	cache := NewMemoryCache(100, 1024*1024)
	endpoints := NewEndpointStore(cache)
	denied := NewDeniedPatterns()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	srv := NewHTTPServer(sessions, endpoints, denied, testAuthSigningKey, "https://example.com", logger)
	return srv, sessions, endpoints, denied
}

func setupEndpoint(t *testing.T, endpoints *EndpointStore, name string) string {
	t.Helper()
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("content"), 0o644)
	require.NoError(t, err)

	err = endpoints.Update([]*agentv1.EndpointConfig{
		{
			Name: fmt.Sprintf("organizations/acme/storageGateways/gw1/endpoints/%s", name),
			Configuration: &agentv1.EndpointConfig_Filesystem{
				Filesystem: &agentv1.FileSystemEndpointConfig{Path: dir},
			},
		},
	})
	require.NoError(t, err)
	return dir
}

// ---------------------------------------------------------------------------
// ServeHTTP — production auth path (devSkipAuth = false)
// ---------------------------------------------------------------------------

func TestHTTPAuth_MissingCookie_Unauthorized(t *testing.T) {
	srv, _, _, _ := newAuthTestHTTPServer(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/media/file.txt", nil)
	srv.ServeHTTP(w, r)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHTTPAuth_InvalidJWT_Unauthorized(t *testing.T) {
	srv, _, _, _ := newAuthTestHTTPServer(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/media/file.txt", nil)
	r.AddCookie(&http.Cookie{Name: "pivox_session", Value: "not-a-jwt"})
	srv.ServeHTTP(w, r)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHTTPAuth_ExpiredJWT_Unauthorized(t *testing.T) {
	srv, _, _, _ := newAuthTestHTTPServer(t)

	token := makeAuthJWT(map[string]interface{}{
		"token": "session-abc",
		"exp":   float64(time.Now().Add(-time.Hour).Unix()),
	}, testAuthSigningKey)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/media/file.txt", nil)
	r.AddCookie(&http.Cookie{Name: "pivox_session", Value: token})
	srv.ServeHTTP(w, r)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHTTPAuth_WrongSigningKey_Unauthorized(t *testing.T) {
	srv, _, _, _ := newAuthTestHTTPServer(t)

	token := makeAuthJWT(map[string]interface{}{
		"token": "session-abc",
		"exp":   float64(time.Now().Add(time.Hour).Unix()),
	}, []byte("wrong-key-will-not-match-server!"))

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/media/file.txt", nil)
	r.AddCookie(&http.Cookie{Name: "pivox_session", Value: token})
	srv.ServeHTTP(w, r)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHTTPAuth_MissingTokenClaim_Unauthorized(t *testing.T) {
	srv, _, _, _ := newAuthTestHTTPServer(t)

	// JWT is valid but has no "token" claim.
	token := makeAuthJWT(map[string]interface{}{
		"sub": "user-123",
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	}, testAuthSigningKey)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/media/file.txt", nil)
	r.AddCookie(&http.Cookie{Name: "pivox_session", Value: token})
	srv.ServeHTTP(w, r)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHTTPAuth_EmptyTokenClaim_Unauthorized(t *testing.T) {
	srv, _, _, _ := newAuthTestHTTPServer(t)

	token := makeAuthJWT(map[string]interface{}{
		"token": "",
		"exp":   float64(time.Now().Add(time.Hour).Unix()),
	}, testAuthSigningKey)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/media/file.txt", nil)
	r.AddCookie(&http.Cookie{Name: "pivox_session", Value: token})
	srv.ServeHTTP(w, r)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHTTPAuth_ValidJWT_SessionNotFound_Forbidden(t *testing.T) {
	srv, _, _, _ := newAuthTestHTTPServer(t)

	// Valid JWT with token claim, but no matching session.
	token := makeAuthJWT(map[string]interface{}{
		"token": "no-such-session",
		"exp":   float64(time.Now().Add(time.Hour).Unix()),
	}, testAuthSigningKey)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/media/file.txt", nil)
	r.AddCookie(&http.Cookie{Name: "pivox_session", Value: token})
	srv.ServeHTTP(w, r)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestHTTPAuth_ValidJWT_AuthorizedSession_Success(t *testing.T) {
	srv, sessions, endpoints, _ := newAuthTestHTTPServer(t)
	setupEndpoint(t, endpoints, "media")

	// Grant a session.
	sessions.Grant("session-xyz", []string{"/media/*"}, time.Now().Add(time.Hour))

	token := makeAuthJWT(map[string]interface{}{
		"token": "session-xyz",
		"exp":   float64(time.Now().Add(time.Hour).Unix()),
	}, testAuthSigningKey)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/media/file.txt", nil)
	r.AddCookie(&http.Cookie{Name: "pivox_session", Value: token})
	srv.ServeHTTP(w, r)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "content", w.Body.String())
}

func TestHTTPAuth_ValidJWT_DeniedPattern_NotFound(t *testing.T) {
	srv, sessions, endpoints, denied := newAuthTestHTTPServer(t)
	setupEndpoint(t, endpoints, "media")
	denied.Update([]string{"/media/file.txt"})

	sessions.Grant("session-xyz", []string{"/media/*"}, time.Now().Add(time.Hour))

	token := makeAuthJWT(map[string]interface{}{
		"token": "session-xyz",
		"exp":   float64(time.Now().Add(time.Hour).Unix()),
	}, testAuthSigningKey)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/media/file.txt", nil)
	r.AddCookie(&http.Cookie{Name: "pivox_session", Value: token})
	srv.ServeHTTP(w, r)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHTTPAuth_CORSPreflight_SkipsAuth(t *testing.T) {
	srv, _, _, _ := newAuthTestHTTPServer(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodOptions, "/media/file.txt", nil)
	srv.ServeHTTP(w, r)

	// OPTIONS should return 204 without requiring auth.
	assert.Equal(t, http.StatusNoContent, w.Code)
}
