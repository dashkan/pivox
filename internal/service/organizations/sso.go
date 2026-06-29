package organizations

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/netip"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	"github.com/dashkan/pivox/internal/server"
)

// GetSsoConfig returns the per-org SSO configuration. The
// underlying client_secret is NEVER returned to clients — the proto
// treats it as effectively output-only-empty by design (callers
// rotate via Update rather than reading).
//
// Returns NOT_FOUND when no SsoConfig has been set for the org.
//
// Permission: organizations.ssoConfig.read (interceptor-gated).
func (s *OrganizationsServer) GetSsoConfig(ctx context.Context, req *apiv1.GetSsoConfigRequest) (*apiv1.SsoConfig, error) {
	resolved := server.MustResolvedOrgFromContext(ctx)
	if err := assertSsoConfigName(req.GetName(), resolved.Slug); err != nil {
		return nil, err
	}
	row, err := s.queries.GetSsoConfigByOrgID(ctx, resolved.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierr.NotFound("SsoConfig", req.GetName())
		}
		slog.ErrorContext(ctx, "get sso config: lookup failed", "org_id", resolved.ID, "error", err)
		return nil, apierr.Internal("lookup sso config")
	}
	return convert.SsoConfigToProto(row, resolved.Slug, nil), nil
}

// UpdateSsoConfig is the singleton create-or-update for the
// per-org SsoConfig row. Steps, in order:
//
//  1. Validate the request shape (org slug match, exactly one of
//     OIDC/SAML set, OIDC required fields populated).
//  2. KMS-envelope-encrypt the client_secret if the request set
//     one. Empty string means "leave the existing secret alone";
//     the SQL upsert preserves it via COALESCE.
//  3. Look up the existing row to keep a stable provider id
//     ("oidc.<org-slug>") across the row's lifetime.
//  4. Upsert the local row (provider_id + display_name + enabled +
//     oidc_config JSONB + ciphertext).
//
// Keycloak does SSO natively now (the realm owns the upstream OIDC/SAML
// provider), so this handler no longer provisions an upstream provider —
// it only persists the Pivox-side SsoConfig row.
//
// Permission: organizations.ssoConfig.update (interceptor-gated).
func (s *OrganizationsServer) UpdateSsoConfig(ctx context.Context, req *apiv1.UpdateSsoConfigRequest) (*apiv1.SsoConfig, error) {
	if s.encryptor == nil {
		return nil, apierr.Internal("UpdateSsoConfig is not configured on this server (encryptor dep missing)")
	}
	resolved := server.MustResolvedOrgFromContext(ctx)
	cfg := req.GetSsoConfig()
	if cfg == nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("sso_config", "must not be nil"))
	}
	if err := assertSsoConfigName(cfg.GetName(), resolved.Slug); err != nil {
		return nil, err
	}

	caller := server.MustPivoxUserID(ctx)

	// Exactly-one-of validation. The proto's `oneof config` already
	// enforces at most one, but both unset is a request-shape error
	// the handler must surface. Per-config field validation runs
	// before any I/O so callers see InvalidArgument promptly.
	oidc := cfg.GetOidc()
	saml := cfg.GetSaml()
	if (oidc == nil) == (saml == nil) {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("sso_config.config",
			"exactly one of oidc / saml must be set"))
	}
	if oidc != nil {
		if err := validateOidc(oidc); err != nil {
			return nil, err
		}
	} else {
		if err := validateSaml(saml); err != nil {
			return nil, err
		}
	}

	// Read the existing row so we keep a stable provider_id across
	// updates. UNIQUE(org_id) keeps the lookup at most-one-row.
	existing, getErr := s.queries.GetSsoConfigByOrgID(ctx, resolved.ID)
	creating := errors.Is(getErr, pgx.ErrNoRows)
	if !creating && getErr != nil {
		slog.ErrorContext(ctx, "update sso config: lookup existing row failed", "org_id", resolved.ID, "error", getErr)
		return nil, apierr.Internal("lookup existing sso config")
	}

	upsert := db.UpsertSsoConfigParams{
		OrgID:       resolved.ID,
		DisplayName: cfg.GetDisplayName(),
		Enabled:     cfg.GetEnabled(),
		CreatedBy:   convert.PgUUID(caller),
	}

	switch {
	case oidc != nil:
		oidcJSON, err := convert.OidcConfigRowFromProto(oidc)
		if err != nil {
			slog.ErrorContext(ctx, "update sso config: marshal oidc config failed", "error", err)
			return nil, apierr.Internal("marshal oidc config")
		}
		var ciphertext []byte
		if newSecret := oidc.GetClientSecret(); newSecret != "" {
			ct, err := s.encryptor.Encrypt([]byte(newSecret))
			if err != nil {
				slog.ErrorContext(ctx, "update sso config: encrypt client secret failed", "error", err)
				return nil, apierr.Internal("encrypt client secret")
			}
			ciphertext = ct
		}
		upsert.FirebaseProviderID = providerID("oidc.", resolved.Slug, existing, creating)
		upsert.OidcConfig = oidcJSON
		upsert.ClientSecretCiphertext = ciphertext
	default: // saml != nil
		samlJSON, err := convert.SamlConfigRowFromProto(saml)
		if err != nil {
			slog.ErrorContext(ctx, "update sso config: marshal saml config failed", "error", err)
			return nil, apierr.Internal("marshal saml config")
		}
		upsert.FirebaseProviderID = providerID("saml.", resolved.Slug, existing, creating)
		upsert.SamlConfig = samlJSON
	}

	row, err := s.queries.UpsertSsoConfig(ctx, upsert)
	if err != nil {
		slog.ErrorContext(ctx, "update sso config: upsert failed", "org_id", resolved.ID, "error", err)
		return nil, apierr.Internal("upsert sso config")
	}
	return convert.SsoConfigToProto(row, resolved.Slug, nil), nil
}

// providerID returns the stable provider id stored on the SsoConfig
// row. It's derived from the org slug ("oidc.<slug>" / "saml.<slug>")
// on first create and preserved from the existing row thereafter so a
// re-Update never changes it. Vestigial: Keycloak owns the upstream
// provider now, but the column is retained on the row for forward
// compatibility.
func providerID(prefix, orgSlug string, existing db.SsoConfig, creating bool) string {
	if !creating && existing.FirebaseProviderID != "" {
		return existing.FirebaseProviderID
	}
	return prefix + orgSlug
}

// validateSaml enforces the request-side SAML invariants beyond
// what protovalidate covers: idp_entity_id, sso_url, and at least
// one x509_certificate are mandatory. Firebase rejects
// missing-required errors with opaque messages, so catching them
// here gives the caller a clearer InvalidArgument.
func validateSaml(s *apiv1.SamlConfig) error {
	if strings.TrimSpace(s.GetIdpEntityId()) == "" {
		return apierr.InvalidArgument(apierr.FieldViolation("sso_config.saml.idp_entity_id", "must not be empty"))
	}
	if strings.TrimSpace(s.GetSsoUrl()) == "" {
		return apierr.InvalidArgument(apierr.FieldViolation("sso_config.saml.sso_url", "must not be empty"))
	}
	if len(s.GetX509Certificates()) == 0 {
		return apierr.InvalidArgument(apierr.FieldViolation("sso_config.saml.x509_certificates", "at least one PEM-encoded certificate is required"))
	}
	return nil
}

// assertSsoConfigName validates the resource path shape and matches
// the org slug against the interceptor-resolved scope. Defense
// against gate-vs-handler name drift.
func assertSsoConfigName(name, expectedOrg string) error {
	parts := strings.Split(name, "/")
	if len(parts) != 3 || parts[0] != "organizations" || parts[1] == "" || parts[2] != "ssoConfig" {
		return apierr.InvalidArgument(apierr.FieldViolation("name",
			"expected organizations/{org}/ssoConfig"))
	}
	if parts[1] != expectedOrg {
		return apierr.InvalidArgument(apierr.FieldViolation("name",
			"org slug in path does not match resolved scope"))
	}
	return nil
}

// validateOidc enforces the request-side OIDC invariants beyond
// what protovalidate covers: at least one response_type flag must
// be set, and code-flow requires a client_secret on first create.
// (The handler can't easily check "first create" here — the create-
// vs-update branch fires later — so we only enforce the response-
// type-must-be-non-empty rule and let Firebase reject a code-flow-
// without-secret combination authoritatively.)
func validateOidc(o *apiv1.OidcConfig) error {
	rt := o.GetResponseType()
	if !rt.GetCode() && !rt.GetIdToken() {
		return apierr.InvalidArgument(apierr.FieldViolation(
			"sso_config.oidc.response_type",
			"at least one of code / id_token must be true"))
	}
	// RFC 8414 §3 requires HTTPS for the issuer. Without this an
	// admin could persist an http:// issuer that the OAuth broker
	// would later reject — better to surface the misconfiguration
	// at write time. Localhost http:// is allowed because dev IdPs
	// (Keycloak in docker) commonly run without TLS. Mirror of
	// `requireSecureIssuer` in internal/server/oauth_broker.go.
	if iss := o.GetIssuer(); iss != "" {
		u, err := url.Parse(iss)
		if err != nil {
			return apierr.InvalidArgument(apierr.FieldViolation(
				"sso_config.oidc.issuer",
				"invalid URL: "+err.Error()))
		}
		isSecure := u.Scheme == "https"
		isLoopbackHTTP := u.Scheme == "http" && isLoopbackHost(u.Host)
		if !isSecure && !isLoopbackHTTP {
			return apierr.InvalidArgument(apierr.FieldViolation(
				"sso_config.oidc.issuer",
				"issuer must be https (http only allowed for localhost)"))
		}
	}
	return nil
}

// isLoopbackHost mirrors the broker's localhost check; kept local
// so this package doesn't import internal/server.
func isLoopbackHost(host string) bool {
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		h = host
	}
	if h == "localhost" {
		return true
	}
	addr, err := netip.ParseAddr(h)
	if err != nil {
		return false
	}
	return addr.IsLoopback()
}

// pgUUID wraps a uuid.UUID into the pgtype shape sqlc expects on
// nullable columns. Unused if your queries don't take pgtype.UUID
// args; left here to avoid an unused-import on pgtype if all the
// references go through generated code.
var _ = pgtype.UUID{}
