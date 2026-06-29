package grpcharness

import (
	"context"

	"github.com/dashkan/pivox/internal/authn"
)

// testAuthService is the harness's authn.Service implementation. It
// trusts the bearer token verbatim: the token IS the verified UID.
// VerifyToken always succeeds and returns the token as the identity
// UID — mirroring production, where the Keycloak access token's `sub`
// IS the Pivox identity id (a UUID). The auth interceptor parses that
// UID as the `identities.id` UUID, so a Caller authenticates as the
// identity whose id equals its UID.
//
// SeedIdentity creates the identities row with a generated UUID and
// sets that UUID as the Caller's UID, so any caller created via the
// harness flow authenticates cleanly. A token that isn't a UUID is
// rejected by the interceptor with Unauthenticated — the correct
// shape for "not a valid identity."
type testAuthService struct{}

func (testAuthService) VerifyToken(_ context.Context, token string) (*authn.Identity, error) {
	return &authn.Identity{UID: token}, nil
}
