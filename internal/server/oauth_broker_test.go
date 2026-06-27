package server

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dashkan/pivox/internal/config"
)

// testBrokerAppKey is a ≥32-char HMAC key — signOAuthState rejects
// shorter keys (oauthSigningKey enforces the floor).
const testBrokerAppKey = "test-oauth-broker-app-key-0123456789abcd"

func TestOAuthBroker_staticProvider(t *testing.T) {
	t.Parallel()

	bothConfigured := config.OAuthBrokerConfig{
		GitHubClientID:     "gh-client-id",
		GitHubClientSecret: "gh-client-secret",
		GoogleClientID:     "goog-client-id",
		GoogleClientSecret: "goog-client-secret",
	}

	tests := []struct {
		name     string
		cfg      config.OAuthBrokerConfig
		provider string
		wantOK   bool
		check    func(t *testing.T, pc *providerConfig)
	}{
		{
			name:     "google configured",
			cfg:      bothConfigured,
			provider: "google",
			wantOK:   true,
			check: func(t *testing.T, pc *providerConfig) {
				is := assert.New(t)
				is.Equal("google", pc.id)
				is.Equal(kindGoogleIDToken, pc.kind)
				is.Equal("https://accounts.google.com/o/oauth2/v2/auth", pc.authorizeURL)
				is.Equal("https://oauth2.googleapis.com/token", pc.tokenURL)
				is.Equal([]string{"openid", "email", "profile"}, pc.scopes)
				is.Equal("goog-client-id", pc.clientID)
				is.Equal("goog-client-secret", pc.clientSecret)
				// Google gets the account picker, not prompt=login —
				// a "Sign in with Google" click shouldn't force a full
				// password re-entry.
				is.Equal("select_account", pc.prompt)
			},
		},
		{
			name:     "github configured (regression)",
			cfg:      bothConfigured,
			provider: "github",
			wantOK:   true,
			check: func(t *testing.T, pc *providerConfig) {
				is := assert.New(t)
				is.Equal("github", pc.id)
				is.Equal(kindGitHubAccessToken, pc.kind)
				is.Equal("gh-client-id", pc.clientID)
			},
		},
		{
			name:     "google credentials unset is not configured",
			cfg:      config.OAuthBrokerConfig{GitHubClientID: "x", GitHubClientSecret: "y"},
			provider: "google",
			wantOK:   false,
		},
		{
			name:     "github credentials unset is not configured",
			cfg:      config.OAuthBrokerConfig{GoogleClientID: "x", GoogleClientSecret: "y"},
			provider: "github",
			wantOK:   false,
		},
		{
			name:     "unknown provider",
			cfg:      bothConfigured,
			provider: "facebook",
			wantOK:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b := &OAuthBroker{cfg: tt.cfg}
			pc, ok := b.staticProvider(tt.provider)
			assert.Equal(t, tt.wantOK, ok)
			if !tt.wantOK {
				assert.Nil(t, pc)
				return
			}
			require.NotNil(t, pc)
			tt.check(t, pc)
		})
	}
}

func TestOAuthBroker_isAllowedReturnURL(t *testing.T) {
	t.Parallel()
	b := &OAuthBroker{cfg: config.OAuthBrokerConfig{BaseURL: "https://pivox.test"}}

	tests := []struct {
		name      string
		candidate string
		want      bool
	}{
		{"pivox custom scheme", "pivox://auth-complete", true},
		{"pivox scheme carrying a query string", "pivox://auth-complete?es=abc123", true},
		{"same-origin https web callback", "https://pivox.test/auth/broker-callback", true},
		{"loopback 127.0.0.1 with ephemeral port", "http://127.0.0.1:54321/cb", true},
		{"loopback 127.0.0.1 without a port", "http://127.0.0.1/cb", true},
		{"loopback elsewhere in 127.0.0.0/8", "http://127.0.0.2:8080/cb", true},
		{"loopback ipv6 ::1", "http://[::1]:54321/cb", true},
		{"loopback ipv6 ::1 without a port", "http://[::1]/cb", true},
		{"loopback with userinfo is rejected", "http://evil.com@127.0.0.1:5500/cb", false},
		{"octal-encoded loopback is rejected", "http://0177.0.0.1:8080/cb", false},
		{"decimal-integer loopback is rejected", "http://2130706433:8080/cb", false},
		// IP-literal only: the localhost hostname is rejected so the
		// allowlist never depends on name resolution (RFC 8252 §8.3).
		{"localhost hostname is rejected", "http://localhost:54321/cb", false},
		{"https loopback is rejected (loopback is http-only)", "https://127.0.0.1:54321/cb", false},
		{"loopback-lookalike hostname is rejected", "http://127.0.0.1.evil.com/cb", false},
		{"public ip is rejected", "http://8.8.8.8/cb", false},
		{"foreign https host is rejected", "https://evil.com/cb", false},
		{"http on the base host is rejected (scheme mismatch)", "http://pivox.test/cb", false},
		{"unparseable url is rejected", "://nonsense", false},
		{"empty string is rejected", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, b.isAllowedReturnURL(tt.candidate))
		})
	}
}

func TestIsLoopbackIPHost(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		host string
		want bool
	}{
		{"ipv4 loopback bare", "127.0.0.1", true},
		{"ipv4 loopback with port", "127.0.0.1:8080", true},
		{"ipv4 loopback elsewhere in /8", "127.0.0.2", true},
		{"ipv6 loopback with port", "[::1]:8080", true},
		{"ipv6 loopback bare", "::1", true},
		{"ipv6 loopback bracketed no port", "[::1]", true},
		{"octal-encoded ip", "0177.0.0.1", false},
		{"hex-encoded ip", "0x7f.0.0.1", false},
		{"decimal-integer ip", "2130706433", false},
		{"dotted-shorthand ip", "127.1", false},
		{"localhost hostname", "localhost", false},
		{"localhost hostname with port", "localhost:8080", false},
		{"public ipv4 with port", "8.8.8.8:80", false},
		{"loopback-lookalike hostname", "127.0.0.1.evil.com", false},
		{"regular hostname", "example.com", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, isLoopbackIPHost(tt.host))
		})
	}
}

func TestOAuthBroker_start_google(t *testing.T) {
	t.Parallel()

	newBroker := func() *OAuthBroker {
		return &OAuthBroker{
			cfg: config.OAuthBrokerConfig{
				AppKey:             testBrokerAppKey,
				BaseURL:            "https://pivox.test",
				GoogleClientID:     "goog-client-id",
				GoogleClientSecret: "goog-client-secret",
			},
			logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		}
	}

	callStart := func(t *testing.T, provider, rawQuery string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/internal/v1/auth/"+provider+"/start?"+rawQuery, nil)
		req.SetPathValue("provider", provider)
		rec := httptest.NewRecorder()
		newBroker().start(rec, req)
		return rec
	}

	t.Run("redirects to the google authorize endpoint with no nonce", func(t *testing.T) {
		t.Parallel()
		is := assert.New(t)
		must := require.New(t)

		rec := callStart(t, "google", "return="+url.QueryEscape("pivox://auth-complete"))
		must.Equal(http.StatusFound, rec.Code)

		loc, err := url.Parse(rec.Header().Get("Location"))
		must.NoError(err)
		is.Equal("https", loc.Scheme)
		is.Equal("accounts.google.com", loc.Host)
		is.Equal("/o/oauth2/v2/auth", loc.Path)

		q := loc.Query()
		is.Equal("goog-client-id", q.Get("client_id"))
		is.Equal("https://pivox.test/internal/v1/auth/google/callback", q.Get("redirect_uri"))
		is.Equal("code", q.Get("response_type"))
		is.Equal("openid email profile", q.Get("scope"))
		is.Equal("select_account", q.Get("prompt"))
		is.NotEmpty(q.Get("state"))
		// Google must not be sent a nonce — GoogleAuthProvider.credential
		// takes no rawNonce, so a nonce claim would be unverifiable.
		is.Empty(q.Get("nonce"))
	})

	t.Run("accepts a loopback return URL", func(t *testing.T) {
		t.Parallel()
		rec := callStart(t, "google", "return="+url.QueryEscape("http://127.0.0.1:54321/cb"))
		assert.Equal(t, http.StatusFound, rec.Code)
	})

	t.Run("rejects a localhost-hostname return URL", func(t *testing.T) {
		t.Parallel()
		rec := callStart(t, "google", "return="+url.QueryEscape("http://localhost:54321/cb"))
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("forwards login_hint to the IdP", func(t *testing.T) {
		t.Parallel()
		rec := callStart(t, "google",
			"return="+url.QueryEscape("pivox://auth-complete")+"&login_hint="+url.QueryEscape("user@example.com"))
		require.Equal(t, http.StatusFound, rec.Code)
		loc, err := url.Parse(rec.Header().Get("Location"))
		require.NoError(t, err)
		assert.Equal(t, "user@example.com", loc.Query().Get("login_hint"))
	})

	t.Run("unknown provider returns 404", func(t *testing.T) {
		t.Parallel()
		rec := callStart(t, "facebook", "return="+url.QueryEscape("pivox://auth-complete"))
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}

func TestApplyAuthorizeParams(t *testing.T) {
	t.Parallel()

	const (
		callbackURL = "https://pivox.test/internal/v1/auth/oidc.acme/callback"
		state       = "signed-state"
		rawNonce    = "raw-nonce-payload"
		loginHint   = "user@acme.com"
	)

	t.Run("oidc sso forces login and a hashed nonce", func(t *testing.T) {
		t.Parallel()
		is := assert.New(t)
		cfg := &providerConfig{
			id:       "oidc.acme",
			clientID: "pivox",
			scopes:   []string{"openid", "email", "profile"},
			kind:     kindOIDCIDToken,
		}
		q := url.Values{}
		applyAuthorizeParams(q, cfg, callbackURL, state, rawNonce, loginHint)

		// prompt=login: an explicit "Sign in" must require credentials, not
		// silently reuse an existing IdP session (which prompt=select_account
		// would do when only one session exists).
		is.Equal("login", q.Get("prompt"))
		sum := sha256.Sum256([]byte(rawNonce))
		is.Equal(hex.EncodeToString(sum[:]), q.Get("nonce"))
		is.Equal("pivox", q.Get("client_id"))
		is.Equal(callbackURL, q.Get("redirect_uri"))
		is.Equal("code", q.Get("response_type"))
		is.Equal("openid email profile", q.Get("scope"))
		is.Equal(state, q.Get("state"))
		is.Equal(loginHint, q.Get("login_hint"))
	})

	t.Run("static provider keeps its declared prompt and sends no nonce", func(t *testing.T) {
		t.Parallel()
		is := assert.New(t)
		cfg := &providerConfig{
			id:       "google",
			clientID: "goog",
			scopes:   []string{"openid", "email", "profile"},
			prompt:   "select_account",
			kind:     kindGoogleIDToken,
		}
		q := url.Values{}
		applyAuthorizeParams(q, cfg, callbackURL, state, rawNonce, "")

		is.Equal("select_account", q.Get("prompt"))
		// GoogleAuthProvider.credential takes no rawNonce — a nonce claim
		// would be unverifiable, so none is sent.
		is.Empty(q.Get("nonce"))
		// No login_hint supplied -> omitted, not set to empty.
		is.False(q.Has("login_hint"))
	})

	t.Run("access-token provider sets no prompt and no nonce", func(t *testing.T) {
		t.Parallel()
		is := assert.New(t)
		cfg := &providerConfig{
			id:                   "github",
			clientID:             "gh",
			scopes:               []string{"read:user", "user:email"},
			extraAuthorizeParams: map[string]string{"allow_signup": "true"},
			kind:                 kindGitHubAccessToken,
		}
		q := url.Values{}
		applyAuthorizeParams(q, cfg, callbackURL, state, rawNonce, "")

		is.False(q.Has("prompt"))
		is.Empty(q.Get("nonce"))
		is.Equal("true", q.Get("allow_signup"))
	})

	t.Run("broker params override provider-supplied extras", func(t *testing.T) {
		t.Parallel()
		is := assert.New(t)
		cfg := &providerConfig{
			id:       "evil",
			clientID: "real-client",
			scopes:   []string{"openid"},
			// A misconfigured provider must not clobber broker-decided params.
			extraAuthorizeParams: map[string]string{
				"client_id":    "attacker",
				"redirect_uri": "https://evil.test/cb",
				"state":        "forged",
			},
			kind: kindGitHubAccessToken,
		}
		q := url.Values{}
		applyAuthorizeParams(q, cfg, callbackURL, state, rawNonce, "")

		is.Equal("real-client", q.Get("client_id"))
		is.Equal(callbackURL, q.Get("redirect_uri"))
		is.Equal(state, q.Get("state"))
	})
}
