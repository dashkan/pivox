package server

import (
	"context"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/dashkan/pivox/internal/authn"
)

// firebaseIssuerPrefix matches the `iss` claim of every Firebase ID
// token. Full value: `https://securetoken.google.com/<project-id>`.
// We match on the prefix so a single check works across projects
// (dev / staging / prod) without threading project IDs through this
// layer.
const firebaseIssuerPrefix = "https://securetoken.google.com/"

// SsrVerifyFunc verifies an SA-signed JWT minted by the SSR server
// on behalf of a specific user, and returns the asserted Pivox
// identity UUID from the JWT's `actor_uid` claim.
//
// Implementations MUST enforce, at minimum:
//   - Signature verification against the issuing service account's
//     public keys (JWKS at `googleapis.com/service_accounts/v1/jwk/<sa>`).
//   - Audience match (the JWT was minted for THIS Pivox deployment).
//   - Issuer-allowlist match (the SA that signed is permitted to
//     act on behalf of users — i.e., it's an SSR service).
//   - Presence of the `actor_uid` claim as a parseable UUID.
//
// Production wires the keyfunc-backed implementation. Tests pass
// either a stub closure or nil (to leave SSR disabled).
type SsrVerifyFunc func(ctx context.Context, token string) (uuid.UUID, error)

// CompositeAuthService wraps a Firebase authn.Service and adds an
// SSR-acting-as token verification path. Tokens are routed by JWT
// `iss` claim: a token whose iss is the Firebase issuer prefix (or
// any token that doesn't parse) is delegated to the wrapped Firebase
// service. A token whose iss is anything else is sent to the SSR
// verifier; the verifier does the actual signature + audience +
// issuer-allowlist + claim checking and returns the user UUID.
//
// Implements authn.Service by embedding. Only VerifyToken is
// overridden; the rest (CreateCustomToken, DeleteUser, SSO provider
// methods) pass through to the wrapped service unchanged.
//
// The AuthInterceptor doesn't know this type exists — it just calls
// `auth.VerifyToken`. Production wires `NewCompositeAuthService(fb,
// ssrVerify)`; dev/test deployments pass the bare Firebase service.
// Either way, the interceptor's signature and routing logic stays
// the same.
type CompositeAuthService struct {
	authn.Service               // wrapped Firebase implementation
	ssrVerify     SsrVerifyFunc // optional; if nil, this wrapper isn't constructed
}

// NewCompositeAuthService wires the SSR token-verification function
// around a Firebase authn.Service.
//
// When `ssrVerify` is nil, returns the wrapped service as-is so
// callers don't pay for a wrapper that has nothing to add — useful
// for dev/test deployments and any environment where SSR isn't
// configured.
func NewCompositeAuthService(fb authn.Service, ssrVerify SsrVerifyFunc) authn.Service {
	if ssrVerify == nil {
		return fb
	}
	return &CompositeAuthService{Service: fb, ssrVerify: ssrVerify}
}

// VerifyToken routes by JWT `iss` claim:
//   - Token parses as JWT with `iss` that doesn't match the Firebase
//     issuer prefix → ssrVerify (SA-signed actor-acting-as path).
//   - Anything else (Firebase iss, malformed JWT, non-JWT bearer,
//     missing iss) → wrapped Firebase service.
//
// The "fall through to Firebase on doubt" rule keeps routing fail-
// safe: a token can never sneak past Firebase verification by being
// malformed enough to confuse the router.
func (c *CompositeAuthService) VerifyToken(ctx context.Context, token string) (*authn.Identity, error) {
	iss, err := unverifiedIssuer(token)
	if err == nil && iss != "" && !strings.HasPrefix(iss, firebaseIssuerPrefix) {
		uid, verifyErr := c.ssrVerify(ctx, token)
		if verifyErr != nil {
			return nil, verifyErr
		}
		// Synthesize the Identity shape the AuthInterceptor expects.
		// `UID` isn't a real Firebase UID — there is no Firebase
		// account for the SSR server. We populate it with the Pivox
		// identity UUID as a string so the field has a value;
		// downstream consumers read `pivox_user_id` from Claims (the
		// only field that matters for handler dispatch) and ignore
		// UID entirely.
		return &authn.Identity{
			UID:    uid.String(),
			Claims: map[string]any{"pivox_user_id": uid.String()},
		}, nil
	}
	return c.Service.VerifyToken(ctx, token)
}

// unverifiedIssuer extracts the `iss` claim from a JWT WITHOUT
// verifying the signature. Used by routing only — the chosen verifier
// (Firebase Admin SDK or ssrVerify) performs full cryptographic
// verification afterward, so an attacker can't exploit the
// unverified read by crafting a token they can't sign.
//
// Returns the iss string + nil on success, "" + error on a token
// that isn't parseable JWT shape (which is fine — the caller routes
// to Firebase, which handles the actual rejection).
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
