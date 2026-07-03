// Package oidc verifies OIDC tokens (e.g. Keycloak-issued access/ID tokens)
// against a single trusted issuer's published JWKS and maps them to an
// authn.Identity.
//
// The token's standard `sub` claim IS the Pivox identity id (a UUID): when
// Keycloak is the IdP, the realm user id == identities.id, so no
// provider-specific custom claim is needed.
// This is the seam that lets the gRPC interceptor stay provider-agnostic.
package oidc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/MicahParks/jwkset"
	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/dashkan/pivox/internal/authn"
)

// Config configures a Verifier for one issuer.
type Config struct {
	// Issuer is the exact `iss` every accepted token must carry
	// (e.g. https://keycloak.example.com/realms/pivox).
	Issuer string
	// JWKSURL is the issuer's JWKS endpoint
	// (e.g. <issuer>/protocol/openid-connect/certs).
	JWKSURL string
	// Audience is required (unless DisableAudienceValidation): the token's `aud`
	// claim must contain this value (aud may be a string or an array). We control
	// every client, so all our tokens carry one shared audience (set via a
	// Keycloak audience mapper) — no list needed.
	Audience string

	// DisableAudienceValidation turns the `aud` check off entirely. Audience
	// validation is ON by default and NewVerifier errors if Audience is empty
	// without this set — so skipping the check is always a deliberate choice
	// (wired to --disable-oidc-audience-validation), never an accidental empty
	// config. Use only when a deployment genuinely can't scope token audiences.
	DisableAudienceValidation bool

	// JWKSRefreshInterval is how often a background goroutine re-fetches the
	// issuer's JWKS. Keycloak doesn't rotate signing keys on a schedule — keys
	// change only on an operator-initiated rotation or a fresh realm import — so
	// this is just how fast every verifier converges on a new key after such an
	// event (KC publishes new+old keys together, enabling zero-downtime rotation).
	// 0 disables background refresh entirely (fetch once at startup, never
	// again). There is NO on-demand refresh on an unknown `kid`: that path is
	// attacker-triggerable (the `kid` is caller-controlled) and would amplify
	// forged tokens into JWKS fetches, so recovery rides on this interval only.
	//
	// NewVerifier applies NO default: an unset (zero) value means never-refresh.
	// The 5m production default lives at the flag layer
	// (--oidc-jwks-refresh-interval / PIVOX_OIDC_JWKS_REFRESH_INTERVAL), so any
	// caller constructing Config directly (e.g. a test) and relying on refresh
	// must set this explicitly.
	JWKSRefreshInterval time.Duration
}

// Verifier validates OIDC tokens from one issuer and maps them to a Pivox
// identity. Safe for concurrent use.
//
// JWKS handling (see NewVerifier): the key set is fetched once at startup and
// then refreshed by a background goroutine every Config.JWKSRefreshInterval, so
// an operator-initiated key rotation is picked up without a process restart. We
// deliberately do NOT use keyfunc's on-demand "refresh on unknown kid" path —
// the `kid` is caller-controlled, so it would let forged tokens amplify into
// JWKS fetches against the IdP.
type Verifier struct {
	keyfunc    jwt.Keyfunc
	parser     *jwt.Parser
	audience   string
	disableAud bool
}

// NewVerifier loads the issuer's JWKS and builds a Verifier. The verifier
// enforces, in order: JWT signature against the JWKS, a required `exp`, an
// `iss` matching cfg.Issuer, an `aud` containing cfg.Audience (unless
// DisableAudienceValidation), and a `sub` that parses as a UUID.
func NewVerifier(ctx context.Context, cfg Config) (*Verifier, error) {
	if cfg.Issuer == "" {
		return nil, errors.New("oidc: Config.Issuer is required")
	}
	if cfg.JWKSURL == "" {
		return nil, errors.New("oidc: Config.JWKSURL is required")
	}
	if !cfg.DisableAudienceValidation && cfg.Audience == "" {
		return nil, errors.New("oidc: audience validation is enabled but Config.Audience is empty; set Audience or DisableAudienceValidation (--disable-oidc-audience-validation)")
	}
	// Build the JWKS storage directly (not keyfunc.NewDefault*) so we get a
	// background-only refresh: the store fetches once now and a goroutine
	// re-fetches every JWKSRefreshInterval (0 = never). We do NOT wrap it in
	// jwkset.NewHTTPClient with a RefreshUnknownKID limiter — that on-demand
	// path is caller-triggerable via the token's `kid` and would let forged
	// tokens amplify into JWKS fetches. NoErrorReturnFirstHTTPReq is left false
	// (the default) so an unreachable IdP at startup is a loud, fail-fast boot
	// error rather than a process that comes up unable to verify anything.
	storage, err := jwkset.NewStorageFromHTTP(cfg.JWKSURL, jwkset.HTTPClientStorageOptions{
		Ctx:             ctx,
		RefreshInterval: cfg.JWKSRefreshInterval,
		RefreshErrorHandler: func(ctx context.Context, err error) {
			slog.ErrorContext(ctx, "oidc: JWKS background refresh failed", "url", cfg.JWKSURL, "error", err)
		},
	})
	if err != nil {
		return nil, fmt.Errorf("oidc: load JWKS: %w", err)
	}
	k, err := keyfunc.New(keyfunc.Options{Ctx: ctx, Storage: storage})
	if err != nil {
		return nil, fmt.Errorf("oidc: build keyfunc: %w", err)
	}
	// `aud` is checked in VerifyToken (must contain cfg.Audience) rather than via
	// WithAudience, so a string-or-array aud is handled uniformly.
	parser := jwt.NewParser(
		jwt.WithExpirationRequired(),
		jwt.WithIssuer(cfg.Issuer),
		jwt.WithValidMethods([]string{"RS256", "ES256"}),
	)
	return &Verifier{
		keyfunc:    k.Keyfunc,
		parser:     parser,
		audience:   cfg.Audience,
		disableAud: cfg.DisableAudienceValidation,
	}, nil
}

// VerifyToken validates the bearer token and returns the caller's identity,
// with UID set to the `sub` claim (the Pivox identity id). It satisfies the
// VerifyToken half of authn.Service.
func (v *Verifier) VerifyToken(_ context.Context, token string) (*authn.Identity, error) {
	parsed, err := v.parser.Parse(token, v.keyfunc)
	if err != nil {
		return nil, fmt.Errorf("oidc: parse/verify: %w", err)
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("oidc: unexpected claims type")
	}
	if err := v.checkAudience(claims); err != nil {
		return nil, err
	}
	sub, _ := claims["sub"].(string)
	if sub == "" {
		return nil, errors.New("oidc: missing sub claim")
	}
	if _, err := uuid.Parse(sub); err != nil {
		return nil, fmt.Errorf("oidc: sub %q is not a uuid: %w", sub, err)
	}
	email, _ := claims["email"].(string)
	return &authn.Identity{
		UID:    sub,
		Email:  email,
		Claims: map[string]any(claims),
	}, nil
}

// checkAudience enforces the audience policy unless validation is disabled: the
// token's `aud` (string or array, normalized by GetAudience) must contain
// v.audience (guaranteed non-empty when not disabled).
func (v *Verifier) checkAudience(claims jwt.MapClaims) error {
	if v.disableAud {
		return nil
	}
	tokenAuds, err := claims.GetAudience()
	if err != nil {
		return fmt.Errorf("oidc: read aud: %w", err)
	}
	if slices.Contains(tokenAuds, v.audience) {
		return nil
	}
	return fmt.Errorf("oidc: token audience %v does not include %q", []string(tokenAuds), v.audience)
}
