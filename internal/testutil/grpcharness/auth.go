//go:build dev

package grpcharness

import (
	"context"

	"github.com/dashkan/pivox/internal/authn"
	db "github.com/dashkan/pivox/internal/db/generated"
)

// testAuthService is the harness's authn.Service implementation. It
// trusts the bearer token verbatim: the token IS the verified UID.
// VerifyToken always succeeds; tests control which UID a request
// authenticates as by changing the outgoing metadata via
// Harness.SetCaller.
//
// Post-Phase-7, the real auth interceptor requires a `pivox_user_id`
// claim on every authenticated token. To match that contract here,
// VerifyToken looks up the `identities` row by UID and
// populates the claim with its UUID. SeedIdentity creates the row up
// front, so any caller created via the harness flow naturally has
// the claim. A token whose UID is not seeded yields an Identity with
// no claim — the interceptor will reject with Unauthenticated, which
// is the correct shape for "not synced."
//
// The other authn.Service methods (CreateCustomToken, DeleteUser,
// Create/Update/DeleteOidcProvider, Create/Update/DeleteSamlProvider)
// are stubbed to no-op success. Tests that need to assert specific
// side-effect calls construct their own mock and pass it via
// WithAuth(...) so the harness's fake doesn't shadow assertions.
type testAuthService struct {
	queries db.Querier
}

func (s testAuthService) VerifyToken(ctx context.Context, token string) (*authn.Identity, error) {
	id := &authn.Identity{UID: token}
	if s.queries != nil {
		if row, err := s.queries.GetIdentityByFirebaseUID(ctx, token); err == nil {
			id.Email = row.Email
			id.Claims = map[string]any{"pivox_user_id": row.ID.String()}
		}
	}
	return id, nil
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
