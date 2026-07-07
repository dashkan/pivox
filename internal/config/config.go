package config

import "time"

// Config holds all server configuration. Populated from cobra flags
// in cmd/pivox-cloud/main.go, with env var fallbacks.
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
	OIDC OIDCConfig

	// Encryption selects the at-rest encryption backend (KEK source).
	Encryption EncryptionConfig
}

// EncryptionConfig selects the at-rest encryption backend. All backends
// are Tink-backed; only the key-encryption key differs.
type EncryptionConfig struct {
	// Provider is "local" (cleartext Tink keyset) or "gcp" (Cloud KMS).
	// Defaults to "local".
	Provider string
	// LocalKeyset is the base64-encoded cleartext Tink keyset used when
	// Provider is "local". It IS the master key — treat as a secret.
	LocalKeyset string
	// GCPKMSKeyName is the Cloud KMS key resource name used when Provider
	// is "gcp".
	GCPKMSKeyName string
}

// OIDCConfig configures the backend's OIDC access-token verifier (Keycloak).
// The Cloud Controller is a pure resource server: it validates Bearer access
// tokens against the issuer's JWKS. There is deliberately no client_id /
// client_secret here — those belong to the BFF (the `start` server runs the
// OAuth code exchange), not the resource server.
type OIDCConfig struct {
	// Issuer is the exact `iss` accepted tokens must carry
	// (e.g. https://pivox.ngrok.app/realms/pivox). Empty leaves OIDC auth off
	// entirely (unauthenticated; dev/test only).
	Issuer string

	// Audience the access token's `aud` must contain — the value of the
	// Keycloak audience mapper (e.g. pivox-cloud). Required unless
	// DisableAudienceValidation.
	Audience string

	// DisableAudienceValidation turns the aud check off (opt-out, wired to
	// --disable-oidc-audience-validation). Audience validation is fail-closed
	// otherwise: an empty Audience with OIDC enabled is a startup error.
	DisableAudienceValidation bool

	// JWKSRefreshInterval is how often the background goroutine re-fetches the
	// issuer's JWKS (wired to --oidc-jwks-refresh-interval, default 5m). Keycloak
	// rotates signing keys only on operator action, so this is just how fast
	// verifiers converge after a rotation. 0 = fetch once at startup, never
	// refresh. There is no on-demand refresh (see internal/oidc.Verifier).
	JWKSRefreshInterval time.Duration
}
