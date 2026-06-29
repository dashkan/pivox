package config

// ServiceAccountAuthConfig configures verification of JWTs signed
// by Google Cloud service accounts.
//
// Currently used by one surface — SyncAuth: Firebase Functions
// calling `/internal/v1/auth:syncIdentity`. The Functions runtime
// mints OIDC identity tokens via its default service account; Pivox
// verifies them via `google.golang.org/api/idtoken` (signed by
// Google's OIDC issuer, NOT the function's own SA).
//
// The type is kept general (allowlist of SA emails + expected
// audience) so additional service-to-service surfaces can reuse the
// shape with their own SEPARATE instance — each surface owns its own
// trust boundary, so allowlists must never be shared across surfaces.
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
