package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dashkan/pivox/internal/config"
	"github.com/dashkan/pivox/internal/crypto"
	db "github.com/dashkan/pivox/internal/db/generated"
)

// OAuthBroker drives federated sign-in (GitHub and Google as static
// providers; OIDC SSO via per-org SsoConfig rows) for native and web
// clients. Pairs with
// oauth_broker_state.go for the HMAC-signed `state` token that
// round-trips through the IdP redirect.
//
// Mounted at /api/oauth/{provider}/{start,callback}. The provider
// segment can be a static name like "github" or a dynamic OIDC
// provider id like "oidc.acme" that resolves against `sso_configs`.
//
// History: previously lived in TanStack Start
// (web/apps/start/src/server/oauth/*). Migrated server-side to
// co-locate auth machinery — the broker needs DB access to read
// SsoConfig and KMS access to decrypt client secrets, both of which
// are already wired in Go but would require duplicating the
// envelope/encryption stack to host in TS.
type OAuthBroker struct {
	queries   db.Querier
	logger    *slog.Logger
	encryptor crypto.Encryptor
	cfg       config.OAuthBrokerConfig

	// httpClient is reused across token-exchange and OIDC discovery
	// fetches. Has a tight 10s timeout — IdPs are remote and slow
	// upstreams should fail fast rather than tying up our handler.
	httpClient *http.Client

	// Per-issuer discovery doc cache. The OIDC `.well-known/`
	// document is conventionally cacheable for hours and the
	// broker would otherwise refetch it on every `start` AND
	// `callback` (twice per sign-in). 5-minute TTL keeps the
	// blast radius small if an IdP rotates endpoints.
	discoveryMu    sync.Mutex
	discoveryCache map[string]discoveryCacheEntry
}

type discoveryCacheEntry struct {
	doc       *oidcDiscovery
	expiresAt time.Time
}

const oidcDiscoveryCacheTTL = 5 * time.Minute

// OAuthBrokerConfig is the constructor input for OAuthBroker. Named
// with the `OAuthBroker` prefix to disambiguate from the unrelated
// settings struct `config.OAuthBrokerConfig` (HMAC app key, base
// URL, GitHub creds) which is held inside this Config as `Broker`.
type OAuthBrokerConfig struct {
	// Queries is the sqlc query interface. Required.
	Queries db.Querier
	// Encryptor decrypts per-org SSO client secrets stored in the
	// `sso_configs` table. Required.
	Encryptor crypto.Encryptor
	// Broker carries the HMAC app key, the public base URL used to
	// build redirect_uri, and static GitHub credentials. Required —
	// the broker can't sign / verify state tokens or build callback
	// URLs without it.
	Broker config.OAuthBrokerConfig
	// Logger is the slog logger used for broker-side audit lines.
	// Required.
	Logger *slog.Logger
}

// NewOAuthBroker constructs an OAuthBroker from cfg. Panics on a
// missing required field — startup-time programmer error, fail loud
// on boot.
//
// Dynamic OIDC providers are resolved per-request from the DB; the
// settings struct (cfg.Broker) only supplies the static GitHub
// credentials and the broker's own URL configuration.
func NewOAuthBroker(cfg OAuthBrokerConfig) *OAuthBroker {
	if cfg.Queries == nil {
		panic("server: OAuthBrokerConfig.Queries is required")
	}
	if cfg.Encryptor == nil {
		panic("server: OAuthBrokerConfig.Encryptor is required")
	}
	if cfg.Logger == nil {
		panic("server: OAuthBrokerConfig.Logger is required")
	}
	// Broker is a struct (not a pointer/interface) so a forgotten
	// caller-side init is invisible to a `nil` panic. Validate the
	// load-bearing fields directly. AppKey is the most consequential —
	// an empty AppKey makes signOAuthState/verifyOAuthState HMAC over
	// a zero-byte key, which would make broker `state` tokens
	// trivially forgeable. Fail loud on boot rather than ship a
	// silently-misconfigured broker.
	if cfg.Broker.AppKey == "" {
		panic("server: OAuthBrokerConfig.Broker.AppKey is required (forgeable state tokens otherwise)")
	}
	if cfg.Broker.BaseURL == "" {
		panic("server: OAuthBrokerConfig.Broker.BaseURL is required")
	}
	return &OAuthBroker{
		queries:        cfg.Queries,
		logger:         cfg.Logger,
		encryptor:      cfg.Encryptor,
		cfg:            cfg.Broker,
		httpClient:     &http.Client{Timeout: 10 * time.Second},
		discoveryCache: make(map[string]discoveryCacheEntry),
	}
}

// Register mounts the broker handlers on the provided mux. Routes:
//
//	GET /internal/v1/auth/{provider}/start?return=<url>
//	GET /internal/v1/auth/{provider}/callback?code=…&state=…
//
// Same `/internal/v1/auth/` prefix as the JSON RPC siblings in
// internal_hooks.go (`auth:syncIdentity`, `auth:exchangeToken`,
// `auth:resolveProvider`, etc.) — nginx already routes the prefix to
// the Go REST listener so no proxy change is needed when the broker
// came over from TanStack. Despite the `internal/` name the routes
// are public-facing for browser redirects; the prefix tracks
// "Pivox-server-implemented" vs the gRPC-gateway-generated `/v1/...`
// routes.
// Register mounts the broker handlers. No app-level rate limiting:
// pivox-cloud runs behind an edge proxy / load balancer that owns
// volumetric per-IP defense. Auth-flow abuse defenses live in the
// HMAC-signed state token, single-use codes, and the IdP's own
// brute-force protections on `/authorize`.
func (b *OAuthBroker) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /internal/v1/auth/{provider}/start", b.start)
	mux.HandleFunc("GET /internal/v1/auth/{provider}/callback", b.callback)
}

// providerConfig describes everything the broker needs to drive an
// authorize request and a code-for-token exchange against an IdP.
// Static providers (GitHub) are hard-coded; OIDC providers are
// constructed dynamically from `sso_configs` + the issuer's
// discovery document.
type providerConfig struct {
	id           string
	authorizeURL string
	tokenURL     string
	scopes       []string
	clientID     string
	clientSecret string
	// extraAuthorizeParams are appended verbatim to the authorize URL.
	extraAuthorizeParams map[string]string
	// tokenRequestHeaders are added to the code-exchange POST.
	// GitHub specifically defaults to form-encoded responses unless
	// asked for JSON via Accept: application/json.
	tokenRequestHeaders map[string]string
	// prompt, when non-empty, is sent as the OAuth `prompt` authorize
	// parameter. Static providers set it explicitly (Google →
	// "select_account"); OIDC SSO providers leave it empty and have
	// `prompt=login` forced separately in start().
	prompt string
	// kind tells the callback how to interpret the token response and
	// the client which Firebase credential to build:
	//   "github_access_token" → GithubAuthProvider.credential
	//   "google_id_token"     → GoogleAuthProvider.credential (no nonce)
	//   "oidc_id_token"       → OAuthProvider(id).credential + rawNonce
	kind credentialKind
}

type credentialKind string

const (
	kindGitHubAccessToken credentialKind = "github_access_token"
	kindGoogleIDToken     credentialKind = "google_id_token"
	kindOIDCIDToken       credentialKind = "oidc_id_token"
)

// resolveProviderConfig is the dispatcher. Routes never branch on
// provider name — they always go through here. Static providers are
// looked up by literal name; dynamic OIDC providers are recognized
// by the `oidc.` prefix and resolved via the SsoConfig table.
//
// Returns (nil, nil) for unknown providers so the caller can return
// 404 without disclosing whether the provider is unconfigured vs.
// disabled vs. nonexistent.
func (b *OAuthBroker) resolveProviderConfig(ctx context.Context, providerID string) (*providerConfig, error) {
	if cfg, ok := b.staticProvider(providerID); ok {
		return cfg, nil
	}
	if strings.HasPrefix(providerID, "oidc.") {
		return b.resolveOIDCProvider(ctx, providerID)
	}
	return nil, nil
}

// staticProvider returns the env-backed config for non-DB providers
// (GitHub and Google). Returns (nil, false) when the provider is
// unknown OR its credentials aren't set in the env — the latter
// case is treated as "not configured" rather than a config error so
// dev environments without those clients don't fail boot.
func (b *OAuthBroker) staticProvider(providerID string) (*providerConfig, bool) {
	switch providerID {
	case "github":
		if b.cfg.GitHubClientID == "" || b.cfg.GitHubClientSecret == "" {
			return nil, false
		}
		return &providerConfig{
			id:                   "github",
			authorizeURL:         "https://github.com/login/oauth/authorize",
			tokenURL:             "https://github.com/login/oauth/access_token",
			scopes:               []string{"read:user", "user:email"},
			extraAuthorizeParams: map[string]string{"allow_signup": "true"},
			clientID:             b.cfg.GitHubClientID,
			clientSecret:         b.cfg.GitHubClientSecret,
			tokenRequestHeaders:  map[string]string{"Accept": "application/json"},
			kind:                 kindGitHubAccessToken,
		}, true
	case "google":
		if b.cfg.GoogleClientID == "" || b.cfg.GoogleClientSecret == "" {
			return nil, false
		}
		return &providerConfig{
			id:           "google",
			authorizeURL: "https://accounts.google.com/o/oauth2/v2/auth",
			tokenURL:     "https://oauth2.googleapis.com/token",
			// openid yields the id_token; email+profile populate the
			// claims Firebase consumes. Endpoints are hard-coded —
			// staticProvider is synchronous (no discovery round-trip)
			// and Google's OIDC endpoints have been stable for years.
			scopes:       []string{"openid", "email", "profile"},
			clientID:     b.cfg.GoogleClientID,
			clientSecret: b.cfg.GoogleClientSecret,
			// Account picker, not prompt=login — a "Sign in with
			// Google" click shouldn't force a password re-entry on an
			// existing Google session.
			prompt: "select_account",
			kind:   kindGoogleIDToken,
		}, true
	}
	return nil, false
}

// resolveOIDCProvider builds a providerConfig from the SsoConfig row
// matching `firebase_provider_id`. Decrypts the client secret and
// fetches the issuer's discovery document to learn the
// authorization_endpoint and token_endpoint. Returns (nil, nil) for
// unknown / disabled providers so the caller surfaces 404.
func (b *OAuthBroker) resolveOIDCProvider(ctx context.Context, providerID string) (*providerConfig, error) {
	row, err := b.queries.GetSsoConfigByFirebaseProviderID(ctx, providerID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("lookup sso config: %w", err)
	}

	// `oidc_config` is the JSONB column; deserialize the small bit
	// we need (issuer + client_id). Saml-only configs have a NULL
	// oidc_config — surfaces as "not OIDC" → 404.
	if len(row.OidcConfig) == 0 {
		return nil, nil
	}
	var oidc struct {
		Issuer   string `json:"issuer"`
		ClientID string `json:"client_id"`
	}
	if err := json.Unmarshal(row.OidcConfig, &oidc); err != nil {
		// Existence-probe defense: any "row exists but unusable" path
		// returns the same 404 a missing row does. Real reason logged.
		b.logger.Warn("oidc config malformed", "provider", providerID, "error", err)
		return nil, nil
	}
	if oidc.Issuer == "" || oidc.ClientID == "" {
		return nil, nil
	}
	// RFC 8414 §3 requires HTTPS for the issuer. Without this an
	// org admin could set an http:// issuer and the broker would
	// fetch the discovery doc unauthenticated AND POST the
	// client_secret to a possibly-MITMable token endpoint over
	// plaintext. Localhost is allowed because dev IdPs (Keycloak
	// in docker, etc.) commonly run without TLS.
	if err := requireSecureIssuer(oidc.Issuer); err != nil {
		b.logger.Warn("oidc issuer rejected", "provider", providerID, "issuer", oidc.Issuer, "error", err)
		return nil, nil
	}

	if len(row.ClientSecretCiphertext) == 0 {
		// SsoConfig exists but the secret was never set / was cleared.
		// Without a secret we can't drive code-flow.
		return nil, nil
	}
	secretBytes, err := b.encryptor.Decrypt(row.ClientSecretCiphertext)
	if err != nil {
		// Existence-probe defense: collapse to "unknown provider" so
		// a malformed/legacy ciphertext doesn't surface as a different
		// status code from a missing row. Real reason logged.
		b.logger.Warn("decrypt client secret failed", "provider", providerID, "error", err)
		return nil, nil
	}

	disc, err := b.fetchOIDCDiscovery(ctx, oidc.Issuer)
	if err != nil {
		return nil, fmt.Errorf("fetch discovery: %w", err)
	}

	return &providerConfig{
		id:           providerID,
		authorizeURL: disc.AuthorizationEndpoint,
		tokenURL:     disc.TokenEndpoint,
		// Standard OIDC scopes; openid is required for an id_token.
		// `email`+`profile` cover the claims FB-side OIDC consumes.
		scopes:       []string{"openid", "email", "profile"},
		clientID:     oidc.ClientID,
		clientSecret: string(secretBytes),
		kind:         kindOIDCIDToken,
	}, nil
}

// oidcDiscovery is the subset of RFC 8414 we read.
type oidcDiscovery struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
}

func (b *OAuthBroker) fetchOIDCDiscovery(ctx context.Context, issuer string) (*oidcDiscovery, error) {
	// Cache hit short-circuits the network round-trip. Discovery
	// docs are conventionally cacheable for hours; we use a
	// 5-minute TTL to keep the staleness window small in case an
	// IdP rotates endpoints. Per-issuer keying because each org's
	// SsoConfig points at a different issuer.
	b.discoveryMu.Lock()
	if entry, ok := b.discoveryCache[issuer]; ok && time.Now().Before(entry.expiresAt) {
		doc := entry.doc
		b.discoveryMu.Unlock()
		return doc, nil
	}
	b.discoveryMu.Unlock()

	// Per RFC 8414 §3, the well-known URL appends `/.well-known/openid-configuration`
	// to the issuer.
	wkURL := strings.TrimRight(issuer, "/") + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, "GET", wkURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("discovery returned status %d", resp.StatusCode)
	}
	// Cap reads — discovery docs are small (a few KB).
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return nil, err
	}
	var disc oidcDiscovery
	if err := json.Unmarshal(body, &disc); err != nil {
		return nil, err
	}
	if disc.AuthorizationEndpoint == "" || disc.TokenEndpoint == "" {
		return nil, fmt.Errorf("discovery doc missing endpoints")
	}
	b.discoveryMu.Lock()
	b.discoveryCache[issuer] = discoveryCacheEntry{
		doc:       &disc,
		expiresAt: time.Now().Add(oidcDiscoveryCacheTTL),
	}
	b.discoveryMu.Unlock()
	return &disc, nil
}

// start handles GET /api/oauth/{provider}/start?return=<url>.
// Builds the IdP authorize URL with our broker's callback baked in
// and redirects the browser there.
func (b *OAuthBroker) start(w http.ResponseWriter, r *http.Request) {
	providerID := r.PathValue("provider")
	returnURL := r.URL.Query().Get("return")

	if returnURL == "" {
		b.renderError(w, r, http.StatusBadRequest, "missing_return", nil)
		return
	}
	if !b.isAllowedReturnURL(returnURL) {
		b.renderError(w, r, http.StatusBadRequest, "disallowed_return_url",
			fmt.Errorf("return=%q", returnURL))
		return
	}

	cfg, err := b.resolveProviderConfig(r.Context(), providerID)
	if err != nil {
		b.renderError(w, r, http.StatusInternalServerError, "provider_misconfigured", err)
		return
	}
	if cfg == nil {
		b.renderError(w, r, http.StatusNotFound, "unknown_provider",
			fmt.Errorf("provider=%q", providerID))
		return
	}

	state, payload, err := signOAuthState(b.cfg.AppKey, returnURL, providerID)
	if err != nil {
		b.renderError(w, r, http.StatusInternalServerError, "sign_state_failed", err)
		return
	}

	authURL, err := url.Parse(cfg.authorizeURL)
	if err != nil {
		b.renderError(w, r, http.StatusInternalServerError, "parse_authorize_url_failed", err)
		return
	}
	// Build authorize params. Apply the provider's `extraAuthorizeParams`
	// FIRST so the security-critical params below always win — a
	// future provider config that mistakenly sets `client_id` /
	// `redirect_uri` / `state` / `nonce` / `scope` / `response_type`
	// in its extras can't override what the broker decided.
	q := authURL.Query()
	for k, v := range cfg.extraAuthorizeParams {
		q.Set(k, v)
	}
	q.Set("client_id", cfg.clientID)
	q.Set("redirect_uri", b.callbackURL(providerID))
	q.Set("response_type", "code")
	q.Set("scope", strings.Join(cfg.scopes, " "))
	q.Set("state", state)
	// Forward `login_hint` if the caller supplied one. Keycloak,
	// Okta, Google, etc. all honor this OIDC-standard param to
	// pre-fill the username/email field on the IdP login page —
	// saves the user one re-type when our own SSO entry already
	// asked for the email. Empty value falls through (no hint).
	if hint := r.URL.Query().Get("login_hint"); hint != "" {
		q.Set("login_hint", hint)
	}
	// Static providers declare their authorize-time `prompt`
	// explicitly (Google → select_account). OIDC SSO providers leave
	// it empty and get prompt=login forced in the block below.
	if cfg.prompt != "" {
		q.Set("prompt", cfg.prompt)
	}
	// OIDC nonce convention (matches Firebase's Apple-Sign-In and
	// generic OIDC contract): the value sent to the IdP in the
	// authorize request is `SHA256(rawNonce)` (hex); the IdP echoes
	// that hash back as the id_token's `nonce` claim; native passes
	// `rawNonce` (UNhashed) into OAuthProvider.credential, and
	// Firebase re-computes SHA256(rawNonce) and compares with the
	// id_token's claim. Both sides must use the same encoding —
	// hex(SHA256(utf8(rawNonce))) is what FB expects.
	//
	// We reuse `payload.N` as the rawNonce (16 random bytes encoded
	// base64url) — it's already bound to this flow's signed state,
	// so we don't need a second source of randomness. The callback
	// echoes payload.N to native untouched; the broker is the only
	// thing that hashes.
	if cfg.kind == kindOIDCIDToken {
		sum := sha256.Sum256([]byte(payload.N))
		q.Set("nonce", hex.EncodeToString(sum[:]))
		// Force re-authentication at the IdP. Without this an already-
		// signed-in IdP session lets the user through immediately,
		// which is the wrong default for an explicit "Sign in"
		// click — the user pressed sign-in to assert "this is me,
		// right now," not to silently re-use whatever session their
		// browser happens to have. RFC 6749 §3.1.2.1 / OIDC Core
		// §3.1.2.1 — `prompt=login` is the standard verb every
		// compliant IdP (Keycloak, Okta, Auth0, Google, Apple, etc.)
		// honors. Use `prompt=select_account` instead when "switch
		// account" semantics are wanted.
		q.Set("prompt", "login")
	}
	authURL.RawQuery = q.Encode()

	http.Redirect(w, r, authURL.String(), http.StatusFound)
}

// callback handles GET /api/oauth/{provider}/callback?code=…&state=….
// Verifies state, exchanges the code for the IdP's tokens, then
// redirects to the original `return` URL with the credential in the
// fragment.
func (b *OAuthBroker) callback(w http.ResponseWriter, r *http.Request) {
	providerID := r.PathValue("provider")
	q := r.URL.Query()
	code := q.Get("code")
	stateToken := q.Get("state")
	providerErr := q.Get("error")

	if providerErr != "" {
		b.respondWithProviderError(w, r, stateToken, providerErr, q.Get("error_description"))
		return
	}
	if code == "" || stateToken == "" {
		b.renderError(w, r, http.StatusBadRequest, "missing_code_or_state", nil)
		return
	}

	state, err := verifyOAuthState(b.cfg.AppKey, stateToken)
	if err != nil {
		b.renderError(w, r, http.StatusBadRequest, "invalid_state", err)
		return
	}
	if state.P != providerID {
		b.renderError(w, r, http.StatusBadRequest, "provider_mismatch",
			fmt.Errorf("state.P=%q url=%q", state.P, providerID))
		return
	}
	// Defense-in-depth: re-run the return-URL allowlist on the
	// signed state. We validated state.R when signing, but the
	// allowlist may have tightened in the interim (config change,
	// new build, etc.) — a 10-minute-old token shouldn't grant
	// redirect to a now-disallowed origin.
	if !b.isAllowedReturnURL(state.R) {
		b.renderError(w, r, http.StatusBadRequest, "disallowed_return_url",
			fmt.Errorf("state.R=%q", state.R))
		return
	}

	cfg, err := b.resolveProviderConfig(r.Context(), providerID)
	if err != nil {
		b.renderError(w, r, http.StatusInternalServerError, "provider_misconfigured", err)
		return
	}
	if cfg == nil {
		b.renderError(w, r, http.StatusNotFound, "unknown_provider",
			fmt.Errorf("provider=%q", providerID))
		return
	}

	tokens, err := b.exchangeCode(r.Context(), cfg, code)
	if err != nil {
		b.renderError(w, r, http.StatusBadGateway, "token_exchange_failed", err)
		return
	}

	// Build the return URL with the credential in the fragment.
	// Fragment never travels in HTTP requests, so this avoids
	// referrer-leak / access-log exposure of the IdP token.
	returnURL, err := url.Parse(state.R)
	if err != nil {
		b.renderError(w, r, http.StatusBadRequest, "invalid_return_url", err)
		return
	}
	frag := url.Values{}
	frag.Set("provider", providerID)
	frag.Set("kind", string(cfg.kind))
	switch cfg.kind {
	case kindGitHubAccessToken:
		if tokens.AccessToken == "" {
			b.renderError(w, r, http.StatusBadGateway, "missing_access_token", nil)
			return
		}
		frag.Set("token", tokens.AccessToken)
	case kindOIDCIDToken, kindGoogleIDToken:
		if tokens.IDToken == "" {
			b.renderError(w, r, http.StatusBadGateway, "missing_id_token", nil)
			return
		}
		frag.Set("token", tokens.IDToken)
		if tokens.AccessToken != "" {
			frag.Set("access_token", tokens.AccessToken)
		}
		// Corporate OIDC: the client rebuilds an OAuthCredential and
		// Firebase verifies sha256(rawNonce) against the id_token's
		// nonce claim, so echo the raw nonce (the `n` field of state).
		// Google does NOT use the nonce flow — GoogleAuthProvider's
		// credential takes no rawNonce — and start() never sent a
		// nonce, so there is nothing to echo.
		if cfg.kind == kindOIDCIDToken {
			frag.Set("nonce", state.N)
		}
	}
	returnURL.Fragment = frag.Encode()
	http.Redirect(w, r, returnURL.String(), http.StatusFound)
}

// tokenResponse is the union of fields we read from token endpoints.
// Different IdPs populate different subsets — GitHub returns only
// access_token; OIDC returns id_token + access_token.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	IDToken     string `json:"id_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
}

func (b *OAuthBroker) exchangeCode(ctx context.Context, cfg *providerConfig, code string) (*tokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("client_id", cfg.clientID)
	form.Set("client_secret", cfg.clientSecret)
	form.Set("redirect_uri", b.callbackURL(cfg.id))

	req, err := http.NewRequestWithContext(ctx, "POST", cfg.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range cfg.tokenRequestHeaders {
		req.Header.Set(k, v)
	}
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, string(body))
	}
	var tok tokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return nil, err
	}
	return &tok, nil
}

// callbackURL builds the broker's redirect_uri for the given
// provider. The IdP must have THIS exact URL registered (Keycloak's
// Valid Redirect URIs, GitHub's OAuth app callback URL, etc.).
func (b *OAuthBroker) callbackURL(providerID string) string {
	return strings.TrimRight(b.cfg.BaseURL, "/") + "/internal/v1/auth/" + providerID + "/callback"
}

// isAllowedReturnURL guards against open-redirect abuse. Three caller
// shapes are accepted as a `return=` value: the native `pivox://`
// custom scheme, an http loopback URL (Electron's RFC 8252 loopback
// server), and same-origin web URLs relative to our base URL.
func (b *OAuthBroker) isAllowedReturnURL(candidate string) bool {
	u, err := url.Parse(candidate)
	if err != nil {
		return false
	}
	// Native apps (SwiftUI / .NET) catch the redirect via the pivox://
	// custom scheme.
	if u.Scheme == "pivox" {
		return true
	}
	// Electron catches the redirect with an ephemeral http server on a
	// loopback IP (RFC 8252). Restricted to literal loopback IPs (not
	// the "localhost" hostname) so the credential can only ever land
	// on the user's own machine without trusting name resolution.
	// Userinfo is rejected — a clean loopback return URL never has it.
	if u.Scheme == "http" && u.User == nil && isLoopbackIPHost(u.Host) {
		return true
	}
	// Browser apps redirect back to a same-origin web page.
	if b.cfg.BaseURL == "" {
		return false
	}
	base, err := url.Parse(b.cfg.BaseURL)
	if err != nil {
		return false
	}
	return u.Scheme == base.Scheme && u.Host == base.Host
}

// respondWithProviderError tries to bubble the IdP's error back to
// the original `return` URL via the fragment. Falls back to a plain
// 400 if state is unusable (we can't recover the return URL
// without it).
func (b *OAuthBroker) respondWithProviderError(w http.ResponseWriter, r *http.Request, stateToken, code, desc string) {
	if stateToken == "" {
		b.renderError(w, r, http.StatusBadRequest, "provider_error_no_state",
			fmt.Errorf("idp_error=%q desc=%q", code, desc))
		return
	}
	state, err := verifyOAuthState(b.cfg.AppKey, stateToken)
	if err != nil {
		b.renderError(w, r, http.StatusBadRequest, "provider_error_invalid_state",
			fmt.Errorf("idp_error=%q desc=%q verify=%v", code, desc, err))
		return
	}
	if !b.isAllowedReturnURL(state.R) {
		// Allowlist may have tightened since the state was signed —
		// never redirect to a now-disallowed origin even with the
		// IdP error in the fragment.
		b.renderError(w, r, http.StatusBadRequest, "provider_error_disallowed_return",
			fmt.Errorf("idp_error=%q state.R=%q", code, state.R))
		return
	}
	returnURL, err := url.Parse(state.R)
	if err != nil {
		b.renderError(w, r, http.StatusBadRequest, "provider_error_bad_return",
			fmt.Errorf("idp_error=%q desc=%q parse=%v", code, desc, err))
		return
	}
	frag := url.Values{}
	frag.Set("error", code)
	if desc != "" {
		frag.Set("error_description", desc)
	}
	returnURL.Fragment = frag.Encode()
	http.Redirect(w, r, returnURL.String(), http.StatusFound)
}

// renderError emits a user-friendly HTML page in place of a raw
// `http.Error` plaintext response. Internal reason codes
// ("invalid_state", "token_exchange_failed", etc.) are useful for
// debugging but should never leak to end users mid-auth — they're
// confusing and disclose implementation details.
//
// Pattern:
//   - Caller passes the HTTP status, a one-line internal reason
//     (logged), the actual error (logged), and ANY context the
//     correlation lookup will need.
//   - We mint a correlation ID, log {status, reason, error, ID, request
//     path}, and render an HTML page with a friendly headline + the
//     correlation ID for the user to relay to support.
//
// The native macOS app sees this rendered page inside its
// ASWebAuthenticationSession popup; the user closes the popup and
// gets back to the login card. Keeping this server-side as HTML
// (rather than redirecting back to native with an error fragment)
// because some of these failures happen BEFORE we have a verified
// `return` URL we can trust.
func (b *OAuthBroker) renderError(w http.ResponseWriter, r *http.Request, status int, reason string, err error) {
	corr := uuid.NewString()
	b.logger.Error("oauth broker error",
		"correlation_id", corr,
		"status", status,
		"reason", reason,
		"path", r.URL.Path,
		"error", err)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Correlation-Id", corr)
	w.WriteHeader(status)

	// Tiny static template — keep this self-contained so an asset-
	// pipeline rebuild isn't required to ship a copy change.
	const tmpl = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Sign in error</title>
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>
  :root { color-scheme: light dark; }
  body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", system-ui, sans-serif;
         max-width: 420px; margin: 12vh auto; padding: 0 24px; text-align: center; }
  h1 { font-size: 1.25rem; margin-bottom: 0.5rem; }
  p { color: #666; line-height: 1.5; }
  code { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 0.85rem;
         padding: 0.15rem 0.4rem; background: rgba(127,127,127,0.15); border-radius: 4px; }
</style>
</head>
<body>
<h1>Couldn't complete sign-in</h1>
<p>Something went wrong on our end. You can close this window and try again.</p>
<p>If the problem persists, contact support and reference: <code>%s</code></p>
</body>
</html>`
	_, _ = fmt.Fprintf(w, tmpl, html.EscapeString(corr))
}

// requireSecureIssuer enforces the OIDC issuer URL constraints:
// must parse, must be `https://`, with a localhost exception so
// dev IdPs (Keycloak in docker, ory hydra, etc.) without TLS still
// work. Returns a descriptive error that's safe to log but is NEVER
// surfaced to the user — the broker maps any rejection here to the
// same 404 a missing row produces.
func requireSecureIssuer(issuer string) error {
	u, err := url.Parse(issuer)
	if err != nil {
		return fmt.Errorf("parse issuer: %w", err)
	}
	if u.Scheme == "https" {
		return nil
	}
	if u.Scheme == "http" && isLocalhostHost(u.Host) {
		return nil
	}
	return fmt.Errorf("issuer must be https (got scheme=%q host=%q)", u.Scheme, u.Host)
}

// splitHostNoPort extracts the host from a host[:port] string,
// stripping IPv6 brackets. net.SplitHostPort strips brackets only
// when a port is present; a port-less bracketed IPv6 host ("[::1]")
// would otherwise keep them and fail netip.ParseAddr.
func splitHostNoPort(host string) string {
	h := host
	if hostOnly, _, err := net.SplitHostPort(h); err == nil {
		h = hostOnly
	}
	h = strings.TrimPrefix(h, "[")
	return strings.TrimSuffix(h, "]")
}

// isLocalhostHost reports whether the host (with optional port) is
// a loopback address — `localhost`, `127.0.0.0/8`, or `::1`.
func isLocalhostHost(host string) bool {
	h := splitHostNoPort(host)
	if h == "localhost" {
		return true
	}
	addr, err := netip.ParseAddr(h)
	if err != nil {
		return false
	}
	return addr.IsLoopback()
}

// isLoopbackIPHost reports whether host (with optional port) is a
// loopback IP literal — 127.0.0.0/8 or ::1. Unlike isLocalhostHost it
// deliberately rejects the "localhost" hostname: the OAuth return-URL
// allowlist must resolve to loopback without trusting name resolution
// (RFC 8252 §8.3 recommends the IP literal). Kept separate from
// isLocalhostHost so the two security gates stay independently
// auditable; both share splitHostNoPort so a parsing fix reaches both.
func isLoopbackIPHost(host string) bool {
	addr, err := netip.ParseAddr(splitHostNoPort(host))
	if err != nil {
		return false
	}
	return addr.IsLoopback()
}
