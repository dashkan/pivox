package authn

// ClaimOrganization is the OIDC access-token claim naming the
// organization(s) the token is scoped to. Keycloak populates it from
// the `organization` client scope (an org-membership mapper); it is
// absent on tokens minted without that scope (the non-MCP
// web/electron flows). This is the single source of truth for the
// claim key — both the MCP verifier and the `accounts/me` whoami read
// it through the helper below rather than hardcoding the string.
const ClaimOrganization = "organization"

// OrganizationsFromClaims normalizes the `organization` claim to a
// slice of org aliases (each equal to a Pivox organization slug).
//
// Keycloak's bare-`organization` (ANY) scope yields a single-element
// JSON array, but a bare string is accepted defensively, as is a
// pre-decoded []string. Returns nil when the claim is absent, empty,
// or of an unexpected type — callers treat nil as "token not bound to
// an organization."
func OrganizationsFromClaims(claims map[string]any) []string {
	switch v := claims[ClaimOrganization].(type) {
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	case []any:
		orgs := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				orgs = append(orgs, s)
			}
		}
		if len(orgs) == 0 {
			return nil
		}
		return orgs
	case []string:
		orgs := make([]string, 0, len(v))
		for _, s := range v {
			if s != "" {
				orgs = append(orgs, s)
			}
		}
		if len(orgs) == 0 {
			return nil
		}
		return orgs
	default:
		return nil
	}
}
