//go:build !dev

package main

import (
	"strings"

	"github.com/dashkan/pivox-server/internal/config"
	"github.com/spf13/cobra"
)

func addSyncAuthFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.String("allowed-service-accounts", envOrDefault("ALLOWED_SERVICE_ACCOUNTS", ""), "Comma-separated list of service account emails allowed to call internal endpoints")
	f.String("audience", envOrDefault("AUDIENCE", ""), "Expected audience in OIDC tokens (e.g. https://api.pivox.app)")
}

func loadSyncAuthConfig(cmd *cobra.Command) config.SyncAuthConfig {
	raw := must(cmd.Flags().GetString("allowed-service-accounts"))
	var accounts []string
	if raw != "" {
		accounts = strings.Split(raw, ",")
		for i := range accounts {
			accounts[i] = strings.TrimSpace(accounts[i])
		}
	}
	return config.SyncAuthConfig{
		AllowedServiceAccounts: accounts,
		Audience:               must(cmd.Flags().GetString("audience")),
	}
}
