// Package mcp implements Pivox's remote Model Context Protocol (MCP) server: an
// org-scoped, OAuth-protected HTTP surface exposing resources and tools to MCP
// clients (Claude, ChatGPT, VS Code, ...).
//
// Authorization is Keycloak-issued access tokens whose audience is the MCP
// resource URL (bound via a Keycloak `mcp:*` client scope + audience mapper).
// That audience is distinct from the main Pivox API's audience on purpose: it is
// the anti-confusion boundary that stops a token minted for one surface from
// being replayed against the other. The verifier here therefore wraps an
// oidc.Verifier configured with the MCP resource URL as its audience.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"

	"github.com/dashkan/pivox/internal/authn"
)

// OIDC claim keys read off a Keycloak access token. The `organization`
// claim key lives in internal/authn (authn.ClaimOrganization) — the
// single source of truth shared with the accounts/me whoami.
const (
	claimScope      = "scope"
	claimExpiration = "exp"
)

// ExtraIdentity is the auth.TokenInfo.Extra key under which the full verified
// *authn.Identity (UID + email + raw token claims) is stashed.
const ExtraIdentity = "identity"

// ExtraRawToken is the auth.TokenInfo.Extra key under which the RAW bearer
// token string is stashed. The tool/resource/completion handlers forward it as
// outbound gRPC metadata on the in-process bufconn call to McpService, so that
// service's own interceptor chain re-verifies the MCP-audience token (cheap,
// JWKS-cached) and runs membership/permission before touching data.
const ExtraRawToken = "raw_token"

// NewTokenVerifier adapts an authn.Service into the SDK's auth.TokenVerifier.
//
// verifySvc MUST be an OIDC verifier configured with the MCP resource URL as its
// audience (see package docs); the audience check is the security boundary, and
// this adapter deliberately does not re-derive the resource identifier from the
// request (the Host header is forgeable). On any verification failure it returns
// the auth.ErrInvalidToken sentinel — which drives the SDK's 401 + WWW-
// Authenticate challenge — without surfacing the underlying reason to the caller
// (it is logged server-side at debug level instead).
//
// verifySvc is required; a nil service is a boot-time programmer error.
func NewTokenVerifier(verifySvc authn.Service) auth.TokenVerifier {
	if verifySvc == nil {
		panic("mcp: NewTokenVerifier requires a non-nil authn.Service")
	}
	return func(ctx context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
		identity, err := verifySvc.VerifyToken(ctx, token)
		if err != nil {
			// Log the real reason (bad signature/issuer/audience/expiry) server-
			// side only; return the bare sentinel so nothing about the boundary
			// leaks to the caller. Debug level: token verification failures are
			// attacker-triggerable, so they must not be a routine error-log firehose.
			slog.DebugContext(ctx, "mcp: token verification failed", "error", err)
			return nil, fmt.Errorf("mcp: token verification failed: %w", auth.ErrInvalidToken)
		}

		// Stash the full verified identity so the whoami handler can resolve the
		// account through iam.BuildAccount (which reads email, the `name` claim,
		// and the `organization` claim off it). The SDK-facing fields
		// (UserID/Scopes/Expiration) are still populated for the transport.
		return &auth.TokenInfo{
			UserID:     identity.UID,
			Scopes:     scopesFromClaims(identity.Claims),
			Expiration: expirationFromClaims(identity.Claims),
			Extra: map[string]any{
				ExtraIdentity: identity,
				// Carry the raw token so the SDK handlers can forward it to
				// McpService over bufconn for re-verification + authz.
				ExtraRawToken: token,
			},
		}, nil
	}
}

// scopesFromClaims splits the space-delimited OAuth `scope` claim. Returns nil
// when the claim is absent or not a string.
func scopesFromClaims(claims map[string]any) []string {
	raw, ok := claims[claimScope].(string)
	if !ok || raw == "" {
		return nil
	}
	return strings.Fields(raw)
}

// expirationFromClaims reads the `exp` claim (unix seconds) into a time.Time so
// auth.TokenInfo.Expiration is set. The underlying verifier already enforces a
// present, non-expired `exp`; this is just carried through. Zero time when absent.
func expirationFromClaims(claims map[string]any) time.Time {
	switch v := claims[claimExpiration].(type) {
	case float64:
		return time.Unix(int64(v), 0)
	case int64:
		return time.Unix(v, 0)
	case json.Number:
		// Guard the assumption: golang-jwt decodes numbers as float64 today, so
		// this branch is currently unreachable — but if the OIDC layer ever uses a
		// json.Decoder with UseNumber(), a missing case here would zero the
		// expiration and the SDK would 401 *every* token (silent fail-closed outage).
		if n, err := v.Int64(); err == nil {
			return time.Unix(n, 0)
		}
		return time.Time{}
	default:
		return time.Time{}
	}
}
