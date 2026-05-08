package storageagent

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agentv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/agent/v1"
)

// setupEndpoint registers a filesystem endpoint backed by t.TempDir
// containing a single file.txt of "content". Returns the temp dir
// path so callers can write more fixtures into it.
func setupEndpoint(t *testing.T, endpoints *EndpointStore, name string) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "file.txt"), []byte("content"), 0o644))

	err := endpoints.Update([]*agentv1.EndpointConfig{
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

// validSessionRequest constructs a GET request carrying a session
// cookie with a JWT signed by testSigningKey whose token claim is
// the value tests already wired into the SessionStore.
func validSessionRequest(method, path, sessionToken string) *http.Request {
	r := httptest.NewRequest(method, path, nil)
	jwt := makeJWT(map[string]any{
		"token": sessionToken,
		"exp":   float64(time.Now().Add(time.Hour).Unix()),
	}, testSigningKey)
	r.AddCookie(&http.Cookie{Name: "pivox_session", Value: jwt})
	return r
}

// ---------------------------------------------------------------------------
// Reject paths — every branch that must produce 401.
// ---------------------------------------------------------------------------

func TestHTTPAuth_RejectPaths(t *testing.T) {
	t.Parallel()

	wrongKey := []byte("wrong-key-will-not-match-server!")
	cases := []struct {
		name   string
		cookie *http.Cookie
		want   int
	}{
		{
			name:   "missing cookie",
			cookie: nil,
			want:   http.StatusUnauthorized,
		},
		{
			name:   "invalid jwt format",
			cookie: &http.Cookie{Name: "pivox_session", Value: "not-a-jwt"},
			want:   http.StatusUnauthorized,
		},
		{
			name: "expired jwt",
			cookie: &http.Cookie{Name: "pivox_session", Value: makeJWT(map[string]any{
				"token": "session-abc",
				"exp":   float64(time.Now().Add(-time.Hour).Unix()),
			}, testSigningKey)},
			want: http.StatusUnauthorized,
		},
		{
			name: "wrong signing key",
			cookie: &http.Cookie{Name: "pivox_session", Value: makeJWT(map[string]any{
				"token": "session-abc",
				"exp":   float64(time.Now().Add(time.Hour).Unix()),
			}, wrongKey)},
			want: http.StatusUnauthorized,
		},
		{
			name: "missing token claim",
			cookie: &http.Cookie{Name: "pivox_session", Value: makeJWT(map[string]any{
				"sub": "user-123",
				"exp": float64(time.Now().Add(time.Hour).Unix()),
			}, testSigningKey)},
			want: http.StatusUnauthorized,
		},
		{
			name: "empty token claim",
			cookie: &http.Cookie{Name: "pivox_session", Value: makeJWT(map[string]any{
				"token": "",
				"exp":   float64(time.Now().Add(time.Hour).Unix()),
			}, testSigningKey)},
			want: http.StatusUnauthorized,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, _, _, _ := newTestHTTPServer(t)

			r := httptest.NewRequest(http.MethodGet, "/media/file.txt", nil)
			if tc.cookie != nil {
				r.AddCookie(tc.cookie)
			}
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, r)

			assert.Equal(t, tc.want, w.Code)
		})
	}
}

// ---------------------------------------------------------------------------
// Authorized session — the request reaches the endpoint layer.
// ---------------------------------------------------------------------------

func TestHTTPAuth_ValidJWT_SessionNotFound_Forbidden(t *testing.T) {
	srv, _, _, _ := newTestHTTPServer(t)

	// Valid JWT with a token claim whose session was never granted.
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, validSessionRequest(http.MethodGet, "/media/file.txt", "no-such-session"))

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestHTTPAuth_ValidJWT_AuthorizedSession_Success(t *testing.T) {
	srv, sessions, endpoints, _ := newTestHTTPServer(t)
	setupEndpoint(t, endpoints, "media")
	require.NoError(t, sessions.Grant(context.Background(), "session-xyz", []string{"/media/*"}, time.Now().Add(time.Hour)))

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, validSessionRequest(http.MethodGet, "/media/file.txt", "session-xyz"))

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "content", w.Body.String())
}

func TestHTTPAuth_ValidJWT_NoEndpoint_NotFound(t *testing.T) {
	srv, sessions, _, _ := newTestHTTPServer(t)
	require.NoError(t, sessions.Grant(context.Background(), "session-xyz", []string{"/missing/*"}, time.Now().Add(time.Hour)))

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, validSessionRequest(http.MethodGet, "/missing/file.txt", "session-xyz"))

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHTTPAuth_ValidJWT_DeniedPattern_NotFound(t *testing.T) {
	srv, sessions, endpoints, denied := newTestHTTPServer(t)
	setupEndpoint(t, endpoints, "media")
	require.NoError(t, denied.Update(context.Background(), []string{"/media/file.txt"}))
	require.NoError(t, sessions.Grant(context.Background(), "session-xyz", []string{"/media/*"}, time.Now().Add(time.Hour)))

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, validSessionRequest(http.MethodGet, "/media/file.txt", "session-xyz"))

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHTTPAuth_ValidJWT_DeniedNoMatch_FallsThrough(t *testing.T) {
	srv, sessions, endpoints, denied := newTestHTTPServer(t)
	setupEndpoint(t, endpoints, "media")
	require.NoError(t, denied.Update(context.Background(), []string{"/secret/*"})) // doesn't match /media/*
	require.NoError(t, sessions.Grant(context.Background(), "session-xyz", []string{"/media/*"}, time.Now().Add(time.Hour)))

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, validSessionRequest(http.MethodGet, "/media/file.txt", "session-xyz"))

	assert.Equal(t, http.StatusOK, w.Code, "non-denied path should be served from endpoint")
	assert.Equal(t, "content", w.Body.String())
}

// TestHTTPAuth_ValidJWT_NilDeniedPatterns exercises the s.denied != nil
// guard in ServeHTTP — when no denied list is wired, requests skip the
// denied check entirely. Reproduces a server constructed without a
// DeniedPatterns instance.
func TestHTTPAuth_ValidJWT_NilDeniedPatterns(t *testing.T) {
	sessions := NewSessionStore(SessionStoreConfig{})
	cache := NewMemoryCache(100, 1024*1024)
	endpoints := NewEndpointStore(cache)
	logger := newSilentLogger()
	srv := NewHTTPServer(Config{
		Sessions:   sessions,
		Endpoints:  endpoints,
		SigningKey: testSigningKey,
		CORSOrigin: "https://example.com",
		Logger:     logger,
	})

	setupEndpoint(t, endpoints, "ep")
	require.NoError(t, sessions.Grant(context.Background(), "session-xyz", []string{"/ep/*"}, time.Now().Add(time.Hour)))

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, validSessionRequest(http.MethodGet, "/ep/file.txt", "session-xyz"))

	assert.Equal(t, http.StatusOK, w.Code, "nil denied patterns should let request reach endpoint")
	assert.Equal(t, "content", w.Body.String())
}

// newSilentLogger returns a logger that discards everything. Used by
// the nil-denied test which constructs a server with non-default
// fixtures and doesn't want the stderr noise.
func newSilentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestHTTPAuth_CORSPreflight_SkipsAuth(t *testing.T) {
	srv, _, _, _ := newTestHTTPServer(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodOptions, "/media/file.txt", nil)
	srv.ServeHTTP(w, r)

	assert.Equal(t, http.StatusNoContent, w.Code, "OPTIONS must not require auth")
}
