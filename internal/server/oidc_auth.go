package server

import (
	"context"

	"github.com/golang-jwt/jwt/v5"

	"github.com/dashkan/pivox/internal/authn"
)

// oidcTokenVerifier verifies an OIDC access token and returns the caller's
// identity (UID = the `sub` claim). *oidc.Verifier satisfies it; kept as a
// local interface so this package doesn't depend on the concrete type and tests
// can stub it.
type oidcTokenVerifier interface {
	VerifyToken(ctx context.Context, token string) (*authn.Identity, error)
}

// oidcAuthService routes Keycloak-issued tokens (`iss` == issuer) to the OIDC
// verifier and everything else to the wrapped service (Firebase). Transitional:
// once Firebase is removed the wrapped service goes away and the OIDC verifier
// becomes the sole authn.Service.
//
// Embeds authn.Service so CreateCustomToken / DeleteUser / SSO provider methods
// pass straight through to the wrapped service; only VerifyToken is routed.
type oidcAuthService struct {
	authn.Service                   // wrapped fallback for non-Keycloak issuers
	oidc          oidcTokenVerifier // Keycloak access-token verifier
	issuer        string            // the Keycloak realm issuer to route on
}

// NewOIDCAuthService wraps base so tokens whose `iss` equals issuer are verified
// by the OIDC verifier. Returns base unchanged when OIDC isn't configured
// (verifier nil or issuer empty), so Firebase-only deployments pay nothing.
func NewOIDCAuthService(base authn.Service, verifier oidcTokenVerifier, issuer string) authn.Service {
	if verifier == nil || issuer == "" {
		return base
	}
	return &oidcAuthService{Service: base, oidc: verifier, issuer: issuer}
}

// VerifyToken sends tokens issued by the Keycloak realm to the OIDC verifier;
// all other tokens (Firebase, non-JWT bearers) fall through to the wrapped
// service. Routing reads only the UNVERIFIED `iss` to pick the verifier — the
// chosen verifier still does full signature + claim validation.
func (s *oidcAuthService) VerifyToken(ctx context.Context, token string) (*authn.Identity, error) {
	if iss, err := unverifiedIssuer(token); err == nil && iss == s.issuer {
		return s.oidc.VerifyToken(ctx, token)
	}
	return s.Service.VerifyToken(ctx, token)
}

// unverifiedIssuer extracts the `iss` claim from a JWT WITHOUT verifying the
// signature. Used by routing only — the chosen verifier (Firebase Admin SDK or
// the OIDC verifier) performs full cryptographic verification afterward, so an
// attacker can't exploit the unverified read by crafting a token they can't
// sign.
//
// Returns the iss string + nil on success, "" + error on a token that isn't
// parseable JWT shape (which is fine — the caller routes to the wrapped Firebase
// service, which handles the actual rejection).
func unverifiedIssuer(token string) (string, error) {
	parsed, _, err := jwt.NewParser().ParseUnverified(token, jwt.MapClaims{})
	if err != nil {
		return "", err
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return "", jwt.ErrTokenInvalidClaims
	}
	iss, _ := claims["iss"].(string)
	return iss, nil
}
