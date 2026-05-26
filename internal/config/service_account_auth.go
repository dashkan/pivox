package config

// ServiceAccountAuthConfig configures verification of JWTs signed
// by Google Cloud service accounts. Pivox uses this for two
// independent service-to-service surfaces:
//
//   - SyncAuth: Firebase Functions calling
//     `/internal/v1/auth:syncIdentity`. The Functions runtime mints
//     OIDC identity tokens via its default service account; Pivox
//     verifies them via `google.golang.org/api/idtoken` (signed by
//     Google's OIDC issuer, NOT the function's own SA).
//   - SsrAuth: the SSR server (TanStack Start) minting per-user
//     `actor_uid` JWTs for SSR-acting-as data prefetch. These are
//     SA-signed JWTs (signed by the SSR server's own SA private
//     key, verified against the SA's published JWKS).
//
// The two surfaces share the SHAPE of this config (allowlist of SA
// emails + expected audience) but use SEPARATE instances. Each
// surface has its own trust boundary — the Firebase Functions SA
// is permitted to call sync; the SSR server SA is permitted to act
// on behalf of users. Sharing a single allowlist would expand
// either trust to both surfaces, which is a security regression.
type ServiceAccountAuthConfig struct {
	// AllowedServiceAccounts is the list of Google Cloud
	// service-account emails permitted to call this surface.
	// Verified against the token's `iss` (or `email`) claim
	// depending on the surface.
	AllowedServiceAccounts []string

	// Audience is the expected `aud` claim. Typically the backend's
	// public URL (e.g., "https://api.pivox.app"). Tokens with a
	// different audience are rejected.
	Audience string
}

// Enabled reports whether this surface has a usable configuration.
// Both Audience and AllowedServiceAccounts must be set for the
// downstream verifier to construct.
func (c ServiceAccountAuthConfig) Enabled() bool {
	return c.Audience != "" && len(c.AllowedServiceAccounts) > 0
}
