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
	DatabaseURL      string
	GRPCPort         string
	ServiceGRPCPort  string // service-to-service gRPC listener (AgentService et al.)
	RESTPort         string
	DebugPort        string
	LogLevel         string
	RateLimitEnabled bool
	// TrustedProxies is the set of CIDR blocks whose connections are
	// trusted to set X-Forwarded-For. When the connection's
	// RemoteAddr falls in this set, the leftmost X-Forwarded-For
	// entry that is NOT itself a trusted-proxy IP is used as the
	// rate-limit identity. When RemoteAddr is NOT in this set, the
	// header is ignored and RemoteAddr alone is used.
	//
	// Default empty list = "fail closed" — never trust the header,
	// always key on RemoteAddr. Dev configs typically set this to
	// ["0.0.0.0/0", "::/0"] (trust everyone — same machine, no
	// adversaries). Prod configs set it to the load balancer's CIDR.
	TrustedProxies []string
	SyncAuth       SyncAuthConfig
	DelegatedAuth  DelegatedAuthConfig
	OAuthBroker    OAuthBrokerConfig
}

// OAuthBrokerConfig controls the server-side OAuth/OIDC broker that
// handles federated sign-in for native and web clients (formerly the
// TanStack `start` /api/oauth/* routes; consolidated server-side so
// auth machinery lives next to syncIdentity, exchangeToken,
// resolveProvider, etc.). The broker drives the IdP code-flow
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
