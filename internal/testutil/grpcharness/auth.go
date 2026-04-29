//go:build dev

package grpcharness

import (
	"context"

	"github.com/dashkan/pivox/internal/authn"
)

// testAuthService is the harness's authn.Service implementation. It
// trusts the bearer token verbatim: the token IS the verified UID.
// VerifyToken always succeeds; tests control which UID a request
// authenticates as by changing the outgoing metadata via
// Harness.SetCaller.
//
// The other authn.Service methods (CreateCustomToken, DeleteUser,
// Create/Update/DeleteOidcProvider, Create/Update/DeleteSamlProvider)
// are stubbed to no-op success. Tests that need to assert specific
// side-effect calls construct their own mock and pass it via
// WithAuth(...) so the harness's fake doesn't shadow assertions.
type testAuthService struct{}

func (testAuthService) VerifyToken(_ context.Context, token string) (*authn.Identity, error) {
	return &authn.Identity{UID: token}, nil
}

func (testAuthService) CreateCustomToken(_ context.Context, uid string) (string, error) {
	return "test-token-" + uid, nil
}

func (testAuthService) DeleteUser(_ context.Context, _ string) error { return nil }

func (testAuthService) CreateOidcProvider(_ context.Context, _ authn.OidcProviderConfig) error {
	return nil
}

func (testAuthService) UpdateOidcProvider(_ context.Context, _ authn.OidcProviderConfig) error {
	return nil
}

func (testAuthService) DeleteOidcProvider(_ context.Context, _ string) error { return nil }

func (testAuthService) CreateSamlProvider(_ context.Context, _ authn.SamlProviderConfig) error {
	return nil
}

func (testAuthService) UpdateSamlProvider(_ context.Context, _ authn.SamlProviderConfig) error {
	return nil
}

func (testAuthService) DeleteSamlProvider(_ context.Context, _ string) error { return nil }
