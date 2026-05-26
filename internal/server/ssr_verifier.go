package server

import (
	"context"
	"errors"
	"fmt"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/dashkan/pivox/internal/config"
)

// jwksURLForServiceAccount is the canonical JWKS endpoint for a
// Google Cloud service account's signing keys. Google publishes both
// the X.509 PEM form (`/x509/<sa>`) and the JWKS form (`/jwk/<sa>`);
// we use the latter because keyfunc speaks JWKS natively.
const jwksURLForServiceAccount = "https://www.googleapis.com/service_accounts/v1/jwk/"

// NewKeyfuncSsrVerifier builds a production SsrVerifyFunc that
// verifies SA-signed JWTs against each allowlisted service account's
// published JWKS.
//
// Token verification enforces, in order:
//  1. JWT signature against the JWKS of the issuing service account
//     (keyfunc handles JWKS fetch + auto-refresh on `kid` miss, so
//     Google's signing-key rotation is transparent).
//  2. Required `exp` claim (no perpetual tokens).
//  3. Required `aud` claim matching cfg.Audience exactly.
//  4. `iss` claim is in cfg.AllowedServiceAccounts. (Belt-and-
//     suspenders — keyfunc has already proved the JWT was signed by
//     ONE of the SAs we loaded JWKS for, but we re-check the iss
//     claim explicitly so a signer that drops off the allowlist
//     loses authority immediately rather than at next JWKS refresh.)
//  5. Required `actor_uid` claim is a parseable UUID.
//
// Returns the asserted Pivox identity UUID + nil on success.
//
// The returned closure is safe for concurrent use. JWKS caching +
// HTTP refresh respect keyfunc's defaults (Cache-Control aware,
// background refresh, on-demand on unknown kid).
func NewKeyfuncSsrVerifier(ctx context.Context, cfg config.ServiceAccountAuthConfig) (SsrVerifyFunc, error) {
	if cfg.Audience == "" {
		return nil, errors.New("ssr: Audience is required")
	}
	if len(cfg.AllowedServiceAccounts) == 0 {
		return nil, errors.New("ssr: at least one allowed service account is required")
	}
	urls := make([]string, 0, len(cfg.AllowedServiceAccounts))
	for _, sa := range cfg.AllowedServiceAccounts {
		if sa == "" {
			return nil, errors.New("ssr: allowlist contains empty service-account email")
		}
		urls = append(urls, jwksURLForServiceAccount+sa)
	}
	return newSsrVerifierFromURLs(ctx, cfg.Audience, cfg.AllowedServiceAccounts, urls)
}

// newSsrVerifierFromURLs is the testable core. Production reaches it
// via NewKeyfuncSsrVerifier with the canonical Google JWKS URLs;
// tests reach it with an httptest server URL so they can fully
// exercise the signature-verification path with a generated RSA key
// pair instead of mocking the verifier surface.
func newSsrVerifierFromURLs(ctx context.Context, audience string, allowedIssuers, jwksURLs []string) (SsrVerifyFunc, error) {
	k, err := keyfunc.NewDefaultCtx(ctx, jwksURLs)
	if err != nil {
		return nil, fmt.Errorf("ssr: load JWKS: %w", err)
	}
	allowed := make(map[string]struct{}, len(allowedIssuers))
	for _, iss := range allowedIssuers {
		allowed[iss] = struct{}{}
	}
	parser := jwt.NewParser(
		jwt.WithExpirationRequired(),
		jwt.WithAudience(audience),
		jwt.WithValidMethods([]string{"RS256", "ES256"}),
	)
	return func(_ context.Context, token string) (uuid.UUID, error) {
		parsed, err := parser.Parse(token, k.Keyfunc)
		if err != nil {
			return uuid.Nil, fmt.Errorf("ssr: parse/verify: %w", err)
		}
		claims, ok := parsed.Claims.(jwt.MapClaims)
		if !ok {
			return uuid.Nil, errors.New("ssr: unexpected claims type")
		}
		iss, _ := claims["iss"].(string)
		if _, allowedIss := allowed[iss]; !allowedIss {
			return uuid.Nil, fmt.Errorf("ssr: issuer %q not in allowlist", iss)
		}
		actorClaim, _ := claims["actor_uid"].(string)
		if actorClaim == "" {
			return uuid.Nil, errors.New("ssr: missing actor_uid claim")
		}
		actorID, parseErr := uuid.Parse(actorClaim)
		if parseErr != nil {
			return uuid.Nil, fmt.Errorf("ssr: parse actor_uid: %w", parseErr)
		}
		return actorID, nil
	}, nil
}
