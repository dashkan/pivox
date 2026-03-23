//go:build dev

package config

// SyncAuthConfig holds configuration for authenticating internal service-to-service
// calls in development mode. Uses a static shared secret since the Firebase
// Functions emulator cannot mint Google Cloud OIDC identity tokens.
type SyncAuthConfig struct {
	// SharedSecret is the static bearer token used to authenticate calls
	// from the Firebase Functions emulator to internal endpoints.
	SharedSecret string
}
