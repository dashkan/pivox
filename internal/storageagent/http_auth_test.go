package storageagent

import (
	"bytes"
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

	err := endpoints.Update(t.Context(), []*agentv1.EndpointConfig{
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
	endpoints := NewEndpointStore(EndpointStoreConfig{Cache: cache})
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

// ---------------------------------------------------------------------------
// #27 phase 6 — Authorization: Bearer <jwt> as an alternative to the
// pivox_session cookie. Native clients (macOS, Windows) attach the
// JWT they read from CreateStorageSessionResponse.token as a Bearer
// token rather than parsing gRPC Set-Cookie metadata.
// ---------------------------------------------------------------------------

// validBearerRequest returns a GET request that authenticates via
// Authorization: Bearer <jwt>. Mirrors validSessionRequest but
// targets the bearer parser instead of the cookie parser.
func validBearerRequest(method, path, sessionToken string) *http.Request {
	r := httptest.NewRequest(method, path, nil)
	jwt := makeJWT(map[string]any{
		"token": sessionToken,
		"sub":   "user-uuid-xyz",
		"org":   "acme",
		"exp":   float64(time.Now().Add(time.Hour).Unix()),
	}, testSigningKey)
	r.Header.Set("Authorization", "Bearer "+jwt)
	return r
}

func TestHTTPAuth_BearerSuccess(t *testing.T) {
	t.Parallel()
	srv, sessions, endpoints, _ := newTestHTTPServer(t)
	setupEndpoint(t, endpoints, "media")
	require.NoError(t, sessions.Grant(context.Background(), "bearer-session", []string{"/media/*"}, time.Now().Add(time.Hour)))

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, validBearerRequest(http.MethodGet, "/media/file.txt", "bearer-session"))

	assert.Equal(t, http.StatusOK, w.Code, "valid Bearer token must authorize like a cookie")
	assert.Equal(t, "content", w.Body.String())
}

// TestHTTPAuth_BearerRejectPaths is the strict-parsing acceptance.
// Per #27 phase 6's plan, the Bearer prefix is case-sensitive and
// rejects leading whitespace — laxness here is where bypasses come
// from. Each row exercises one rejection path; the expected status is
// 401 (Unauthenticated, "refresh and retry") rather than 403
// (Forbidden, "you can't do this").
func TestHTTPAuth_BearerRejectPaths(t *testing.T) {
	t.Parallel()

	validJWT := makeJWT(map[string]any{
		"token": "session-abc",
		"exp":   float64(time.Now().Add(time.Hour).Unix()),
	}, testSigningKey)
	expiredJWT := makeJWT(map[string]any{
		"token": "session-abc",
		"exp":   float64(time.Now().Add(-time.Hour).Unix()),
	}, testSigningKey)
	tamperedJWT := validJWT + "x" // signature mismatch

	cases := []struct {
		name   string
		header string // "" means don't set Authorization
		want   int
	}{
		{
			name:   "missing authorization header",
			header: "",
			want:   http.StatusUnauthorized,
		},
		{
			name:   "lowercase bearer prefix",
			header: "bearer " + validJWT,
			want:   http.StatusUnauthorized,
		},
		{
			name:   "leading whitespace before Bearer",
			header: " Bearer " + validJWT,
			want:   http.StatusUnauthorized,
		},
		{
			name:   "Bearer with no token",
			header: "Bearer ",
			want:   http.StatusUnauthorized,
		},
		{
			name:   "Bearer prefix without space",
			header: "Bearer" + validJWT,
			want:   http.StatusUnauthorized,
		},
		{
			name:   "Basic auth instead of Bearer",
			header: "Basic " + validJWT,
			want:   http.StatusUnauthorized,
		},
		{
			name:   "expired jwt",
			header: "Bearer " + expiredJWT,
			want:   http.StatusUnauthorized,
		},
		{
			name:   "tampered signature",
			header: "Bearer " + tamperedJWT,
			want:   http.StatusUnauthorized,
		},
		{
			name:   "malformed jwt body",
			header: "Bearer not-a-jwt",
			want:   http.StatusUnauthorized,
		},
		// These two pass the strict-prefix gate but produce a
		// JWT with leading whitespace which validateJWT rejects
		// downstream. Pinning here insures against a future
		// "helpful" parser cleanup (e.g. strings.TrimSpace on the
		// token body) that would silently accept malformed tokens.
		{
			name:   "Bearer with tab separator",
			header: "Bearer\t" + validJWT,
			want:   http.StatusUnauthorized,
		},
		{
			name:   "Bearer with double space",
			header: "Bearer  " + validJWT,
			want:   http.StatusUnauthorized,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, _, _, _ := newTestHTTPServer(t)

			r := httptest.NewRequest(http.MethodGet, "/media/file.txt", nil)
			if tc.header != "" {
				r.Header.Set("Authorization", tc.header)
			}
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, r)

			assert.Equal(t, tc.want, w.Code,
				"strict Bearer parsing: %s must be rejected with 401", tc.name)
		})
	}
}

// TestHTTPAuth_BearerTakesPrecedenceOverCookie pins the precedence
// contract: when both Authorization: Bearer and the pivox_session
// cookie are present, the Bearer header wins. This matches the
// HTTP-spec convention that explicit Authorization headers override
// implicit cookie-based credentials.
func TestHTTPAuth_BearerTakesPrecedenceOverCookie(t *testing.T) {
	t.Parallel()
	srv, sessions, endpoints, _ := newTestHTTPServer(t)
	setupEndpoint(t, endpoints, "media")
	require.NoError(t, sessions.Grant(context.Background(), "bearer-only", []string{"/media/*"}, time.Now().Add(time.Hour)))

	bearerJWT := makeJWT(map[string]any{
		"token": "bearer-only",
		"exp":   float64(time.Now().Add(time.Hour).Unix()),
	}, testSigningKey)
	// Cookie carries a DIFFERENT (and unauthorized) session; Bearer
	// must win and the request must succeed via the bearer-only
	// session's grants.
	cookieJWT := makeJWT(map[string]any{
		"token": "cookie-only-not-granted",
		"exp":   float64(time.Now().Add(time.Hour).Unix()),
	}, testSigningKey)

	r := httptest.NewRequest(http.MethodGet, "/media/file.txt", nil)
	r.Header.Set("Authorization", "Bearer "+bearerJWT)
	r.AddCookie(&http.Cookie{Name: "pivox_session", Value: cookieJWT})

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	assert.Equal(t, http.StatusOK, w.Code,
		"Bearer header must take precedence over cookie when both present")
}

// TestHTTPAuth_BearerLogsSubAndOrgOnSuccess captures the agent's
// log output and verifies the new claims appear as request-scoped
// attribution fields when a request is authorized via Bearer.
// Bridges phase-5's controller-side claim emission to phase-6's
// agent-side audit consumption.
func TestHTTPAuth_BearerLogsSubAndOrgOnSuccess(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	sessions := NewSessionStore(SessionStoreConfig{})
	cache := NewMemoryCache(100, 1024*1024)
	endpoints := NewEndpointStore(EndpointStoreConfig{Cache: cache})
	denied := NewDeniedPatterns(DeniedPatternsConfig{})

	// Capture-buffer logger so the test can assert on emitted lines.
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	srv := NewHTTPServer(Config{
		Sessions:   sessions,
		Endpoints:  endpoints,
		Denied:     denied,
		SigningKey: testSigningKey,
		CORSOrigin: "https://example.com",
		Logger:     logger,
	})
	setupEndpoint(t, endpoints, "media")
	require.NoError(t, sessions.Grant(ctx, "audit-session", []string{"/media/*"}, time.Now().Add(time.Hour)))

	jwt := makeJWT(map[string]any{
		"token": "audit-session",
		"sub":   "identity-uuid-1234",
		"org":   "acme",
		"exp":   float64(time.Now().Add(time.Hour).Unix()),
	}, testSigningKey)
	r := httptest.NewRequest(http.MethodGet, "/media/file.txt", nil)
	r.Header.Set("Authorization", "Bearer "+jwt)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	require.Equal(t, http.StatusOK, w.Code)

	out := buf.String()
	assert.Contains(t, out, `sub=identity-uuid-1234`,
		"sub claim from the JWT must appear in the agent's per-request log "+
			"so audit downstream can attribute requests without a directory lookup")
	assert.Contains(t, out, `org=acme`,
		"org claim from the JWT must appear in the agent's per-request log")
}

// TestHTTPAuth_BearerMissingAuditClaimsLogsWarn verifies that a
// validly signed JWT WITHOUT sub/org claims still authorizes
// (verify-then-trust — audit metadata isn't a re-auth gate) but
// the agent emits a Warn so silent claim-shape drift becomes
// observable rather than producing empty `sub= org=` logs.
func TestHTTPAuth_BearerMissingAuditClaimsLogsWarn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	sessions := NewSessionStore(SessionStoreConfig{})
	cache := NewMemoryCache(100, 1024*1024)
	endpoints := NewEndpointStore(EndpointStoreConfig{Cache: cache})
	denied := NewDeniedPatterns(DeniedPatternsConfig{})

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	srv := NewHTTPServer(Config{
		Sessions:   sessions,
		Endpoints:  endpoints,
		Denied:     denied,
		SigningKey: testSigningKey,
		CORSOrigin: "https://example.com",
		Logger:     logger,
	})
	setupEndpoint(t, endpoints, "media")
	require.NoError(t, sessions.Grant(ctx, "no-claims-session", []string{"/media/*"}, time.Now().Add(time.Hour)))

	// Mint a JWT WITHOUT sub or org claims — only token + exp.
	jwt := makeJWT(map[string]any{
		"token": "no-claims-session",
		"exp":   float64(time.Now().Add(time.Hour).Unix()),
	}, testSigningKey)
	r := httptest.NewRequest(http.MethodGet, "/media/file.txt", nil)
	r.Header.Set("Authorization", "Bearer "+jwt)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	require.Equal(t, http.StatusOK, w.Code,
		"missing audit claims must NOT block an otherwise-valid request "+
			"(verify-then-trust posture; audit metadata is not a re-auth gate)")

	out := buf.String()
	assert.Contains(t, out, `level=WARN`,
		"missing audit claims must surface as a Warn so silent controller-side "+
			"claim-shape drift is observable in operator dashboards")
	assert.Contains(t, out, "missing audit claims",
		"warn message must name the failure mode")
	assert.Contains(t, out, "sub_present=false",
		"warn must structurally distinguish which claim was missing")
}

func TestHTTPAuth_CORSPreflight_SkipsAuth(t *testing.T) {
	srv, _, _, _ := newTestHTTPServer(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodOptions, "/media/file.txt", nil)
	srv.ServeHTTP(w, r)

	assert.Equal(t, http.StatusNoContent, w.Code, "OPTIONS must not require auth")
}
