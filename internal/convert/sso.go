package convert

import (
	"encoding/json"
	"log/slog"

	"google.golang.org/protobuf/types/known/timestamppb"

	db "github.com/dashkan/pivox/internal/db/generated"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
)

// SsoConfigToProto wraps a db.SsoConfig row into the wire-level
// apiv1.SsoConfig. The resource name is constructed from the parent
// org slug; the OIDC/SAML JSONB columns are unmarshaled into the
// proto oneof.
//
// client_secret is NEVER returned to clients (per the proto's
// OUTPUT_ONLY-by-design treatment). The KMS ciphertext stays at
// rest; UpdateSsoConfig is the only path that decrypts it, and only
// to forward to Firebase Admin SDK.
func SsoConfigToProto(s db.SsoConfig, orgSlug string) *apiv1.SsoConfig {
	pb := &apiv1.SsoConfig{
		Name:               "organizations/" + orgSlug + "/ssoConfig",
		FirebaseProviderId: s.FirebaseProviderID,
		DisplayName:        s.DisplayName,
		Enabled:            s.Enabled,
		Etag:               s.Etag,
		CreateTime:         timestamppb.New(s.CreateTime),
		UpdateTime:         timestamppb.New(s.UpdateTime),
	}

	// Unmarshal whichever JSONB column is set. CHECK constraint
	// in the schema enforces exactly one — but we tolerate both
	// being nil (returning a row with neither populated, which is
	// invalid but shouldn't crash on read).
	if len(s.OidcConfig) > 0 {
		var oidcRow oidcConfigRow
		if err := json.Unmarshal(s.OidcConfig, &oidcRow); err != nil {
			slog.Error("convert: unmarshal oidc_config", "error", err)
		} else {
			pb.Config = &apiv1.SsoConfig_Oidc{Oidc: oidcRow.toProto()}
		}
	}
	if len(s.SamlConfig) > 0 {
		var samlRow samlConfigRow
		if err := json.Unmarshal(s.SamlConfig, &samlRow); err != nil {
			slog.Error("convert: unmarshal saml_config", "error", err)
		} else {
			pb.Config = &apiv1.SsoConfig_Saml{Saml: samlRow.toProto()}
		}
	}
	return pb
}

// oidcConfigRow is the JSONB shape persisted in sso_configs.oidc_config.
// It mirrors apiv1.OidcConfig minus client_secret (which lives in
// the encrypted bytea column, not in the JSON blob — separating
// secret material from descriptive config keeps rotation simpler
// and prevents accidental log exposure).
type oidcConfigRow struct {
	Issuer      string `json:"issuer"`
	ClientID    string `json:"client_id"`
	CodeFlow    bool   `json:"code_flow"`
	IDTokenFlow bool   `json:"id_token_flow"`
}

func (r oidcConfigRow) toProto() *apiv1.OidcConfig {
	return &apiv1.OidcConfig{
		Issuer:   r.Issuer,
		ClientId: r.ClientID,
		// ClientSecret deliberately empty — never returned to clients.
		ResponseType: &apiv1.OidcConfig_ResponseType{
			Code:    r.CodeFlow,
			IdToken: r.IDTokenFlow,
		},
	}
}

// OidcConfigRowFromProto builds the JSONB row shape from a proto
// OidcConfig. Used by UpdateSsoConfig handler before persistence.
func OidcConfigRowFromProto(p *apiv1.OidcConfig) ([]byte, error) {
	row := oidcConfigRow{
		Issuer:      p.GetIssuer(),
		ClientID:    p.GetClientId(),
		CodeFlow:    p.GetResponseType().GetCode(),
		IDTokenFlow: p.GetResponseType().GetIdToken(),
	}
	return json.Marshal(row)
}

// samlConfigRow is the JSONB shape persisted in sso_configs.saml_config.
// SAML is proto-defined for forward-compat but not implemented in
// the v1 handler; this row exists so a future SAML wiring doesn't
// require a schema change.
type samlConfigRow struct {
	IdpEntityID           string   `json:"idp_entity_id"`
	SsoURL                string   `json:"sso_url"`
	X509Certificates      []string `json:"x509_certificates"`
	RequestSigningEnabled bool     `json:"request_signing_enabled"`
}

func (r samlConfigRow) toProto() *apiv1.SamlConfig {
	return &apiv1.SamlConfig{
		IdpEntityId:           r.IdpEntityID,
		SsoUrl:                r.SsoURL,
		X509Certificates:      r.X509Certificates,
		RequestSigningEnabled: r.RequestSigningEnabled,
	}
}

// SamlConfigRowFromProto builds the JSONB row shape from a proto
// SamlConfig. RpEntityId and CallbackUrl are server-derived
// (Firebase-managed) and not persisted on the local row.
func SamlConfigRowFromProto(p *apiv1.SamlConfig) ([]byte, error) {
	row := samlConfigRow{
		IdpEntityID:           p.GetIdpEntityId(),
		SsoURL:                p.GetSsoUrl(),
		X509Certificates:      p.GetX509Certificates(),
		RequestSigningEnabled: p.GetRequestSigningEnabled(),
	}
	return json.Marshal(row)
}
