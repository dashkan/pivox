// Package authn defines the authentication service interface used throughout
// the application. The concrete implementation lives in internal/firebase;
// this package contains only the contract so consumers never import a
// specific identity provider SDK.
package authn

import "context"

// Identity represents a verified user identity, independent of the
// underlying identity provider (Firebase, Auth0, custom JWT, etc.).
type Identity struct {
	UID    string
	Email  string
	Claims map[string]any
}

// Service is the authentication interface that all identity provider
// implementations must satisfy.
type Service interface {
	// VerifyToken validates a bearer token and returns the caller's identity.
	VerifyToken(ctx context.Context, token string) (*Identity, error)

	// CreateCustomToken mints a provider-specific token for the given UID
	// that a client can use to sign in.
	CreateCustomToken(ctx context.Context, uid string) (string, error)

	// DeleteUser removes the user from the underlying identity provider.
	// Called as the LAST step of the DeleteUser LRO so a partial failure
	// leaves the Firebase identity alive (and Pivox state already
	// cleaned up — but recoverable). Implementations should be idempotent
	// (no error for already-deleted UIDs) so the LRO is safe to retry
	// after a transient failure.
	DeleteUser(ctx context.Context, uid string) error
}
