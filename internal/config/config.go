package config

import "time"

// Config holds all server configuration. Populated from cobra flags
// in cmd/pivox-cloud/main.go, with env var fallbacks.
//
// Note: there is no GoogleCloudConfig — Firebase credentials and
// project ID resolve entirely through Google's standard Application
// Default Credentials chain (service-account JSON → metadata server →
// gcloud user identity). Operators do not configure GCP knobs in
// Pivox-named env vars.
type Config struct {
	DatabaseURL     string
	GRPCPort        string
	ServiceGRPCPort string // service-to-service gRPC listener (AgentService et al.)
	RESTPort        string
	DebugPort       string
	LogLevel        string
	// EnableReflection registers gRPC server reflection (grpc.reflection.v1
	// and v1alpha) on the gRPC servers. OFF by default: reflection exposes
	// the full API surface to unauthenticated callers (the AuthInterceptor
	// exempts reflection methods, so registering it makes the schema world-
	// readable). Enable only in dev — PIVOX_ENABLE_REFLECTION=true — for
	// tooling like grpcurl / buf curl. In production this stays unset AND
	// the edge proxy blocks the reflection route (defense in depth).
	EnableReflection bool
	// Rate limiting is the responsibility of the edge proxy / load
	// balancer in front of pivox-cloud (Cloudflare, GCLB, nginx). The
	// Cloud Controller does not implement app-level per-IP limits;
	// abuse defenses live in single-use codes, TTLs, response-shape
	// uniformity, and the auth chain.
	SyncAuth      ServiceAccountAuthConfig
	DelegatedAuth DelegatedAuthConfig
	OAuthBroker   OAuthBrokerConfig
	OIDC          OIDCConfig
}

// OIDCConfig configures the backend's OIDC access-token verifier (Keycloak).
// The Cloud Controller is a pure resource server: it validates Bearer access
// tokens against the issuer's JWKS. There is deliberately no client_id /
// client_secret here — those belong to the BFF (the `start` server runs the
// OAuth code exchange), not the resource server.
type OIDCConfig struct {
	// Issuer is the exact `iss` accepted tokens must carry
	// (e.g. https://pivox.ngrok.app/realms/pivox). Empty leaves OIDC auth off
	// (Firebase-only) during the migration.
	Issuer string

	// Audience the access token's `aud` must contain — the value of the
	// Keycloak audience mapper (e.g. pivox-cloud). Required unless
	// DisableAudienceValidation.
	Audience string

	// DisableAudienceValidation turns the aud check off (opt-out, wired to
	// --disable-oidc-audience-validation). Audience validation is fail-closed
	// otherwise: an empty Audience with OIDC enabled is a startup error.
	DisableAudienceValidation bool
}

// OAuthBrokerConfig controls the server-side OAuth/OIDC broker that
// handles federated sign-in for native and web clients (formerly the
// TanStack `start` /api/oauth/* routes; consolidated server-side so
// auth machinery lives next to syncIdentity, resolveProvider,
// etc.). The broker drives the IdP code-flow
// handshake using the client_secret stored server-side and returns
// to the native app via a custom URL scheme with the IdP token in
// the URL fragment. See `internal_hooks_oauth_broker.go`.
type OAuthBrokerConfig struct {
	// AppKey is the HMAC secret used to sign the broker's `state`
	// token. ≥32 bytes. Rotating this invalidates every in-flight
	// flow but does not affect any persistent server state.
	AppKey string

	// BaseURL is the server's public origin used to construct the
	// IdP-facing redirect_uri (`{BaseURL}/api/oauth/{provider}/callback`).
	// Must match what the IdP has on file as a Valid Redirect URI.
	BaseURL string

	// GitHub OAuth app credentials. Empty disables GitHub federation.
	GitHubClientID     string
	GitHubClientSecret string

	// Google OAuth (Web application) client credentials. Empty
	// disables Google federation. The client must live in the same
	// GCP project as the Firebase project so Firebase trusts the
	// id_token the broker mints.
	GoogleClientID     string
	GoogleClientSecret string
}

// DelegatedAuthConfig controls the delegated auth session flow used by
// plugins (NRCS ActiveX, Adobe UXP, etc.) that cannot safely authenticate
// in-process and must hand off to the Pivox app (AUTHN-07).
type DelegatedAuthConfig struct {
	// SessionTTL bounds how long a plugin has between creating a session
	// and the app completing it before the code expires.
	SessionTTL time.Duration

	// PollInterval is returned to clients in the createDelegatedAuthSession
	// response so they poll at a rate the server is comfortable with.
	PollInterval time.Duration
}
