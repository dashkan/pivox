package main

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/dashkan/pivox/internal/config"
)

// addSsrAuthFlags registers the SSR-acting-as verifier flags on the
// root cobra command. Mirrors addSyncAuthFlags for the parallel
// service-account surface — both are allowlists of Google Cloud
// service accounts but with separate trust boundaries (the Firebase
// Functions SA is permitted to call sync; the SSR server SA is
// permitted to act on behalf of users — they must NOT share an
// allowlist).
//
// Audience defaults to the shared `--audience` flag (PIVOX_AUDIENCE)
// — in most deployments both SyncAuth and SsrAuth target the same
// backend URL. The override (--ssr-audience / PIVOX_SSR_AUDIENCE)
// exists for deployments that want a distinct audience per surface
// (clearer token introspection, ability to invalidate one surface
// without breaking the other).
//
// When the allowlist is empty, SsrAuth.Enabled returns false and
// main.go leaves the SSR auth path disabled — the composite isn't
// constructed and non-Firebase tokens are rejected by the bare
// Firebase service. This is the correct dev / electron-only default.
func addSsrAuthFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.String("ssr-allowed-service-accounts", envOrDefault("PIVOX_SSR_ALLOWED_SERVICE_ACCOUNTS", ""),
		"Comma-separated list of service account emails permitted to mint SSR-acting-as JWTs. Empty disables the SSR auth path.")
	f.String("ssr-audience", envOrDefault("PIVOX_SSR_AUDIENCE", ""),
		"Expected audience in SSR-acting-as JWTs. Empty inherits from --audience (PIVOX_AUDIENCE).")
}

// loadSsrAuthConfig builds the SSR ServiceAccountAuthConfig from the
// registered flags. Audience is loaded verbatim from --ssr-audience;
// the empty-string fallback to SyncAuth's audience is applied in
// serve() after both configs are loaded, so this function doesn't
// reach across into a different addFlags' registration.
func loadSsrAuthConfig(cmd *cobra.Command) config.ServiceAccountAuthConfig {
	raw := must(cmd.Flags().GetString("ssr-allowed-service-accounts"))
	var accounts []string
	if raw != "" {
		for _, sa := range strings.Split(raw, ",") {
			sa = strings.TrimSpace(sa)
			if sa != "" {
				accounts = append(accounts, sa)
			}
		}
	}
	return config.ServiceAccountAuthConfig{
		AllowedServiceAccounts: accounts,
		Audience:               must(cmd.Flags().GetString("ssr-audience")),
	}
}
