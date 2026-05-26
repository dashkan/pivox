package main

import (
	"strings"

	"github.com/dashkan/pivox/internal/config"
	"github.com/spf13/cobra"
)

func addSyncAuthFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.String("allowed-service-accounts", envOrDefault("PIVOX_ALLOWED_SERVICE_ACCOUNTS", ""), "Comma-separated list of service account emails allowed to call internal endpoints")
	f.String("audience", envOrDefault("PIVOX_AUDIENCE", ""), "Expected audience in OIDC tokens (e.g. https://api.pivox.app)")
}

func loadSyncAuthConfig(cmd *cobra.Command) config.ServiceAccountAuthConfig {
	raw := must(cmd.Flags().GetString("allowed-service-accounts"))
	var accounts []string
	if raw != "" {
		// Trim each entry AND drop empties. A trailing comma or
		// adjacent commas in the env var ("sa@a,,sa@b" or "sa@a,")
		// would otherwise land an empty string in the allowlist —
		// which then matches a token with no `email` claim (the
		// type assertion `.(string)` on a missing claim returns "").
		// Pivox's downstream verifier builds an `allowed[email]`
		// map; an `allowed[""]` entry is a real bypass.
		for _, sa := range strings.Split(raw, ",") {
			sa = strings.TrimSpace(sa)
			if sa != "" {
				accounts = append(accounts, sa)
			}
		}
	}
	return config.ServiceAccountAuthConfig{
		AllowedServiceAccounts: accounts,
		Audience:               must(cmd.Flags().GetString("audience")),
	}
}
