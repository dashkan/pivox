//go:build !dev

package config

// SyncAuthConfig holds configuration for authenticating internal service-to-service
// calls (e.g., Firebase Functions → accounts:sync). In production, this uses
// Google Cloud OIDC identity tokens verified against the caller's service account.
type SyncAuthConfig struct {
	// AllowedServiceAccounts is a list of service account emails permitted to
	// call internal endpoints. Verified against the OIDC token's email claim.
	AllowedServiceAccounts []string

	// Audience is the expected audience in the OIDC token. Typically the
	// backend's public URL (e.g., "https://api.pivox.app").
	Audience string
}
